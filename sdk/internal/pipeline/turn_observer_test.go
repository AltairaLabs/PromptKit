package pipeline

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// observeTurns runs elems through a turn observer stage and returns what it
// reported (each report's continues) and what it forwarded.
func observeTurns(t *testing.T, elems ...stage.StreamElement) (reports []bool, forwarded int) {
	t.Helper()
	s := newTurnObserverStage(func(_ context.Context, continues bool) { reports = append(reports, continues) })
	in := make(chan stage.StreamElement, len(elems))
	out := make(chan stage.StreamElement, len(elems))
	for _, e := range elems {
		in <- e
	}
	close(in)
	require.NoError(t, s.Process(context.Background(), in, out))
	for range out {
		forwarded++
	}
	return reports, forwarded
}

func msgElem(role string, endOfStream bool) stage.StreamElement {
	return stage.StreamElement{Message: &types.Message{Role: role, Content: "x"}, EndOfStream: endOfStream}
}

// The stage reports the elements that end a reply the transcript holds, and
// no others, and forwards every element.
func TestTurnObserver_ReportsRecordedTurnEnds(t *testing.T) {
	held := msgElem("assistant", true)
	held.Meta.PlayoutHeld = true
	release := msgElem("assistant", false)
	release.Meta.PlayoutRelease = true
	release.Meta.PlayoutTurnEnd = true
	releasedUser := msgElem("user", false)
	releasedUser.Meta.PlayoutRelease = true
	releasedUser.Meta.PlayoutTurnEnd = true
	bargedIn := msgElem("assistant", false)
	bargedIn.Meta.Interrupted = true
	toolCall := msgElem("assistant", true)
	toolCall.Message.ToolCalls = []types.MessageToolCall{{ID: "c1", Name: "lookup"}}
	text := "hello"

	cases := []struct {
		name string
		elem stage.StreamElement
		want []bool
	}{
		{"a completed reply", msgElem("assistant", true), []bool{false}},
		{"a reply barge-in cut short", bargedIn, []bool{false}},
		{"a reply that calls tools", toolCall, []bool{true}},
		{"a held reply, not yet in the transcript", held, nil},
		{"a held reply released", release, []bool{false}},
		{"a cascaded turn's end", stage.NewEndOfTurnElement(), []bool{false}},
		{"a user message ending input", msgElem("user", true), nil},
		{"a released user message", releasedUser, nil},
		{"a reply message mid-stream", msgElem("assistant", false), nil},
		{"live text", stage.StreamElement{Text: &text}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reports, forwarded := observeTurns(t, tc.elem)
			assert.Equal(t, tc.want, reports)
			assert.Equal(t, 1, forwarded)
		})
	}
}
