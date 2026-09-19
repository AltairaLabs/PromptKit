//go:build integration

package conformance_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/claude"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/gemini"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// The wire matrix proves a schema REACHES the API. It cannot prove the model
// obeyed it, and those are different claims: Gemini 2.5 accepts responseSchema
// without a mime type and then answers in prose anyway.
//
// This suite asks the question that actually matters to a caller whose contract
// is "the response conforms to this schema": is the assistant turn JSON, or is
// it prose? It runs every provider through all four modes against the live API
// and parses the result.
//
// Run:
//
//	ANTHROPIC_API_KEY=... OPENAI_API_KEY=... GEMINI_API_KEY=... \
//	  go test -tags integration ./runtime/providers/conformance/ \
//	  -run TestProviders_AssistantTurnIsJSON_Live -v

// additionalProperties:false is REQUIRED by both Claude and OpenAI in strict
// schema mode — omitting it is a 400 from each:
//
//	claude: "output_config.format.schema: For 'object' type,
//	         'additionalProperties' must be explicitly set to false"
//	openai: "'additionalProperties' is required to be supplied and to be false"
//
// Gemini neither needs nor accepts it; the provider's sanitizer strips it,
// which is why an unadorned schema works there and fails on the other two.
const answerSchema = `{"type":"object","properties":{"city":{"type":"string"},` +
	`"celsius":{"type":"number"}},"required":["city","celsius"],` +
	`"additionalProperties":false}`

// jsonCase is one provider under test, plus which modes are expected to yield
// schema-conforming JSON.
type jsonCase struct {
	name    string
	envKeys []string
	build   func(t *testing.T, model string) providers.Provider
	model   string
	// conformsInMode records the expectation per mode. False is a claim about
	// the world — a vendor limit or an unimplemented feature — and carries a
	// reason so it is never a silent skip.
	conformsInMode map[string]bool
	why            string
}

func jsonCases() []jsonCase {
	return []jsonCase{
		{
			name:    "claude",
			envKeys: []string{"ANTHROPIC_API_KEY", "CLAUDE_API_KEY"},
			model:   envOr("CLAUDE_MODEL", "claude-sonnet-4-6"),
			build: func(t *testing.T, model string) providers.Provider {
				t.Helper()
				return claude.NewToolProvider("claude-json", model,
					"https://api.anthropic.com/v1", providers.ProviderDefaults{MaxTokens: 2048}, false)
			},
			conformsInMode: allModes(true),
		},
		{
			name:    "openai",
			envKeys: []string{"OPENAI_API_KEY"},
			model:   envOr("OPENAI_MODEL", "gpt-4o-mini"),
			build: func(t *testing.T, model string) providers.Provider {
				t.Helper()
				return openai.NewToolProvider("openai-json", model,
					"https://api.openai.com/v1", providers.ProviderDefaults{MaxTokens: 2048},
					false, nil, nil)
			},
			conformsInMode: allModes(true),
		},
		{
			name:    "gemini_2.5",
			envKeys: []string{"GEMINI_API_KEY", "GOOGLE_API_KEY"},
			model:   envOr("GEMINI_25_MODEL", "gemini-2.5-flash"),
			build: func(t *testing.T, model string) providers.Provider {
				t.Helper()
				return gemini.NewToolProvider("gemini25-json", model,
					"https://generativelanguage.googleapis.com/v1beta",
					providers.ProviderDefaults{MaxTokens: 2048}, false)
			},
			// Gemini 2.5 rejects function calling combined with a JSON response
			// mime type, so the provider drops the schema on tool-carrying
			// rounds and the answer is prose. Not a defect we can fix.
			conformsInMode: map[string]bool{
				modeUnary: true, modeStream: true, modeUnaryTools: false, modeStreamTools: false,
			},
			why: "on generateContent a schema constrains every turn, so 2.5 returns HTTP 400 " +
				"for function calling plus responseMimeType application/json. Not a Gemini " +
				"limitation — the Interactions API handles it (#1851)",
		},
		{
			name:    "gemini_3",
			envKeys: []string{"GEMINI_API_KEY", "GOOGLE_API_KEY"},
			model:   envOr("GEMINI_3_MODEL", "gemini-3.7-flash"),
			build: func(t *testing.T, model string) providers.Provider {
				t.Helper()
				return gemini.NewToolProvider("gemini3-json", model,
					"https://generativelanguage.googleapis.com/v1beta",
					providers.ProviderDefaults{MaxTokens: 4096}, false)
			},
			// All four modes conform since #1852. On generateContent a schema
			// alongside tools left Gemini 3 looping without ever answering, so
			// the provider now routes that combination to the Interactions API,
			// which constrains only the answering turn.
			conformsInMode: allModes(true),
		},
	}
}

