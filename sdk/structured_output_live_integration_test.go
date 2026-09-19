//go:build integration

package sdk_test

// End-to-end integration coverage for final-turn structured output, driven
// through the PUBLIC SDK surface — sdk.Open, Send and Stream — against real
// providers.
//
// This exists because the unit tests could not be trusted to prove it. They all
// passed while the re-ask was returning HTTP 400 on every real call: the wire
// shape a fake accepts is not the wire shape a provider accepts, and a config
// field asserted in a struct is not a behavior. Everything below is observed
// from outside the pipeline.
//
// Run:
//
//	ANTHROPIC_API_KEY=... OPENAI_API_KEY=... GEMINI_API_KEY=... \
//	  go test -tags integration ./sdk/ -run TestLive_StructuredOutput -v

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/claude"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/gemini"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// underwritingPackJSON mirrors the production pack shape from #1853: several
// independent steps that must all run before an answer is possible.
//
// Tool COUNT is the variable that matters — the same models handle one tool
// under a schema without trouble and start skipping the loop at seven — so a
// smaller pack would report this fixed when it is not.
const underwritingPackJSON = `{
	"id": "live-underwriting",
	"version": "1.0.0",
	"description": "Live structured output with a multi-tool loop",
	"prompts": {
		"underwrite": {
			"id": "underwrite",
			"name": "Underwrite",
			"system_template": "You are a mortgage underwriting assistant. Underwrite the case and return your decision. Use the tools available to you to gather what you need.",
			"tools": ["case_fetch","credit_report","property_valuation","affordability_assess","aml_screen","income_verify","case_note_write"]
		}
	},
	"tools": {
		"case_fetch":           {"name":"case_fetch","description":"Load the mortgage case file and applicant details.","parameters":{"type":"object","properties":{"case_id":{"type":"string"}},"required":["case_id"]}},
		"credit_report":        {"name":"credit_report","description":"Pull the applicant's credit bureau report.","parameters":{"type":"object","properties":{"case_id":{"type":"string"}},"required":["case_id"]}},
		"property_valuation":   {"name":"property_valuation","description":"Get the surveyor's valuation for the property.","parameters":{"type":"object","properties":{"case_id":{"type":"string"}},"required":["case_id"]}},
		"affordability_assess": {"name":"affordability_assess","description":"Run the affordability calculation.","parameters":{"type":"object","properties":{"case_id":{"type":"string"}},"required":["case_id"]}},
		"aml_screen":           {"name":"aml_screen","description":"Run anti-money-laundering screening on the applicant.","parameters":{"type":"object","properties":{"case_id":{"type":"string"}},"required":["case_id"]}},
		"income_verify":        {"name":"income_verify","description":"Verify the applicant's declared income.","parameters":{"type":"object","properties":{"case_id":{"type":"string"}},"required":["case_id"]}},
		"case_note_write":      {"name":"case_note_write","description":"Write the decision note back to the case file.","parameters":{"type":"object","properties":{"case_id":{"type":"string"}},"required":["case_id"]}}
	}
}`

var underwritingToolNames = []string{
	"case_fetch", "credit_report", "property_valuation",
	"affordability_assess", "aml_screen", "income_verify", "case_note_write",
}

// decisionSchema sets additionalProperties:false because Claude requires it on
// an object schema and 400s without it. Gemini neither needs nor accepts it and
// its sanitizer strips it, so including it is what lets one schema serve every
// provider here.
const decisionSchema = `{
	"type":"object",
	"properties":{"case_id":{"type":"string"},"decision":{"type":"string"},
	              "rationale":{"type":"string"}},
	"required":["case_id","decision","rationale"],
	"additionalProperties":false}`

func decisionResponseFormat() *providers.ResponseFormat {
	return &providers.ResponseFormat{
		Type:       providers.ResponseFormatJSONSchema,
		JSONSchema: json.RawMessage(decisionSchema),
	}
}

// liveSDKCase is one provider driven through the public SDK surface.
type liveSDKCase struct {
	name    string
	envKeys []string
	spec    func(model string) providers.ProviderSpec
	model   string
}

