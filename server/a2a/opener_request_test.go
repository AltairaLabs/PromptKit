package a2aserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// NewServerFor gives the opener the request that opens each context, once per
// context: the caller the host identified on it opens that conversation.
func TestNewServerFor_OpenerSeesTheRequestOpeningEachContext(t *testing.T) {
	var mu sync.Mutex
	opened := map[string]string{} // context ID -> caller header of the opening request
	opens := 0
	opener := func(r *http.Request, contextID string) (Conversation, error) {
		mu.Lock()
		defer mu.Unlock()
		opens++
		opened[contextID] = r.Header.Get(callerHeader)
		return &mockConv{sendFunc: func(context.Context, any) (SendResult, error) {
			return &mockSendResult{parts: []types.ContentPart{types.NewTextPart("ok")}, text: "ok"}, nil
		}}, nil
	}
	srv := NewServerFor(opener, WithTaskOwner(ownerFromHeader))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	defer func() { _ = srv.Shutdown(context.Background()) }()

	for _, msg := range []struct{ caller, contextID string }{
		{"alice", "ctx-alice"}, {"bob", "ctx-bob"}, {"alice", "ctx-alice"},
	} {
		resp := as(t, ts, msg.caller, a2a.MethodV1SendMessage, a2a.SendMessageRequest{Message: userMessage(msg.contextID)})
		require.Nil(t, resp.Error, "%s into %s", msg.caller, msg.contextID)
	}

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, map[string]string{"ctx-alice": "alice", "ctx-bob": "bob"}, opened)
	assert.Equal(t, 2, opens, "a context's conversation is opened once")
}
