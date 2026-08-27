//go:build integration

package stage_test

// Cross-provider live proof that final-turn structured output does what #1853
// needs: the model does the WORK and the answer conforms.
//
// This tests the STAGE, not the providers. The provider-level companion is
// runtime/providers/conformance's TestProviders_AssistantTurnIsJSON_Live, which
// records that tools+schema does NOT conform on Gemini — a real provider gap
// that this stage behavior is what compensates for. Both are true; they measure
// different layers.
//
// Every canned test in this package passes against a stage that sends the
// schema on every round, because the wire shape is identical either way. What
// differs is whether the MODEL keeps calling tools, and only a real provider
// shows that.
//
// Run:
//
//	ANTHROPIC_API_KEY=... OPENAI_API_KEY=... GEMINI_API_KEY=... \
//	  go test -tags integration ./runtime/pipeline/stage/ \
//	  -run TestLive_StructuredOutput -v

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/claude"
	"github.com/AltairaLabs/PromptKit/runtime/providers/gemini"
	"github.com/AltairaLabs/PromptKit/runtime/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// underwritingTools mirrors the shape of the production pack in #1853: several
// independent steps that all have to run before an answer is possible.
//
// Tool COUNT is the variable that matters. The same models handle one tool
// under a schema without trouble and start skipping the loop at seven, so a
// one-tool probe would report this bug as fixed when it is not.
var underwritingTools = []string{
	"case_fetch", "credit_report", "property_valuation",
	"affordability_assess", "aml_screen", "income_verify", "case_note_write",
}

// underwritingPrompt deliberately does NOT enumerate the tools or say "you must
// call every one". That scaffolding supplies tool-calling pressure a real pack
// does not have, and it masks the defect: with it, sonnet-4-6 scores 9/11; a
// natural description drops it to bimodal 0-or-7.
const underwritingPrompt = "You are a mortgage underwriting assistant. Underwrite case " +
	"C-1042 and return your decision. Use the tools available to you to gather what you need."

// underwritingSchema sets additionalProperties:false because Claude REQUIRES it
// on an object schema and 400s without it:
//
//	output_config.format.schema: For 'object' type, 'additionalProperties'
//	must be explicitly set to false
//
// Gemini neither needs nor accepts it; its sanitizer strips it. Including it is
// therefore what lets one schema serve all three providers — omitting it fails
// Claude while still passing Gemini, which reads as a Claude regression.
func underwritingSchema() *providers.ResponseFormat {
	return &providers.ResponseFormat{
		Type: providers.ResponseFormatJSONSchema,
		JSONSchema: json.RawMessage(`{
			"type":"object",
			"properties":{"case_id":{"type":"string"},"decision":{"type":"string"},
			              "rationale":{"type":"string"}},
			"required":["case_id","decision","rationale"],
			"additionalProperties":false}`),
	}
}

// structuredCase is one provider under test.
type structuredCase struct {
	name    string
	envKeys []string
	model   string
	build   func(t *testing.T, model string) providers.Provider
	// temperature is sent on every round. gpt-5 rejects the zero value that
	// ProviderConfig would otherwise default to:
	//   Unsupported value: 'temperature' does not support 0 with this model.
	// so a case that needs a different value says so rather than relying on the
	// struct's zero.
	temperature float32
}

