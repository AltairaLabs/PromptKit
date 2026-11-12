package mock_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

func problemsOf(t *testing.T, body string) []mock.ConfigProblem {
	t.Helper()
	err := mock.ValidateConfig([]byte(body))
	var cfgErr *mock.ConfigError
	require.True(t, errors.As(err, &cfgErr), "want a *ConfigError, got %v", err)
	return cfgErr.Problems
}

// Every unknown key is reported with its line, its place in the config and
// the keys valid there, so an editor can put each diagnostic on its line.
func TestValidateConfig_ReportsEachUnknownKeyWithItsLine(t *testing.T) {
	body := "scenarios:\n" + // 1
		"  s1:\n" + // 2
		"    turns:\n" + // 3
		"      1:\n" + // 4
		"        contnet: hi\n" + // 5
		"        tool_calls:\n" + // 6
		"          - name: t\n" + // 7
		"            args: {}\n" + // 8
		"selfplay:\n" + // 9
		"  p1:\n" + // 10
		"    steps:\n" + // 11
		"      a:\n" + // 12
		"        parts:\n" + // 13
		"          - type: text\n" + // 14
		"            txt: hi\n" // 15
	assert.Equal(t, []mock.ConfigProblem{
		{Line: 5, Path: "scenarios.s1.turns.1", Message: `unknown key "contnet" (valid: audio_file, ` +
			`audio_mime_type, audio_sample_rate, content, parts, response, text, tool_calls, type)`},
		{Line: 8, Path: "scenarios.s1.turns.1.tool_calls[0]", Message: `unknown key "args" (valid: arguments, name)`},
		{Line: 15, Path: "selfplay.p1.steps.a.parts[0]", Message: `unknown key "txt" (valid: audio_url, image_url, ` +
			`metadata, text, type, video_url)`},
	}, problemsOf(t, body))
}

// Unknown keys the YAML decoder finds, at the top level or in a scenario,
// carry the decoder's line.
func TestValidateConfig_DecoderProblemsCarryTheirLine(t *testing.T) {
	assert.Equal(t, []mock.ConfigProblem{
		{Line: 1, Message: `unknown key "default_response" (valid: defaultResponse, scenarios, selfplay)`},
		{Line: 4, Message: `unknown key "turn" (valid: defaultResponse, steps, turns)`},
	}, problemsOf(t, "default_response: hi\nscenarios:\n  s1:\n    turn:\n      1: hi\n"))
}

// A YAML syntax error is one problem on its line.
func TestValidateConfig_SyntaxErrorCarriesItsLine(t *testing.T) {
	assert.Equal(t, []mock.ConfigProblem{
		{Line: 2, Message: "mapping values are not allowed in this context"},
	}, problemsOf(t, "defaultResponse: hi\nscenarios: a: b\n"))
}

// A valid config, and an empty one, have no problems; the same config with
// one key misspelled has one.
func TestValidateConfig_ValidConfigIsNil(t *testing.T) {
	valid := "defaultResponse: hi\nscenarios:\n  s1:\n    turns:\n      1:\n        content: x\n"
	cases := map[string]struct {
		body string
		want []mock.ConfigProblem
	}{
		"valid": {valid, nil},
		"empty": {"", nil},
		"one misspelled key": {strings.Replace(valid, "content", "contnet", 1), []mock.ConfigProblem{{
			Line: 6, Path: "scenarios.s1.turns.1", Message: `unknown key "contnet" (valid: audio_file, ` +
				`audio_mime_type, audio_sample_rate, content, parts, response, text, tool_calls, type)`,
		}}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var got []mock.ConfigProblem
			var cfgErr *mock.ConfigError
			if err := mock.ValidateConfig([]byte(tc.body)); errors.As(err, &cfgErr) {
				got = cfgErr.Problems
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

// The error text names each problem's place, as NewFileMockRepository's does.
func TestValidateConfig_ErrorTextNamesEachPlace(t *testing.T) {
	err := mock.ValidateConfig([]byte("default_response: hi\nscenarios:\n  s1:\n    turns:\n      1:\n        contnet: hi\n"))
	require.Error(t, err)
	assert.Equal(t, `line 1: unknown key "default_response" (valid: defaultResponse, scenarios, selfplay)`, err.Error(),
		"the decoder stops the check, so the turn's key is not reached")
}
