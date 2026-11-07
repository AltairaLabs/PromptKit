package sdk

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pack"
)

// invalidEvalPackJSON holds one usable eval and three that ValidateEvals
// rejects but the schema check (skipped here) would not catch on its own: an
// unknown trigger, a metric name Prometheus cannot register, and a duplicate id.
const invalidEvalPackJSON = `{
	"$schema": "https://promptpack.org/schema/2025.1/promptpack.schema.json",
	"schema_version": "2025.1",
	"id": "invalid-eval-pack",
	"version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "handlebars", "features": []},
	"evals": [
		{"id": "good", "type": "wf_turn_stub", "trigger": "every_turn"},
		{"id": "typo", "type": "wf_turn_stub", "trigger": "every_turns"},
		{"id": "bad_metric", "type": "wf_turn_stub", "trigger": "every_turn",
		 "metric": {"name": "bad-metric-name", "type": "gauge"}},
		{"id": "good", "type": "wf_step_stub", "trigger": "every_turn"}
	],
	"prompts": {
		"chat": {
			"id": "chat", "name": "Chat", "description": "Chat",
			"version": "1.0.0", "system_template": "You chat.",
			"evals": [
				{"id": "", "type": "wf_turn_stub", "trigger": "every_turn"}
			]
		}
	}
}`

// Open runs ValidateEvals over the pack's and the prompt's evals. A def it
// rejects is skipped with a warning that names it and the reason, and the
// usable ones still run.
func TestOpen_ValidatesEvalDefs(t *testing.T) {
	logs := captureLogs(t)
	bus, col, registry, step := newStepEvalHarness(t)
	packPath := writeWorkflowTestPack(t, invalidEvalPackJSON)

	c, err := Open(packPath, "chat",
		WithSkipSchemaValidation(),
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithEvalRegistry(registry),
		WithEventBus(bus),
	)
	require.NoError(t, err, "an unusable eval degrades reporting; it does not fail Open")

	_, err = c.Send(context.Background(), "hello")
	require.NoError(t, err)
	require.NoError(t, c.Close())
	require.Eventually(t, func() bool { return len(col.byTrigger(evals.TriggerEveryTurn)) >= 1 },
		5*time.Second, 5*time.Millisecond)
	time.Sleep(20 * time.Millisecond)

	got := col.byTrigger(evals.TriggerEveryTurn)
	require.Len(t, got, 1, "only the first usable def runs: %+v", got)
	assert.Equal(t, "good", got[0].EvalID)
	assert.Empty(t, step.transcripts(), "the duplicate id must not run")

	out := logs.String()
	assert.Contains(t, out, `invalid trigger \"every_turns\"`)
	assert.Contains(t, out, "bad-metric-name")
	assert.Contains(t, out, `duplicate eval id \"good\"`)
	assert.Contains(t, out, "prompt:chat evals[0]: id is required")
}

// A host that passes its own runner and no registry (WithEvalRunner alone)
// gets no type filtering, so the structural check is what drops an unusable
// `when` before it reaches the runner.
func TestNewEvalMiddleware_DropsUnusableWhenWithRunnerOnly(t *testing.T) {
	conv := &Conversation{
		config: &config{evalRunner: evals.NewEvalRunner(evals.NewEvalTypeRegistry())},
		pack: &pack.Pack{Pack: packspec.Pack{
			Evals: []*evals.EvalDef{
				{ID: "good", Type: "contains", Trigger: evals.TriggerEveryTurn},
				{ID: "gated", Type: "contains", Trigger: evals.TriggerEveryTurn,
					When: map[string]any{"bogus_condition": true}},
			},
		}},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	require.NotNil(t, mw)
	defer mw.close()
	require.Len(t, mw.defs, 1)
	assert.Equal(t, "good", mw.defs[0].ID)
}
