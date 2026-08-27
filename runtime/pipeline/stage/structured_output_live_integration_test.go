//go:build integration

package stage_test

// Live proof that final-turn structured output fixes the defect #1853 reports.
//
// Every canned test in this package passes against a stage that sends the
// schema on every round — the wire shape is identical either way. What differs
// is whether the MODEL still calls tools, and only a real provider can show
// that. So this file drives the same underwriting-shaped loop through the real
// stage, twice per model, and compares tool coverage.
//
// Run: go test -tags integration ./pipeline/stage/ -run Live_StructuredOutput

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
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

var underwritingTools = []string{
	"case_fetch", "credit_report", "property_valuation",
	"affordability_assess", "aml_screen", "income_verify", "case_note_write",
}

const underwritingPrompt = "You are a mortgage underwriting assistant. Underwrite case " +
	"C-1042 and return your decision. Use the tools available to you to gather what you need."

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

// liveUnderwritingRun drives one turn and reports which tools the model called
// and whether the final answer parsed as schema-shaped JSON.
func liveUnderwritingRun(
	t *testing.T, model string, mode stage.StructuredOutputMode,
) (called map[string]bool, conforming bool) {
	t.Helper()
	return liveUnderwritingRunWith(t, model, mode,
		claude.NewToolProvider("live", model, "https://api.anthropic.com/v1",
			providers.ProviderDefaults{MaxTokens: 2048}, false))
}

func liveUnderwritingRunWith(
	t *testing.T, model string, mode stage.StructuredOutputMode, provider providers.Provider,
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

	st := stage.NewProviderStageWithTurnState(provider, reg, nil, &stage.ProviderConfig{
		MaxTokens:            2048,
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
	t.Logf("model=%s mode=%s tools=%d/%d conforming=%v answer=%.90s",
		model, mode, len(called), len(underwritingTools), conforming, final)
	return called, conforming
}

// liveRecordingExecutor notes which tools actually ran.
//
// Mutex-guarded because the stage dispatches a round's tool calls in PARALLEL
// (see getMaxParallelToolCalls) — and these models do emit several calls in one
// round, so an unguarded map here is a real concurrent write, not a theoretical
// one.
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

// snapshot copies the recorded set for reading after the turn.
func (e *liveRecordingExecutor) snapshot() map[string]bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make(map[string]bool, len(e.called))
	for k, v := range e.called {
		out[k] = v
	}
	return out
}

// TestLive_StructuredOutputFinalTurnKeepsToolsAndJSON is the assertion the
// canned tests cannot make: with the schema withheld from the loop, the model
// does the work AND the answer conforms.
//
// claude-sonnet-4-6 is the model from the production regression — it is the one
// that loses tool calls under every_round, so it is the one worth driving.
func TestLive_StructuredOutputFinalTurnKeepsToolsAndJSON(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	for _, model := range []string{"claude-sonnet-4-6", "claude-sonnet-5"} {
		t.Run(model, func(t *testing.T) {
			called, conforming := liveUnderwritingRun(t, model, stage.StructuredOutputFinalTurn)

			var missing []string
			for _, name := range underwritingTools {
				if !called[name] {
					missing = append(missing, name)
				}
			}
			assert.Emptyf(t, missing,
				"final_turn lost tool calls (%v) — withholding the schema is supposed to "+
					"leave tool calling uncontested", missing)
			assert.True(t, conforming,
				"the re-ask must return schema-shaped JSON; prose here means the schema "+
					"never reached the final call")
		})
	}
}

// TestLive_EveryRoundRemainsUsableAndMeasuresTheDefect drives the escape hatch
// on purpose.
//
// Two different things, deliberately separated:
//
// The ASSERTION is deterministic — every_round must keep working. It is the
// documented way to pin pre-#1853 behavior without waiting on a release, so a
// turn that errors or returns nothing is a regression in the escape hatch
// itself, independent of how the model behaves.
//
// The MEASUREMENT is the tool-loss count, which is only logged. The failure is
// probabilistic (~1-2 runs in 5 at the raw API), so asserting on it would flake
// in both directions: a clean sweep proves nothing, and one bad run is not a
// regression. Reporting the fraction lets a human compare the two modes without
// the suite making a claim the sample size cannot support.
func TestLive_EveryRoundRemainsUsableAndMeasuresTheDefect(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	runs := 5
	if n := os.Getenv("EVERY_ROUND_RUNS"); n != "" {
		if parsed, err := strconv.Atoi(n); err == nil && parsed > 0 {
			runs = parsed
		}
	}

	lost := 0
	for i := 0; i < runs; i++ {
		called, conforming := liveUnderwritingRun(t, "claude-sonnet-4-6", stage.StructuredOutputEveryRound)

		// The escape hatch must still produce a schema-shaped answer. Under
		// every_round the schema is on every round, so conformance is the one
		// thing this mode does reliably — losing it would mean the mode is
		// broken outright rather than merely unsafe.
		assert.Truef(t, conforming,
			"run %d: every_round returned no schema-shaped answer; the escape hatch must stay usable", i+1)

		if len(called) < len(underwritingTools) {
			lost++
		}
	}
	t.Logf("every_round on claude-sonnet-4-6: %d/%d runs called fewer than all %d tools",
		lost, runs, len(underwritingTools))
	fmt.Printf("EVERY_ROUND_DEGRADED=%d/%d\n", lost, runs)
}

// TestLive_GeminiFinalTurnKeepsToolsAndJSON covers the provider whose failure
// was worst: on generateContent a schema alongside tools left the tool loop
// terminating only 1 run in 5.
//
// Also the composition check for the Interactions routing merged in #1852. Under
// final_turn the rounds carry no schema and the re-ask carries no tools, so
// resolveAPIMode's automatic condition (schema AND tools) can never fire and
// this whole turn stays on generateContent — the API that could not do it
// before. If that reasoning is wrong the turn fails here rather than in a
// reader's head.
func TestLive_GeminiFinalTurnKeepsToolsAndJSON(t *testing.T) {
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		key = os.Getenv("GOOGLE_API_KEY")
	}
	if key == "" {
		t.Skip("GEMINI_API_KEY / GOOGLE_API_KEY not set")
	}
	t.Setenv("GEMINI_API_KEY", key)

	const model = "gemini-3.7-flash"
	called, conforming := liveUnderwritingRunWith(t, model, stage.StructuredOutputFinalTurn,
		gemini.NewToolProvider("live-gemini", model,
			"https://generativelanguage.googleapis.com/v1beta",
			providers.ProviderDefaults{MaxTokens: 2048}, false))

	assert.NotEmpty(t, called,
		"the tool loop called nothing; a schema leaking onto the rounds stops Gemini terminating")
	assert.True(t, conforming,
		"the re-ask must return schema-shaped JSON")
	t.Logf("gemini final_turn: %d/%d tools, conforming=%v", len(called), len(underwritingTools), conforming)
}
