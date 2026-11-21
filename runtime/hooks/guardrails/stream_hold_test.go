package guardrails

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// scriptedJudge answers every call with pass, recording what it was asked;
// with block set it answers only when its context ends.
type scriptedJudge struct {
	pass  bool
	block bool
	opts  []handlers.JudgeOpts
}

//nolint:gocritic // JudgeOpts is passed by value by the JudgeProvider interface
func (j *scriptedJudge) Judge(ctx context.Context, opts handlers.JudgeOpts) (*handlers.JudgeResult, error) {
	j.opts = append(j.opts, opts)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if j.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	score := 0.0
	if j.pass {
		score = 1
	}
	return &handlers.JudgeResult{Passed: j.pass, Score: score, Reasoning: "scripted"}, nil
}

// streamHoldOf builds an output guardrail and returns its stream_hold mode.
func streamHoldOf(t *testing.T, evalType string, params map[string]any) hooks.StreamHoldMode {
	t.Helper()
	h, err := Output(evalType, params, WithJudge(&scriptedJudge{pass: true})).Hook()
	require.NoError(t, err)
	sh, ok := h.(hooks.StreamHolder)
	require.True(t, ok, "an eval-backed guardrail reports its stream hold")
	return sh.StreamHold()
}

// Without stream_hold, topic, policy and PII-style judges hold per segment, a
// free-form llm_judge holds the turn, a chunk check that can hold a banned
// word's start holds its tail, and every other check holds nothing.
func TestStreamHold_DefaultModePerHandler(t *testing.T) {
	cases := map[string]struct {
		params map[string]any
		want   hooks.StreamHoldMode
	}{
		"llm_judge":        {map[string]any{"criteria": "c"}, hooks.StreamHoldTurn},
		"toxicity":         {nil, hooks.StreamHoldSegment},
		"bias":             {nil, hooks.StreamHoldSegment},
		"role_violation":   {nil, hooks.StreamHoldSegment},
		"pii_leakage":      {nil, hooks.StreamHoldSegment},
		"content_excludes": {map[string]any{"patterns": []any{"x"}}, hooks.StreamHoldTail},
		"max_length":       {map[string]any{"max_characters": 10}, hooks.StreamHoldNone},
		"contains_any":     {map[string]any{"patterns": []any{"x"}}, hooks.StreamHoldNone},
	}
	for evalType, tc := range cases {
		t.Run(evalType, func(t *testing.T) {
			assert.Equal(t, tc.want, streamHoldOf(t, evalType, tc.params))
		})
	}
}

// An input guardrail holds no reply, whatever its handler.
func TestStreamHold_InputGuardrailHoldsNothing(t *testing.T) {
	h, err := Input("toxicity", nil, WithJudge(&scriptedJudge{pass: true})).Hook()
	require.NoError(t, err)
	assert.Equal(t, hooks.StreamHoldNone, h.(hooks.StreamHolder).StreamHold())

	_, err = Input("toxicity", map[string]any{"stream_hold": "turn"}, WithJudge(&scriptedJudge{})).Hook()
	require.ErrorIs(t, err, ErrInvalidGuardrailParams)
	assert.Contains(t, err.Error(), "an input guardrail holds no reply")
}

// stream_hold names the mode, overriding the default; the handler never sees
// it, so a handler that rejects unknown params accepts the guardrail.
func TestStreamHold_ExplicitMode(t *testing.T) {
	assert.Equal(t, hooks.StreamHoldSegment,
		streamHoldOf(t, "llm_judge", map[string]any{"criteria": "c", "stream_hold": "segment"}))
	assert.Equal(t, hooks.StreamHoldTurn,
		streamHoldOf(t, "toxicity", map[string]any{"stream_hold": "turn", "stream_hold_timeout": "2s"}))
	assert.Equal(t, hooks.StreamHoldNone,
		streamHoldOf(t, "content_excludes", map[string]any{"patterns": []any{"x"}, "stream_hold": "none"}))
	assert.Equal(t, hooks.StreamHoldTail,
		streamHoldOf(t, "content_excludes", map[string]any{"patterns": []any{"x"}, "stream_hold": "tail"}))
}

