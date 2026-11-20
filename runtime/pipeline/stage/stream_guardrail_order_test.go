package stage_test

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// scriptedTurnRepo answers every request with one turn.
type scriptedTurnRepo struct{ turn mock.Turn }

func (r *scriptedTurnRepo) GetTurn(context.Context, mock.ResponseParams) (*mock.Turn, error) {
	t := r.turn
	return &t, nil
}

func (r *scriptedTurnRepo) GetResponse(context.Context, mock.ResponseParams) (string, error) {
	return strings.Join(r.turn.StreamChunks, ""), nil
}

// A streamed reply an output guardrail cuts reaches a speech-out stage as
// deltas it skips and one completed message: the stage speaks the guardrail's
// message, once, and never any of the blocked reply.
func TestProviderStage_CutStreamIsSpokenOnceAsTheGuardrailMessage(t *testing.T) {
	const msg = "Let's talk about something else."
	guard, err := guardrails.NewGuardrailHook("content_excludes",
		map[string]any{"patterns": []any{"pineapple"}}, guardrails.WithMessage(msg))
	require.NoError(t, err)
	prov := mock.NewProviderWithRepository("p", "m", false,
		&scriptedTurnRepo{turn: mock.Turn{Type: "text", StreamChunks: []string{"Say P", "ine", "apple, please."}}})
	ps := stage.NewProviderStageWithHooks(prov, nil, nil, &stage.ProviderConfig{MaxTokens: 100}, nil,
		hooks.NewRegistry(hooks.WithProviderHook(guard)))

	user := types.Message{Role: "user", Content: "what goes on pizza?"}
	elems := runStage(t, ps, []stage.StreamElement{stage.NewMessageElement(&user)}, 5*time.Second)

	var deltas []string
	for _, e := range elems {
		if e.Text != nil {
			deltas = append(deltas, *e.Text)
		}
	}
	assert.Equal(t, []string{"Say ", msg}, deltas, "the text the caller sees")

	var mu sync.Mutex
	var spoken []string
	svc := &mockTTSService{synthesizeFunc: func(_ context.Context, text string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
		mu.Lock()
		spoken = append(spoken, text)
		mu.Unlock()
		return io.NopCloser(strings.NewReader(generateTestPCM(10))), nil
	}}
	runStage(t, stage.NewTTSStageWithInterruption(svc, stage.DefaultTTSStageWithInterruptionConfig()), elems, 5*time.Second)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{msg}, spoken)
}
