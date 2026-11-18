package gemini

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A context note goes to Gemini Live as content that does not complete the
// turn, so the model reads it and does not answer it.
func TestStreamContextNote_SendsContentWithoutCompletingTheTurn(t *testing.T) {
	url, msgs := recordLiveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := NewStreamSession(ctx, url, "k", &StreamSessionConfig{})
	require.NoError(t, err)
	defer s.Close()

	require.NoError(t, s.AddContextNote(ctx, "Your previous reply was withheld."))
	require.NoError(t, s.SendText(ctx, "done"))

	var raw []string
	select {
	case raw = <-msgs:
	case <-ctx.Done():
		t.Fatal("server never saw the end marker")
	}
	require.Len(t, raw, 1, "exactly the note before the marker")
	assert.JSONEq(t, `{"client_content":{"turns":[
		{"role":"user","parts":[{"text":"Your previous reply was withheld."}]}
	],"turn_complete":false}}`, raw[0])
}

// A closed session takes no note.
func TestStreamContextNote_ClosedSessionRefuses(t *testing.T) {
	s := &StreamSession{closed: true}
	assert.EqualError(t, s.AddContextNote(context.Background(), "note"), ErrSessionClosed)
}
