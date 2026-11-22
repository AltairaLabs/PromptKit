package core

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func TestTurnFailureSignal_ReadsAndUnwrapsAsTheFailure(t *testing.T) {
	failure := NewStageError("provider", StageTypeGenerate, errors.New("model down"))
	elem := NewTurnFailureSignal(failure)

	require.Error(t, elem.Error)
	assert.Equal(t, failure.Error(), elem.Error.Error())
	assert.Same(t, failure, errors.Unwrap(elem.Error))
	assert.Equal(t, PriorityCritical, elem.Priority)
	assert.True(t, IsTurnFailureSignal(&elem))
}

func TestIsTurnFailureSignal_OnlyTheSignal(t *testing.T) {
	plain := NewErrorElement(errors.New("a stage's non-fatal report"))
	assert.False(t, IsTurnFailureSignal(&plain), "an ordinary error element is not the signal")

	text := NewTextElement("hi")
	assert.False(t, IsTurnFailureSignal(&text), "an element with no error is not the signal")

	wrapped := NewErrorElement(fmt.Errorf("relayed: %w", NewTurnFailureSignal(errors.New("x")).Error))
	assert.True(t, IsTurnFailureSignal(&wrapped), "the signal is found through wrapping")
}

func TestTagErrorSource_FirstStageWins(t *testing.T) {
	err := errors.New("boom")
	elem := NewErrorElement(err)
	assert.Equal(t, "", ErrorSource(&elem), "an untagged element has no source")

	TagErrorSource(&elem, "stt")
	assert.Equal(t, "stt", ErrorSource(&elem))

	TagErrorSource(&elem, "provider")
	assert.Equal(t, "stt", ErrorSource(&elem), "a later stage does not retag the error")
}

func TestTagErrorSource_ElementWithoutError(t *testing.T) {
	elem := NewTextElement("hi")
	TagErrorSource(&elem, "stt")
	elem.Error = errors.New("set later")
	assert.Equal(t, "", ErrorSource(&elem), "an element with no error is not tagged")
}

func TestErrorSource_ReplacedErrorIsRetaggedByItsStage(t *testing.T) {
	elem := NewErrorElement(errors.New("first"))
	TagErrorSource(&elem, "stt")

	elem.Error = errors.New("replacement")
	assert.Equal(t, "", ErrorSource(&elem), "the tag is stale once the error is replaced")

	TagErrorSource(&elem, "provider")
	assert.Equal(t, "provider", ErrorSource(&elem))
}

// uncomparableErr is an error whose dynamic type == would panic on.
type uncomparableErr struct{ parts []string }

func (e uncomparableErr) Error() string { return fmt.Sprint(e.parts) }

func TestErrorSource_UncomparableErrorDoesNotPanic(t *testing.T) {
	elem := NewErrorElement(uncomparableErr{parts: []string{"a"}})
	TagErrorSource(&elem, "stt")
	assert.Equal(t, "", ErrorSource(&elem), "an uncomparable error is never the same value")
}

func TestSameErrorValue(t *testing.T) {
	a, b := errors.New("a"), errors.New("a")
	assert.True(t, sameErrorValue(nil, nil))
	assert.False(t, sameErrorValue(a, nil))
	assert.False(t, sameErrorValue(nil, a))
	assert.True(t, sameErrorValue(a, a))
	assert.False(t, sameErrorValue(a, b), "equal text is not the same value")
	assert.False(t, sameErrorValue(a, uncomparableErr{}), "different dynamic types")
}

func TestForwardUpstreamErrors_SendsEachInOrder(t *testing.T) {
	elems := []StreamElement{NewErrorElement(errors.New("1")), NewErrorElement(errors.New("2"))}
	out := make(chan StreamElement, 2)

	require.NoError(t, ForwardUpstreamErrors(context.Background(), elems, out))
	close(out)
	var got []string
	for e := range out {
		got = append(got, e.Error.Error())
	}
	assert.Equal(t, []string{"1", "2"}, got)
}

func TestForwardUpstreamErrors_StopsWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := ForwardUpstreamErrors(ctx, []StreamElement{NewErrorElement(errors.New("1"))}, make(chan StreamElement))
	assert.ErrorIs(t, err, context.Canceled)
}

func TestSendStreamElement(t *testing.T) {
	out := make(chan StreamElement, 1)
	require.NoError(t, SendStreamElement(context.Background(), NewTextElement("hi"), out))
	assert.Equal(t, "hi", *(<-out).Text)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, SendStreamElement(ctx, NewTextElement("hi"), make(chan StreamElement)), context.Canceled)
}

func TestIsCut(t *testing.T) {
	interrupt := NewInterruptElement()
	assert.True(t, IsCut(&interrupt))

	interrupted := StreamElement{Meta: ElementMetadata{Interrupted: true}}
	assert.True(t, IsCut(&interrupted))

	text := NewTextElement("hi")
	assert.False(t, IsCut(&text))
}

func TestRealHoldClock(t *testing.T) {
	before := time.Now()
	now := RealHoldClock.Now()
	assert.False(t, now.Before(before), "Now reads the wall clock")

	select {
	case <-RealHoldClock.Until(time.Now().Add(-time.Second)):
	case <-time.After(time.Second):
		t.Fatal("Until a past deadline did not deliver at once")
	}
}

func TestStoreCallerTurn(t *testing.T) {
	shared := map[string]interface{}{"k": "v"}
	m := &types.Message{Role: RoleAssistant, Meta: shared}
	StoreCallerTurn(m, 3)
	assert.Equal(t, map[string]interface{}{"k": "v", types.MetaCallerTurn: 3}, m.Meta)
	assert.Equal(t, map[string]interface{}{"k": "v"}, shared, "the message gets a Meta of its own")

	user := &types.Message{Role: RoleUser}
	StoreCallerTurn(user, 3)
	assert.Nil(t, user.Meta, "only a reply records its caller turn")

	assert.NotPanics(t, func() { StoreCallerTurn(nil, 3) })
}