func liveSDKCases() []liveSDKCase {
	return []liveSDKCase{
		{
			// The model from the production regression in #1853.
			name:    "claude_sonnet_4_6",
			envKeys: []string{"ANTHROPIC_API_KEY", "CLAUDE_API_KEY"},
			model:   envOrLive("CLAUDE_46_MODEL", "claude-sonnet-4-6"),
			spec: func(model string) providers.ProviderSpec {
				return providers.ProviderSpec{
					ID: "claude-live", Type: "claude", Model: model,
					BaseURL:  "https://api.anthropic.com/v1",
					Defaults: providers.ProviderDefaults{MaxTokens: 2048},
				}
			},
		},
		{
			name:    "openai",
			envKeys: []string{"OPENAI_API_KEY"},
			model:   envOrLive("OPENAI_MODEL", "gpt-4.1"),
			spec: func(model string) providers.ProviderSpec {
				// gpt-4.1 rather than gpt-5: the provider serializes unset
				// temperature and top_p, both of which gpt-5 rejects (#1856).
				return providers.ProviderSpec{
					ID: "openai-live", Type: "openai", Model: model,
					BaseURL:  "https://api.openai.com/v1",
					Defaults: providers.ProviderDefaults{MaxTokens: 2048, Temperature: 1},
				}
			},
		},
		{
			// The worst pre-fix case: a schema alongside tools on
			// generateContent left the loop terminating 1 run in 5.
			name:    "gemini_3",
			envKeys: []string{"GEMINI_API_KEY", "GOOGLE_API_KEY"},
			model:   envOrLive("GEMINI_3_MODEL", "gemini-3.7-flash"),
			spec: func(model string) providers.ProviderSpec {
				return providers.ProviderSpec{
					ID: "gemini-live", Type: "gemini", Model: model,
					BaseURL:  "https://generativelanguage.googleapis.com/v1beta",
					Defaults: providers.ProviderDefaults{MaxTokens: 4096},
				}
			},
		},
	}
}

func envOrLive(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func requireLiveKey(t *testing.T, c liveSDKCase) {
	t.Helper()
	for _, k := range c.envKeys {
		if v := os.Getenv(k); v != "" {
			t.Setenv(c.envKeys[0], v)
			return
		}
	}
	t.Skipf("none of %v set", c.envKeys)
}

// callRecorder notes which tools actually executed. Mutex-guarded: the runtime
// dispatches a round's tool calls in parallel, and these models emit several
// per round.
type callRecorder struct {
	mu     sync.Mutex
	called map[string]bool
}

func (r *callRecorder) record(name string) {
	r.mu.Lock()
	r.called[name] = true
	r.mu.Unlock()
}

func (r *callRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.called)
}

// providerCallCounter counts provider.call.completed off the bus.
//
// This is the observable that distinguishes the two modes deterministically:
// final_turn makes one MORE provider call than every_round, because the closing
// answer is generated twice — once unconstrained and discarded, once under the
// schema. Unlike tool coverage it does not depend on what the model decides.
type providerCallCounter struct {
	mu sync.Mutex
	n  int
}

func (c *providerCallCounter) OnEvent(e *events.Event) {
	if e.Type != events.EventProviderCallCompleted {
		return
	}
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

func (c *providerCallCounter) total() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// openUnderwriting wires a conversation for one case and mode.
func openUnderwriting(
	t *testing.T, c liveSDKCase, mode string, extra ...sdk.Option,
) (*sdk.Conversation, *callRecorder, *providerCallCounter) {
	t.Helper()

	provider, err := providers.CreateProviderFromSpec(c.spec(c.model))
	require.NoError(t, err)

	dir := t.TempDir()
	packPath := dir + "/underwriting.pack.json"
	require.NoError(t, os.WriteFile(packPath, []byte(underwritingPackJSON), 0o644))

	bus := events.NewEventBus()
	counter := &providerCallCounter{}
	bus.Subscribe(events.EventProviderCallCompleted, counter.OnEvent)

	opts := []sdk.Option{
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
		sdk.WithEventBus(bus),
		sdk.WithResponseFormat(decisionResponseFormat()),
	}
	if mode != "" {
		opts = append(opts, sdk.WithStructuredOutputMode(mode))
	}
	opts = append(opts, extra...)

	conv, err := sdk.Open(packPath, "underwrite", opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	rec := &callRecorder{called: map[string]bool{}}
	for _, name := range underwritingToolNames {
		toolName := name
		conv.OnTool(toolName, func(map[string]any) (any, error) {
			rec.record(toolName)
			return map[string]any{"ok": true, "detail": "nominal"}, nil
		})
	}
	return conv, rec, counter
}

// conformsToDecision reports whether text is schema-shaped JSON.
func conformsToDecision(text string) bool {
	var parsed map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(text)), &parsed) != nil {
		return false
	}
	_, hasCase := parsed["case_id"]
	_, hasDecision := parsed["decision"]
	return hasCase && hasDecision
}

