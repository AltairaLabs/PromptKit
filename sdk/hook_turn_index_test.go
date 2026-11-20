package sdk

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio/cueclip"
	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
)

// The turn index a host is given — on its provider hooks and on the guardrail
// validation events — is 0-based on every path: 0 for the conversation's first
// turn, 1 for the second, whether the turns are sent with Send, streamed with
// Stream, or typed into a realtime session (#2797).

// turnRecorder is a provider hook and a validation-event subscriber that
// records the TurnIndex each one is given.
type turnRecorder struct {
	mu     sync.Mutex
	before []int
	after  []int
	// events is keyed by event type and validator type.
	events map[string][]int
}

func (r *turnRecorder) Name() string { return "turn-recorder" }

func (r *turnRecorder) BeforeCall(_ context.Context, req *hooks.ProviderRequest) hooks.Decision {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.before = append(r.before, req.TurnIndex)
	return hooks.Allow
}

func (r *turnRecorder) AfterCall(_ context.Context, req *hooks.ProviderRequest, _ *hooks.ProviderResponse) hooks.Decision {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.after = append(r.after, req.TurnIndex)
	return hooks.Allow
}

// newTurnRecorder returns a recorder subscribed to the validation events of
// the bus it returns.
func newTurnRecorder(t *testing.T) (*turnRecorder, events.Bus) {
	t.Helper()
	r := &turnRecorder{events: map[string][]int{}}
	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Close() })
	for _, et := range []events.EventType{
		events.EventValidationStarted, events.EventValidationPassed, events.EventValidationFailed,
	} {
		bus.Subscribe(et, func(e *events.Event) {
			if d, ok := e.Data.(*events.ValidationEventData); ok {
				r.mu.Lock()
				key := string(e.Type) + "/" + d.ValidatorType
				r.events[key] = append(r.events[key], d.TurnIndex)
				r.mu.Unlock()
			}
		})
	}
	return r, bus
}

// hookCalls is the TurnIndex of each BeforeCall and AfterCall, in order.
func (r *turnRecorder) hookCalls() (before, after []int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.before), slices.Clone(r.after)
}

// eventTurns waits for n events of type et from validators of type vt, which
// the bus delivers asynchronously, and returns their TurnIndex values in
// ascending order.
func (r *turnRecorder) eventTurns(t *testing.T, et events.EventType, vt string, n int) []int {
	t.Helper()
	var got []int
	require.Eventually(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		got = slices.Clone(r.events[string(et)+"/"+vt])
		return len(got) >= n
	}, 5*time.Second, time.Millisecond, "%s from %s", et, vt)
	slices.Sort(got)
	return got
}

// zebraValidator checks input and output for a word no turn here says, so it
// passes on both sides of every turn and the hook chain runs to the end.
const zebraValidator = `{"type": "banned_words", "id": "no-zebra", "enabled": true,
	"message": "No zebras.", "params": {"words": ["Zebra"], "direction": "both"}}`

// jsonOnlyValidator fails every reply that is not JSON, so each turn records
// one validation.failed event.
const jsonOnlyValidator = `{"type": "json_valid", "id": "json-only", "enabled": true, "message": "Only JSON here."}`

// textTurn drives one text turn.
type textTurn func(t *testing.T, conv *Conversation)

var textDrives = map[string]textTurn{
	"Send": func(t *testing.T, conv *Conversation) {
		_, err := conv.Send(context.Background(), "who is in the family?")
		require.NoError(t, err)
	},
	"Stream": func(t *testing.T, conv *Conversation) {
		for chunk := range conv.Stream(context.Background(), "who is in the family?") {
			require.NoError(t, chunk.Error)
		}
	},
}