const (
	modeUnary       = "unary_no_tools"
	modeStream      = "stream_no_tools"
	modeUnaryTools  = "unary_with_tools"
	modeStreamTools = "stream_with_tools"
)

func allModes(want bool) map[string]bool {
	return map[string]bool{
		modeUnary: want, modeStream: want, modeUnaryTools: want, modeStreamTools: want,
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// TestProviders_AssistantTurnIsJSON_Live asserts the assistant's final turn is
// schema-conforming JSON rather than prose, for every provider and mode.
func TestProviders_AssistantTurnIsJSON_Live(t *testing.T) {
	modes := []string{modeUnary, modeStream, modeUnaryTools, modeStreamTools}

	for _, jc := range jsonCases() {
		t.Run(jc.name, func(t *testing.T) {
			if !anyEnvSet(jc.envKeys) {
				t.Skipf("none of %v set", jc.envKeys)
			}
			for _, mode := range modes {
				t.Run(mode, func(t *testing.T) {
					p := jc.build(t, jc.model)
					defer func() { _ = p.Close() }()

					content := runJSONMode(t, p, mode)
					t.Logf("%s/%s answer: %.120q", jc.name, mode, content)

					conforms, reason := schemaConforms(content)

					if jc.conformsInMode[mode] {
						assert.Truef(t, conforms,
							"%s/%s did NOT return schema-conforming JSON (%s). "+
								"The caller's contract is that the response parses against its "+
								"schema; prose breaks it.\nanswer=%q",
							jc.name, mode, reason, content)
						return
					}

					// Known-nonconforming: assert it, so the day it starts
					// working we find out from a red test.
					assert.Falsef(t, conforms,
						"%s/%s NOW returns conforming JSON — good news. Flip conformsInMode "+
							"so it is enforced from here on.\nprevious reason: %s",
						jc.name, mode, jc.why)
				})
			}
		})
	}
}

func anyEnvSet(keys []string) bool {
	for _, k := range keys {
		if os.Getenv(k) != "" {
			return true
		}
	}
	return false
}

// schemaConforms reports whether content parses as an object carrying both
// required fields. It deliberately checks the SHAPE, not just "is it JSON":
// a bare string is valid JSON and would not satisfy the caller.
func schemaConforms(content string) (bool, string) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false, "empty response"
	}
	var out struct {
		City    *string  `json:"city"`
		Celsius *float64 `json:"celsius"`
	}
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return false, "not JSON: " + err.Error()
	}
	if out.City == nil {
		return false, "JSON but missing required field city"
	}
	if out.Celsius == nil {
		return false, "JSON but missing required field celsius"
	}
	return true, ""
}

// jsonRequest is the shared request: a question the model can only answer
// correctly by using the tool, with the caller's schema attached.
func jsonRequest(withTool bool) providers.PredictionRequest {
	prompt := "What is the temperature in Bristol? Answer with the city and the temperature."
	if withTool {
		prompt = "What is the temperature in Bristol? Use the tool, then answer."
	}
	return providers.PredictionRequest{
		Messages:  []types.Message{{Role: "user", Content: prompt}},
		MaxTokens: 2048,
		ResponseFormat: &providers.ResponseFormat{
			Type:       providers.ResponseFormatJSONSchema,
			JSONSchema: json.RawMessage(answerSchema),
			SchemaName: "weather_answer",
			Strict:     true,
		},
	}
}

