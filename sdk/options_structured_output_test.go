package sdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
)

// configFromOptions runs option functions over a fresh config.
//
// Named to avoid the existing applyOptions in sdk.go, which has a different
// signature.
func configFromOptions(t *testing.T, opts ...Option) *config {
	t.Helper()
	cfg := &config{}
	for _, opt := range opts {
		require.NoError(t, opt(cfg))
	}
	return cfg
}

// TestWithStructuredOutputMode_ParsesAndDefaults covers the option itself.
//
// An unrecognized value must fall back to the default rather than being
// forwarded: guessing here silently changes whether a caller's schema
// constrains tool-calling rounds, which is the behavior #1853 is about.
func TestWithStructuredOutputMode_ParsesAndDefaults(t *testing.T) {
	assert.Equal(t, stage.StructuredOutputEveryRound,
		configFromOptions(t, WithStructuredOutputMode("every_round")).structuredOutputMode)
	assert.Equal(t, stage.StructuredOutputFinalTurn,
		configFromOptions(t, WithStructuredOutputMode("final_turn")).structuredOutputMode)

	// Unrecognized and unset both leave the zero value, which resolves to
	// final_turn in the stage.
	assert.Equal(t, stage.StructuredOutputMode(""),
		configFromOptions(t, WithStructuredOutputMode("nonsense")).structuredOutputMode)
	assert.Equal(t, stage.StructuredOutputMode(""),
		configFromOptions(t).structuredOutputMode)
}

// TestStructuredOutputMode_ReachesThePipelineConfig is the wiring test.
//
// An option that parses correctly but never reaches the stage is inert — the
// most common latent defect in this repo: declared vocabulary, a complete
// consumer, and no producer. Asserting on the config the pipeline is actually
// built from is what distinguishes "the option exists" from "the option works".
//
// buildPipelineConfig is shared by buildPipelineWithParams and
// buildStreamPipelineWithParams, so covering it covers both the unary and
// duplex paths — the two-path split being the other recurring bug shape here.
func TestStructuredOutputMode_ReachesThePipelineConfig(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []Option
		want stage.StructuredOutputMode
	}{
		{"explicit every_round", []Option{WithStructuredOutputMode("every_round")},
			stage.StructuredOutputEveryRound},
		{"explicit final_turn", []Option{WithStructuredOutputMode("final_turn")},
			stage.StructuredOutputFinalTurn},
		{"unset stays zero and resolves to final_turn in the stage", nil,
			stage.StructuredOutputMode("")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Minimum viable Conversation: buildPipelineConfig dereferences
			// the tool registry and the prompt, neither of which this test
			// cares about.
			conv := &Conversation{
				config:       configFromOptions(t, tc.opts...),
				toolRegistry: tools.NewRegistry(),
				prompt:       &pack.Prompt{},
			}
			cfg := conv.buildPipelineConfig(nil, "conv-1", nil, nil)

			require.NotNil(t, cfg)
			assert.Equal(t, tc.want, cfg.StructuredOutputMode,
				"the option did not reach the pipeline config; a mode that never arrives "+
					"leaves every turn on the default regardless of what the caller asked for")
		})
	}
}