func structuredCases() []structuredCase {
	return []structuredCase{
		{
			// The model from the production regression. Under the old behavior
			// it did no work at all in 6 of 15 runs.
			name:    "claude_sonnet_4_6",
			envKeys: []string{"ANTHROPIC_API_KEY"},
			model:   envOrDefault("CLAUDE_46_MODEL", "claude-sonnet-4-6"),
			build: func(t *testing.T, model string) providers.Provider {
				t.Helper()
				return claude.NewToolProvider("live-claude", model, "https://api.anthropic.com/v1",
					providers.ProviderDefaults{MaxTokens: 2048}, false)
			},
		},
		{
			name:    "claude_sonnet_5",
			envKeys: []string{"ANTHROPIC_API_KEY"},
			model:   envOrDefault("CLAUDE_MODEL", "claude-sonnet-5"),
			build: func(t *testing.T, model string) providers.Provider {
				t.Helper()
				return claude.NewToolProvider("live-claude5", model, "https://api.anthropic.com/v1",
					providers.ProviderDefaults{MaxTokens: 2048}, false)
			},
		},
		{
			name:    "openai",
			envKeys: []string{"OPENAI_API_KEY"},
			// gpt-4.1, not gpt-5. The provider serializes unset temperature AND
			// top_p, both of which gpt-5 rejects, and ProviderConfig has no
			// TopP field to override — so gpt-5 cannot be driven through the
			// stage at all today (#1856). Using it here would test that bug
			// instead of structured output. Switch to gpt-5 once #1856 lands.
			model: envOrDefault("OPENAI_MODEL", "gpt-4.1"),
			build: func(t *testing.T, model string) providers.Provider {
				t.Helper()
				return openai.NewToolProvider("live-openai", model, "https://api.openai.com/v1",
					providers.ProviderDefaults{MaxTokens: 2048}, false, nil, nil)
			},
			temperature: 1,
		},
		{
			// The worst pre-fix case: on generateContent a schema alongside
			// tools left the loop terminating only 1 run in 5.
			name:    "gemini_3",
			envKeys: []string{"GEMINI_API_KEY", "GOOGLE_API_KEY"},
			model:   envOrDefault("GEMINI_3_MODEL", "gemini-3.7-flash"),
			build: func(t *testing.T, model string) providers.Provider {
				t.Helper()
				return gemini.NewToolProvider("live-gemini", model,
					"https://generativelanguage.googleapis.com/v1beta",
					providers.ProviderDefaults{MaxTokens: 4096}, false)
			},
		},
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// requireKey skips unless one of the case's env keys is set, and exports it
// under the first name so providers reading a fixed variable find it.
func requireKey(t *testing.T, c structuredCase) {
	t.Helper()
	for _, k := range c.envKeys {
		if v := os.Getenv(k); v != "" {
			t.Setenv(c.envKeys[0], v)
			return
		}
	}
	t.Skipf("none of %v set", c.envKeys)
}

// liveRecordingExecutor notes which tools actually ran.
//
// Mutex-guarded because the stage dispatches a round's tool calls in PARALLEL
// (see getMaxParallelToolCalls) and these models do emit several per round, so
// an unguarded map here is a real concurrent write, not a theoretical one.
type liveRecordingExecutor struct {
	mu     sync.Mutex
	called map[string]bool
}

func (*liveRecordingExecutor) Name() string { return "local" }

func (e *liveRecordingExecutor) Execute(
	_ context.Context, d *tools.ToolDescriptor, _ json.RawMessage,
) (json.RawMessage, error) {
	e.mu.Lock()
	e.called[d.Name] = true
	e.mu.Unlock()
	return json.RawMessage(`{"ok":true,"detail":"nominal"}`), nil
}

func (e *liveRecordingExecutor) snapshot() map[string]bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make(map[string]bool, len(e.called))
	for k, v := range e.called {
		out[k] = v
	}
	return out
}

// liveUnderwritingRun drives one full turn through the real stage and reports
// which tools ran and whether the final answer is schema-shaped JSON.
func liveUnderwritingRun(
	t *testing.T, c structuredCase, mode stage.StructuredOutputMode,
) (called map[string]bool, conforming bool) {
	t.Helper()

	reg := tools.NewRegistry()
	rec := &liveRecordingExecutor{called: map[string]bool{}}
	for _, name := range underwritingTools {
		require.NoError(t, reg.Register(&tools.ToolDescriptor{
			Name:        name,
			Description: "Run the " + name + " step of underwriting.",
			Mode:        "local",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"case_id":{"type":"string"}},"required":["case_id"]}`),
		}))
	}
	reg.RegisterExecutor(rec)

	turnState := stage.NewTurnState()
	turnState.SystemPrompt = underwritingPrompt
	turnState.AllowedTools = underwritingTools

	st := stage.NewProviderStageWithTurnState(c.build(t, c.model), reg, nil, &stage.ProviderConfig{
		MaxTokens:            2048,
		Temperature:          c.temperature,
		ResponseFormat:       underwritingSchema(),
		StructuredOutputMode: mode,
	}, nil, nil, turnState)

	input := make(chan stage.StreamElement, 1)
	msg := types.Message{Role: "user", Content: "Underwrite case C-1042."}
	input <- stage.NewMessageElement(&msg)
	close(input)

	output := make(chan stage.StreamElement, 256)
	require.NoError(t, st.Process(context.Background(), input, output))

	var final string
	for elem := range output {
		if elem.Message != nil && elem.Message.Role == "assistant" && elem.Message.Content != "" {
			final = elem.Message.Content
		}
	}
	called = rec.snapshot()

	var parsed map[string]any
	if json.Unmarshal([]byte(final), &parsed) == nil {
		_, hasCase := parsed["case_id"]
		_, hasDecision := parsed["decision"]
		conforming = hasCase && hasDecision
	}
	t.Logf("%s (%s) mode=%s tools=%d/%d conforming=%v answer=%.80s",
		c.name, c.model, mode, len(called), len(underwritingTools), conforming, final)
	return called, conforming
}

// TestLive_StructuredOutputFinalTurn is the assertion the canned tests cannot
// make: with the schema withheld from the loop, every provider does the work
// AND returns conforming JSON.
//
// Both halves matter, and each alone is satisfiable by a broken build. Tools
// without JSON is the pre-#1853 behavior with the schema simply dropped; JSON
// without tools is the #1853 defect itself — fast, schema-valid, no work behind
// it. Only together do they describe a correct turn.
func TestLive_StructuredOutputFinalTurn(t *testing.T) {
	for _, c := range structuredCases() {
		t.Run(c.name, func(t *testing.T) {
			requireKey(t, c)

			called, conforming := liveUnderwritingRun(t, c, stage.StructuredOutputFinalTurn)

			var missing []string
			for _, name := range underwritingTools {
				if !called[name] {
					missing = append(missing, name)
				}
			}

			// A majority of the tools, not all of them.
			//
			// The defect is bimodal: measured at n=15 under every_round, the
			// model either ran the whole loop (7/7) or skipped it outright
			// (0/7) — never something in between. So the threshold that
			// separates fixed from broken is wide, and anything above zero-ish
			// is a pass.
			//
			// Requiring exactly 7 fails on benign variation instead: gpt-4.1
			// calls 6 of 7 here, and its no-schema control also averages below
			// 7, so a model deciding one step is unnecessary is not this bug.
			// An exact count would make this suite flake on a working build,
			// which costs more than the sensitivity it buys.
			minTools := len(underwritingTools)/2 + 1
			assert.GreaterOrEqualf(t, len(called), minTools,
				"%s ran only %d of %d tools under final_turn (missing %v) — skipping the loop "+
					"is the #1853 defect, and withholding the schema is supposed to prevent it",
				c.name, len(called), len(underwritingTools), missing)

			assert.Truef(t, conforming,
				"%s returned no schema-shaped answer; prose here means the schema never "+
					"reached the re-ask", c.name)
		})
	}
}

// TestLive_EveryRoundRemainsUsableAndMeasuresTheDefect drives the escape hatch
// on purpose, on the model whose production failure opened #1853.
//
// Two different things, deliberately separated:
//
// The ASSERTION is deterministic — every_round must keep working. It is the
// documented way to pin pre-#1853 behavior without waiting on a release, so a
// turn that errors or returns nothing is a regression in the escape hatch
// itself, independent of how the model behaves.
//
// The MEASUREMENT is the tool-loss count, which is only logged. The failure is
// probabilistic (6/15 runs at n=15), so asserting on it would flake in both
// directions: a clean sweep proves nothing and one bad run is not a regression.
// Reporting the fraction lets a human compare modes without the suite making a
// claim the sample size cannot support.
func TestLive_EveryRoundRemainsUsableAndMeasuresTheDefect(t *testing.T) {
	c := structuredCases()[0] // claude_sonnet_4_6
	requireKey(t, c)

	runs := 5
	if n := os.Getenv("EVERY_ROUND_RUNS"); n != "" {
		if parsed, err := strconv.Atoi(n); err == nil && parsed > 0 {
			runs = parsed
		}
	}

	lost := 0
	for i := 0; i < runs; i++ {
		called, conforming := liveUnderwritingRun(t, c, stage.StructuredOutputEveryRound)

		// Under every_round the schema is on every round, so conformance is the
		// one thing this mode does reliably. Losing it would mean the escape
		// hatch is broken outright rather than merely unsafe.
		assert.Truef(t, conforming,
			"run %d: every_round returned no schema-shaped answer; the escape hatch must stay usable",
			i+1)

		if len(called) < len(underwritingTools) {
			lost++
		}
	}
	t.Logf("every_round on %s: %d/%d runs called fewer than all %d tools",
		c.model, lost, runs, len(underwritingTools))
	fmt.Printf("EVERY_ROUND_DEGRADED=%d/%d\n", lost, runs)
}