// allowFuncs are an input and an output func guardrail that allow every
// turn, so each reports validation.started and validation.passed per turn.
func allowFuncs() []Option {
	allow := func(context.Context, *hooks.InputRequest) hooks.Decision { return hooks.Allow }
	allowOut := func(context.Context, *hooks.OutputRequest) hooks.Decision { return hooks.Allow }
	return []Option{
		WithGuardrail(guardrails.InputFunc("allow-in", allow)),
		WithGuardrail(guardrails.OutputFunc("allow-out", allowOut)),
	}
}

// openTextTurns opens a text conversation on a pack with validator and drives
// two turns.
func openTextTurns(t *testing.T, validator string, drive textTurn, opts ...Option) *turnRecorder {
	t.Helper()
	rec, bus := newTurnRecorder(t)
	conv, err := Open(writeValidatorPack(t, validator), "main", append([]Option{
		WithProvider(brunoReplyProvider()), WithSkipSchemaValidation(),
		WithStateStore(statestore.NewMemoryStore()), WithEventBus(bus), WithProviderHook(rec),
	}, opts...)...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	drive(t, conv)
	drive(t, conv)
	return rec
}

// realtimeTurns opens a realtime session on a pack with validator and has the
// caller type two turns, each answered by the model.
func realtimeTurns(t *testing.T, validator string, opts ...Option) *turnRecorder {
	t.Helper()
	rec, bus := newTurnRecorder(t)
	s := openCueSession(t, writeValidatorPack(t, validator), spokenProvider(), append([]Option{
		WithCueVoice(nil, CueClipCache(cueclip.NewMemoryCache(1<<20))), WithEventBus(bus), WithProviderHook(rec),
	}, opts...)...)
	for i := range 2 {
		require.NoError(t, s.conv.SendText(context.Background(), "who is in the family?"))
		// The model answers once the text is sent, and a check that starts
		// while a reply streams places the input after that reply.
		require.Eventually(t, func() bool {
			before, _ := rec.hookCalls()
			return len(before) > i
		}, 5*time.Second, time.Millisecond)
		s.sess.EmitChunk(replyChunk("Let me"))
		s.sess.EmitChunk(endChunk())
		s.read(t, &cueRead{}, (*cueRead).ended)
	}
	return rec
}

// assertPassingTurns asserts a two-turn conversation whose guardrails passed
// both ways: the hooks, and the started and passed events of the pack's
// validator and of the func guardrails, one per direction per turn, report
// turns 0 and 1.
func assertPassingTurns(t *testing.T, rec *turnRecorder) {
	t.Helper()
	for _, vt := range []string{"banned_words", "func"} {
		for _, et := range []events.EventType{events.EventValidationStarted, events.EventValidationPassed} {
			assert.Equal(t, []int{0, 0, 1, 1}, rec.eventTurns(t, et, vt, 4), "%s from %s", et, vt)
		}
	}
	before, after := rec.hookCalls()
	assert.Equal(t, []int{0, 1}, before, "BeforeCall")
	assert.Equal(t, []int{0, 1}, after, "AfterCall")
}

func TestHookTurnIndex_TextPathIsZeroBased(t *testing.T) {
	for name, drive := range textDrives {
		t.Run(name+"/passing", func(t *testing.T) {
			assertPassingTurns(t, openTextTurns(t, zebraValidator, drive, allowFuncs()...))
		})
		t.Run(name+"/firing", func(t *testing.T) {
			rec := openTextTurns(t, jsonOnlyValidator, drive)
			assert.Equal(t, []int{0, 1}, rec.eventTurns(t, events.EventValidationFailed, "json_valid", 2))
		})
	}
}

func TestHookTurnIndex_RealtimePathIsZeroBased(t *testing.T) {
	t.Run("passing", func(t *testing.T) {
		assertPassingTurns(t, realtimeTurns(t, zebraValidator, allowFuncs()...))
	})
	t.Run("firing", func(t *testing.T) {
		rec := realtimeTurns(t, jsonOnlyValidator)
		assert.Equal(t, []int{0, 1}, rec.eventTurns(t, events.EventValidationFailed, "json_valid", 2))
	})
}