// A stream_hold the guardrail cannot honor, or cannot read, does not build.
func TestStreamHold_InvalidParamsAreConstructionErrors(t *testing.T) {
	cases := []struct {
		name     string
		evalType string
		params   map[string]any
		want     string
	}{
		{"unknown mode", "toxicity", map[string]any{"stream_hold": "later"}, `stream_hold "later" is not one of`},
		{"non-string mode", "toxicity", map[string]any{"stream_hold": 3}, `stream_hold "3" is not one of`},
		{"segment needs a partial judge", "contains_any",
			map[string]any{"patterns": []any{"x"}, "stream_hold": "segment"}, "cannot judge a partial reply"},
		{"tail needs a chunk check", "toxicity", map[string]any{"stream_hold": "tail"}, "does not judge chunks"},
		{"unreadable timeout", "toxicity", map[string]any{"stream_hold_timeout": "soon"}, `stream_hold_timeout "soon"`},
		{"zero timeout", "toxicity", map[string]any{"stream_hold_timeout": 0}, "must be positive"},
		{"timeout of the wrong type", "toxicity", map[string]any{"stream_hold_timeout": true}, "must be a duration"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Output(tc.evalType, tc.params, WithJudge(&scriptedJudge{})).Hook()
			require.ErrorIs(t, err, ErrInvalidGuardrailParams)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// A partial reply is judged as one: the judge is told it may be incomplete. A
// firing carries the text that replaces the reply and where in the reply it
// fired.
func TestStreamHold_JudgePartialReply(t *testing.T) {
	judge := &scriptedJudge{pass: false}
	h, err := Output("llm_judge", map[string]any{"criteria": "Only the weather."},
		WithJudge(judge), WithMessage("Weather only.")).Hook()
	require.NoError(t, err)

	d := h.(hooks.StreamHolder).JudgePartialReply(context.Background(),
		&hooks.ProviderRequest{Messages: userTurn("weather?")},
		hooks.PartialReply{Text: "Sunny. Buy stocks.", SegmentIndex: 2, Timeout: time.Second})

	require.Len(t, judge.opts, 1)
	assert.Equal(t, "Sunny. Buy stocks.", judge.opts[0].Content)
	assert.Contains(t, judge.opts[0].Criteria, "Only the weather.")
	assert.Contains(t, judge.opts[0].Criteria, "may be incomplete")
	assert.False(t, d.Allow)
	assert.True(t, d.Enforced)
	require.NotNil(t, d.Content)
	assert.Equal(t, "Weather only.", *d.Content)
	assert.Equal(t, 2, d.Metadata[hooks.MetadataKeySegmentIndex])
	assert.Equal(t, len("Sunny. Buy stocks."), d.Metadata[hooks.MetadataKeySegmentEnd])

	judge.pass = true
	d = h.(hooks.StreamHolder).JudgePartialReply(context.Background(),
		&hooks.ProviderRequest{}, hooks.PartialReply{Text: "Sunny.", SegmentIndex: 1})
	assert.Equal(t, hooks.Allow, d, "with no bound set, the guardrail's eval timeout bounds the judgment")
}

// A partial-reply judgment is bounded by the guardrail's stream_hold_timeout
// when it sets one, else by the host's, and fails closed past it.
func TestStreamHold_JudgePartialReplyTimeout(t *testing.T) {
	// The bound that must not apply is short too, so a judgment bounded by
	// the wrong one fails here in a second rather than hanging the test; it
	// is told apart from the right one by how long the judgment took.
	const right, wrong = 5 * time.Millisecond, time.Second
	judge := &scriptedJudge{block: true}
	own, err := Output("llm_judge", map[string]any{"criteria": "c", "stream_hold_timeout": "5ms"},
		WithJudge(judge), WithEvalTimeout(wrong)).Hook()
	require.NoError(t, err)
	start := time.Now()
	d := own.(hooks.StreamHolder).JudgePartialReply(context.Background(),
		&hooks.ProviderRequest{}, hooks.PartialReply{Text: "Sunny.", SegmentIndex: 1, Timeout: wrong})
	require.Less(t, time.Since(start), wrong/2, "the guardrail's stream_hold_timeout (%v) bounds it", right)
	assert.False(t, d.Allow)
	assert.Equal(t, "timeout", d.Metadata["reason"])

	hosts, err := Output("llm_judge", map[string]any{"criteria": "c"}, WithJudge(judge), WithEvalTimeout(wrong)).Hook()
	require.NoError(t, err)
	start = time.Now()
	d = hosts.(hooks.StreamHolder).JudgePartialReply(context.Background(),
		&hooks.ProviderRequest{}, hooks.PartialReply{Text: "Sunny.", SegmentIndex: 1, Timeout: right})
	require.Less(t, time.Since(start), wrong/2, "the host's bound (%v) bounds it, not the eval timeout", right)
	assert.False(t, d.Allow)
	assert.Equal(t, "timeout", d.Metadata["reason"])
}

// An input guardrail does not judge a reply.
func TestStreamHold_InputGuardrailJudgesNoPartialReply(t *testing.T) {
	judge := &scriptedJudge{}
	h, err := Input("toxicity", nil, WithJudge(judge)).Hook()
	require.NoError(t, err)
	d := h.(hooks.StreamHolder).JudgePartialReply(context.Background(),
		&hooks.ProviderRequest{}, hooks.PartialReply{Text: "x", SegmentIndex: 1})
	assert.True(t, d.Allow)
	assert.Empty(t, judge.opts)
}

// stream_hold: none on a chunk check holds no tail back: a possible banned
// word's start goes out, and the check still blocks the word.
func TestStreamHold_NoneHoldsNoTail(t *testing.T) {
	h, err := Output("content_excludes", map[string]any{"patterns": []any{"pineapple"}, "stream_hold": "none"}).Hook()
	require.NoError(t, err)
	ci := h.(hooks.ChunkRequestInterceptor)

	d := ci.InterceptChunk(context.Background(), hooks.ChunkRequest{Chunk: &providers.StreamChunk{Content: "I like pine"}})
	assert.Equal(t, hooks.Decision{Allow: true}, d)

	d = ci.InterceptChunk(context.Background(), hooks.ChunkRequest{Chunk: &providers.StreamChunk{Content: "I like pineapple"}})
	assert.False(t, d.Allow)
}

// stream_hold_timeout reads as a duration string or a number of seconds.
func TestParseStreamHoldTimeout(t *testing.T) {
	for raw, want := range map[any]time.Duration{
		"250ms": 250 * time.Millisecond, 2: 2 * time.Second, int64(3): 3 * time.Second, 1.5: 1500 * time.Millisecond,
	} {
		got, err := parseStreamHoldTimeout(raw)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}