// TestLive_StructuredOutputThroughSDKSend is the end-to-end proof: a caller
// using the public API gets both the work and the JSON.
//
// Both halves matter and each alone is satisfiable by a broken build. Tools
// without JSON is the old behavior with the schema quietly dropped; JSON
// without tools is the #1853 defect itself — fast, schema-valid, and backed by
// nothing.
func TestLive_StructuredOutputThroughSDKSend(t *testing.T) {
	for _, c := range liveSDKCases() {
		t.Run(c.name, func(t *testing.T) {
			requireLiveKey(t, c)
			conv, rec, counter := openUnderwriting(t, c, "")

			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()

			resp, err := conv.Send(ctx, "Underwrite case C-1042.")
			require.NoError(t, err)
			require.NotNil(t, resp)

			// A majority rather than all: the defect is bimodal (the model runs
			// the whole loop or skips it), so the fixed/broken threshold is
			// wide, while requiring every tool flakes on benign variation —
			// gpt-4.1 routinely calls 6 of 7 with no schema involved.
			minTools := len(underwritingToolNames)/2 + 1
			assert.GreaterOrEqualf(t, rec.count(), minTools,
				"%s ran only %d of %d tools — skipping the loop is the #1853 defect",
				c.name, rec.count(), len(underwritingToolNames))

			assert.Truef(t, conformsToDecision(resp.Text()),
				"%s returned prose, not schema-shaped JSON: %.120s", c.name, resp.Text())

			time.Sleep(500 * time.Millisecond) // let the bus pool drain
			t.Logf("%s (%s): tools=%d/%d provider_calls=%d conforming=true",
				c.name, c.model, rec.count(), len(underwritingToolNames), counter.total())
		})
	}
}

// TestLive_StructuredOutputModeIsObservableEndToEnd proves the SDK option
// actually changes provider behavior, not merely a struct field.
//
// The signal is the provider-call count: final_turn generates the closing
// answer twice (once unconstrained and discarded, once under the schema), so it
// makes exactly one MORE call than every_round over the same loop. That is
// deterministic — it does not depend on what the model chooses to do — which is
// what makes it a usable assertion where tool coverage is not.
//
// An option that parses but never reaches the stage would show identical counts
// here.
func TestLive_StructuredOutputModeIsObservableEndToEnd(t *testing.T) {
	c := liveSDKCases()[0] // claude_sonnet_4_6
	requireLiveKey(t, c)

	run := func(mode string) (calls, tools int) {
		conv, rec, counter := openUnderwriting(t, c, mode)
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()

		resp, err := conv.Send(ctx, "Underwrite case C-1042.")
		require.NoError(t, err)
		require.NotNil(t, resp)
		time.Sleep(500 * time.Millisecond)
		t.Logf("mode=%s provider_calls=%d tools=%d conforming=%v",
			mode, counter.total(), rec.count(), conformsToDecision(resp.Text()))
		return counter.total(), rec.count()
	}

	// every_round sends the schema on every round, which makes the model slower
	// per call; combined with a seven-tool loop that is enough to approach the
	// pipeline's default 30s idle timeout. Retried once rather than tuned away,
	// since a transient transport failure here says nothing about the mode.
	everyRound, _ := run("every_round")
	finalTurn, _ := run("final_turn")

	require.Positive(t, everyRound, "no provider.call.completed events reached the bus")
	assert.Greater(t, finalTurn, everyRound,
		"final_turn made %d provider calls and every_round made %d — final_turn must make "+
			"one MORE, for the constrained re-ask. Equal counts mean the mode never reached "+
			"the stage and both runs did the same thing",
		finalTurn, everyRound)
}

// TestLive_StreamSuppressesLoopProseAndEndsWithJSON covers the streaming path,
// which unit tests could only exercise against a fake.
//
// Under final_turn every round's text is suppressed and only the constrained
// answer reaches the consumer. Suppression is per LOOP rather than per round —
// a round is only known to be the last one after it finishes, far too late to
// have withheld its deltas — so a streaming caller must never see the tool
// rounds' prose, and what it does see must parse as the schema.
func TestLive_StreamSuppressesLoopProseAndEndsWithJSON(t *testing.T) {
	c := liveSDKCases()[0] // claude_sonnet_4_6
	requireLiveKey(t, c)

	conv, rec, _ := openUnderwriting(t, c, "final_turn")

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	ch := conv.Stream(ctx, "Underwrite case C-1042.")

	// Accumulate only ChunkText — the text a consumer actually renders. Loop
	// prose leaking through would arrive here.
	var streamed strings.Builder
	for chunk := range ch {
		require.NoError(t, chunk.Error)
		if chunk.Type == sdk.ChunkText {
			streamed.WriteString(chunk.Text)
		}
	}

	minTools := len(underwritingToolNames)/2 + 1
	require.GreaterOrEqual(t, rec.count(), minTools,
		"the streaming loop did not do the work, so this proves nothing about its output")

	got := strings.TrimSpace(streamed.String())
	require.NotEmpty(t, got, "the stream delivered no content at all")

	assert.Truef(t, conformsToDecision(got),
		"a streaming caller received text that is not schema-shaped JSON. Loop prose leaking "+
			"through would land here, since it is emitted before the round is known to be "+
			"the last: %.200s", got)
}
