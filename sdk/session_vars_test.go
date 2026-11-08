package sdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// appSetVars returns what the application set, not what the conversation was
// opened with.
func TestAppSetVars_OnlyApplicationSetValues(t *testing.T) {
	conv := newTestConversation()
	assert.Nil(t, conv.appSetVars())

	conv.getBaseSession().SetVar("opened_with", "x") // as an initial variable
	conv.SetVar("customer", "Acme")
	conv.SetVars(map[string]any{"tier": 2})
	t.Setenv("APPVARTEST_REGION", "eu")
	conv.SetVarsFromEnv("APPVARTEST_")

	assert.Equal(t, map[string]string{"customer": "Acme", "tier": "2", "region": "eu"}, conv.appSetVars())
}
