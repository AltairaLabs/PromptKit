package providers

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The rejections are parsed from the error texts the live APIs returned; a
// 400 naming anything but a sampling parameter, or any other status, is not
// one.
func TestRejectedParams_ParsesEachAPIsRejection(t *testing.T) {
	httpErr := func(status int, body string) error {
		return &ProviderHTTPError{StatusCode: status, URL: "u", Body: body}
	}
	cases := []struct {
		name string
		err  error
		want []string
	}{
		{"openai value", httpErr(http.StatusBadRequest,
			`{"error":{"message":"Unsupported value: 'temperature' does not support 0 with this model."}}`),
			[]string{ParamTemperature}},
		{"openai parameter", httpErr(http.StatusBadRequest,
			`{"error":{"message":"Unsupported parameter: 'presence_penalty' is not supported with this model."}}`),
			[]string{ParamPresencePenalty}},
		{"anthropic", httpErr(http.StatusBadRequest,
			"{\"error\":{\"message\":\"`top_p` is deprecated for this model.\"}}"), []string{ParamTopP}},
		{"gemini penalty", httpErr(http.StatusBadRequest,
			`{"error":{"code":400,"message":"Penalty is not enabled for this model"}}`),
			[]string{ParamFrequencyPenalty, ParamPresencePenalty}},
		{"wrapped, as the tool paths return it", errors.New(
			"API error (status 400): {\"error\":{\"message\":\"Unsupported parameter: 'top_p' is not supported\"}}"),
			[]string{ParamTopP}},
		{"not a sampling parameter", httpErr(http.StatusBadRequest,
			`{"error":{"message":"Unsupported parameter: 'max_tokens' is not supported with this model."}}`), nil},
		{"not a 400", httpErr(http.StatusInternalServerError,
			`{"error":{"message":"Unsupported parameter: 'top_p' is not supported with this model."}}`), nil},
		{"nil", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, RejectedParams(tc.err))
		})
	}
}

// RetryRejectedParams retries only while a rejection names a parameter that
// was still being sent, so it ends: a rejection of one already withheld (a
// provider that ignores the set) returns the error rather than looping.
func TestRetryRejectedParams_EndsOnceNothingNewIsLearned(t *testing.T) {
	b := NewBaseProvider("retry-ends", false, http.DefaultClient)
	reject := func(param string) error {
		return &ProviderHTTPError{StatusCode: http.StatusBadRequest,
			Body: "Unsupported parameter: '" + param + "' is not supported with this model."}
	}
	script := []error{reject(ParamTemperature), reject(ParamTopP), nil}
	calls := 0
	err := b.RetryRejectedParams(func() error {
		calls++
		return script[calls-1]
	})
	assert.NoError(t, err)
	assert.Equal(t, 3, calls)
	assert.True(t, b.ParamRejected(ParamTemperature))
	assert.True(t, b.ParamRejected(ParamTopP))
	assert.False(t, b.ParamRejected(ParamTopK))

	calls = 0
	err = b.RetryRejectedParams(func() error {
		calls++
		return reject(ParamTopP) // already withheld, yet rejected again
	})
	assert.Equal(t, 1, calls, "no retry when nothing new was learned")
	assert.ErrorContains(t, err, "top_p")
}

// A parameter is supported until the config lists it or the API rejects it,
// including on a zero BaseProvider (a provider built without the constructor).
func TestParamSupported_ConfiguredAndRejected(t *testing.T) {
	var zero BaseProvider
	assert.True(t, zero.ParamSupported(ParamTopK))
	assert.Nil(t, zero.RejectedParamNames())
	zero.SetUnsupportedParams(nil)
	assert.True(t, zero.ParamSupported(ParamTopK), "an empty list declares nothing")
	zero.SetUnsupportedParams([]string{ParamTopK})
	assert.False(t, zero.ParamSupported(ParamTopK))
	assert.True(t, zero.ParamSupported(ParamTopP))

	b := NewBaseProvider("supported", false, http.DefaultClient)
	err := b.RetryRejectedParams(func() error {
		if b.ParamRejected(ParamTopP) {
			return nil
		}
		return &ProviderHTTPError{StatusCode: http.StatusBadRequest,
			Body: "Unsupported parameter: 'top_p' is not supported with this model."}
	})
	assert.NoError(t, err)
	assert.False(t, b.ParamSupported(ParamTopP), "rejected by the API")
	assert.Equal(t, []string{ParamTopP}, b.RejectedParamNames())
}