func buildProbeTool(t *testing.T, p providers.Provider) (providers.ToolSupport, providers.ProviderTools) {
	t.Helper()
	ts, ok := p.(providers.ToolSupport)
	require.True(t, ok, "provider does not implement ToolSupport")
	tools, err := ts.BuildTooling([]*providers.ToolDescriptor{{
		Name:        "get_temperature",
		Description: "Get the current temperature for a city in Celsius",
		InputSchema: json.RawMessage(
			`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
	}})
	require.NoError(t, err)
	return ts, tools
}

// collectStream accumulates a stream's spoken content. Reasoning is excluded —
// it is not the assistant's answer and must never be mistaken for it.
func collectStream(t *testing.T, ch <-chan providers.StreamChunk, err error) (string, []types.MessageToolCall) {
	t.Helper()
	content, calls, _ := collectStreamWithReasoning(t, ch, err)
	return content, calls
}

// collectStreamWithReasoning additionally returns the accumulated reasoning
// trace, which a tool loop must replay alongside the calls it carried.
//
// Gemini's Interactions API rejects a function_call whose thought signature was
// not replayed, so a caller that drops this cannot complete a second round.
func collectStreamWithReasoning(
	t *testing.T, ch <-chan providers.StreamChunk, err error,
) (string, []types.MessageToolCall, *types.ReasoningTrace) {
	t.Helper()
	require.NoError(t, err)

	var reasoning strings.Builder
	var opaque []types.OpaqueReasoning
	var sb strings.Builder
	var content string
	var calls []types.MessageToolCall
	for chunk := range ch {
		require.NoError(t, chunk.Error)
		if chunk.Delta != "" {
			sb.WriteString(chunk.Delta)
		}
		// Some providers send an accumulated snapshot rather than deltas.
		if chunk.Content != "" {
			content = chunk.Content
		}
		if len(chunk.ToolCalls) > len(calls) {
			calls = chunk.ToolCalls
		}
		if chunk.Reasoning != "" {
			reasoning.WriteString(chunk.Reasoning)
		}
		opaque = append(opaque, chunk.OpaqueReasoning...)
	}

	var trace *types.ReasoningTrace
	if reasoning.Len() > 0 || len(opaque) > 0 {
		trace = &types.ReasoningTrace{Text: reasoning.String(), Opaque: opaque}
	}
	if sb.Len() > 0 {
		return sb.String(), calls, trace
	}
	return content, calls, trace
}

// runToolLoop drives a real tool loop until the model stops calling tools,
// answering every call with a fixed reading, and returns the final answer.
// The loop is what makes this test meaningful: a schema honored on round 1 but
// dropped on the answering round would still fail here.
func runToolLoop(t *testing.T, p providers.Provider, streaming bool) string {
	t.Helper()
	ts, tools := buildProbeTool(t, p)

	req := jsonRequest(true)
	const maxRounds = 5

	for round := 1; round <= maxRounds; round++ {
		var content string
		var calls []types.MessageToolCall

		var reasoning *types.ReasoningTrace
		if streaming {
			ch, err := ts.PredictStreamWithTools(context.Background(), req, tools, "auto")
			content, calls, reasoning = collectStreamWithReasoning(t, ch, err)
		} else {
			resp, c, err := ts.PredictWithTools(context.Background(), req, tools, "auto")
			require.NoError(t, err)
			content, calls, reasoning = resp.Content, c, resp.Reasoning
		}

		if len(calls) == 0 {
			return content
		}

		// Feed every call a reading, so the model has what it needs to answer.
		//
		// Reasoning is replayed with the tool calls, not dropped. Gemini's
		// Interactions API treats a thought step as an opaque signature whose
		// replay is MANDATORY: a function_call arriving without its preceding
		// thought is rejected, and the 400 lands on the round AFTER the one that
		// omitted it. Reconstructing a bare assistant message here made this
		// loop a non-conforming caller and reported a provider fault that was
		// really this helper's. The real ProviderStage carries Reasoning on the
		// assistant message, which is why it does not hit this.
		req.Messages = append(req.Messages, types.Message{
			Role: "assistant", ToolCalls: calls, Reasoning: reasoning,
		})
		for i := range calls {
			req.Messages = append(req.Messages, types.NewToolResultMessage(
				types.NewTextToolResult(calls[i].ID, calls[i].Name, `{"celsius": 21}`)))
		}
	}
	t.Fatalf("model never stopped calling tools after %d rounds", maxRounds)
	return ""
}

// runJSONMode drives one mode and returns the assistant's final answer.
func runJSONMode(t *testing.T, p providers.Provider, mode string) string {
	t.Helper()
	switch mode {
	case modeUnary:
		resp, err := p.Predict(context.Background(), jsonRequest(false))
		require.NoError(t, err)
		return resp.Content
	case modeStream:
		ch, err := p.PredictStream(context.Background(), jsonRequest(false))
		content, _ := collectStream(t, ch, err)
		return content
	case modeUnaryTools:
		return runToolLoop(t, p, false)
	case modeStreamTools:
		return runToolLoop(t, p, true)
	}
	t.Fatalf("unknown mode %q", mode)
	return ""
}
