package openai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTopPZeroIsOmitted pins the rule that a resolved top_p of zero never
// reaches the wire.
//
// OpenAI requires top_p > 0, so zero is invalid on every model — the older ones
// merely tolerate it. gpt-5.1/5.2 answer "must be greater than 0" and gpt-5
// answers "not supported with this model", which reads as a capability problem
// and is really a zero-value problem.
//
// Asserted on the marshaled request rather than the map, because the defect is
// about what is SENT: a key present with value 0 and a key absent are the same
// Go zero value and different HTTP requests.
func TestTopPZeroIsOmitted(t *testing.T) {
	req := map[string]interface{}{}
	addSamplingParamsToRequest(req, nil, 0.7, 0)

	raw, err := json.Marshal(req)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "top_p",
		"top_p:0 reached the wire; OpenAI requires top_p > 0, so this is an invalid request "+
			"on every model rather than a default")
	assert.Contains(t, string(raw), "temperature",
		"temperature must still be sent — only top_p is skipped at zero")
}

// TestTopPNonZeroIsSent is the other half: the skip must be about zero
// specifically, not about top_p.
func TestTopPNonZeroIsSent(t *testing.T) {
	req := map[string]interface{}{}
	addSamplingParamsToRequest(req, nil, 0.7, 1.0)

	raw, err := json.Marshal(req)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "top_p", "a configured top_p must reach the wire")
}

// TestTemperatureZeroIsStillSent pins the deliberate asymmetry.
//
// Zero is a legitimate deterministic temperature and float32 cannot distinguish
// it from unset, so skipping it would silently promote deliberate-zero callers
// to the API default — a quieter bug than the one being fixed. Models that
// genuinely reject a temperature are handled through unsupportedParams.
func TestTemperatureZeroIsStillSent(t *testing.T) {
	req := map[string]interface{}{}
	addSamplingParamsToRequest(req, nil, 0, 0.5)

	raw, err := json.Marshal(req)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"temperature":0`,
		"temperature:0 must still be sent; it is a real setting, not an unset marker")
}

// TestUnsupportedParamsStillWins guards the config-first ordering: an operator
// declaring a parameter unsupported must beat any value-based rule.
func TestUnsupportedParamsStillWins(t *testing.T) {
	req := map[string]interface{}{}
	addSamplingParamsToRequest(req, []string{"temperature", "top_p"}, 0.7, 1.0)

	raw, err := json.Marshal(req)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "temperature")
	assert.NotContains(t, string(raw), "top_p")
}

// TestIsFirstGenGPT5 pins which models get the temperature fallback.
//
// The measured split is the whole point: gpt-5, -mini and -nano accept only
// temperature 1, while gpt-5.1 and 5.2 accept any value. A "gpt-5" prefix match
// would withhold temperature from models that handle it fine, which is why this
// is an explicit list.
func TestIsFirstGenGPT5(t *testing.T) {
	for _, m := range []string{"gpt-5", "gpt-5-mini", "gpt-5-nano", "gpt-5-2025-08-07"} {
		assert.Truef(t, isFirstGenGPT5(m), "%s accepts only temperature 1 and needs the fallback", m)
	}
	for _, m := range []string{"gpt-5.1", "gpt-5.2", "gpt-5.2-pro", "gpt-4.1", "gpt-4o", "o3-mini"} {
		assert.Falsef(t, isFirstGenGPT5(m),
			"%s takes any temperature; withholding it would remove a working control", m)
	}
}
