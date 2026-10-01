package a2a

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// The executor reaches agents whose JSON-RPC endpoint is not {base}/a2a
// (#2101): it discovers the card on first use, or takes the one a ToolBridge
// already fetched.

// gets counts the card fetches the agent has seen.
func (a *cardAgent) gets() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.headers)
}

func (a *cardAgent) posted() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string{}, a.paths...)
}

func rpcAtRoot(base string) AgentCard {
	return AgentCard{Name: "a", SupportedInterfaces: []AgentInterface{
		{URL: base + "/", ProtocolBinding: ProtocolBindingJSONRPC, ProtocolVersion: "1.0"},
	}}
}

func TestExecutor_DiscoversTheAgentEndpointOnce(t *testing.T) {
	agent := &cardAgent{card: rpcAtRoot}
	srv := httptest.NewServer(agent)
	defer srv.Close()

	e := NewExecutor(WithNoRetry())
	defer e.Close()
	desc := &tools.ToolDescriptor{Name: "t", A2AConfig: &tools.A2AConfig{AgentURL: srv.URL}}
	for i := 0; i < 2; i++ {
		_, err := e.Execute(context.Background(), desc, json.RawMessage(`{"query":"q"}`))
		require.NoError(t, err)
	}
	assert.Equal(t, []string{"/", "/"}, agent.posted(), "calls go where the card says")
	assert.Equal(t, 1, agent.gets(), "the card is fetched once per agent")
}

func TestExecutor_FallsBackWhenTheCardCannotBeFetched(t *testing.T) {
	var mu sync.Mutex
	var gets, posts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodGet {
			gets = append(gets, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		posts = append(posts, r.URL.Path)
		req := decodeRPC(r)
		rpcResult(w, req.ID, SendMessageResponse{Task: &Task{ID: "t", Status: TaskStatus{State: TaskStateCompleted}}})
	}))
	defer srv.Close()

	e := NewExecutor(WithNoRetry())
	defer e.Close()
	desc := &tools.ToolDescriptor{Name: "t", A2AConfig: &tools.A2AConfig{AgentURL: srv.URL}}
	for i := 0; i < 2; i++ {
		_, err := e.Execute(context.Background(), desc, json.RawMessage(`{"query":"q"}`))
		require.NoError(t, err)
	}
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"/a2a", "/a2a"}, posts)
	assert.Equal(t, []string{AgentCardPath, LegacyAgentCardPath}, gets, "a failed discovery is not repeated")
}

func TestToolBridge_SharesItsCardWithTheExecutor(t *testing.T) {
	agent := &cardAgent{card: func(base string) AgentCard {
		card := rpcAtRoot(base)
		card.Skills = []AgentSkill{{ID: "s", Name: "S"}}
		return card
	}}
	srv := httptest.NewServer(agent)
	defer srv.Close()

	bridge := NewToolBridge(NewClient(srv.URL))
	descs, err := bridge.RegisterAgent(context.Background())
	require.NoError(t, err)
	require.Len(t, descs, 1)
	require.Equal(t, 1, agent.gets())

	for _, shareFirst := range []bool{true, false} {
		e := NewExecutor(WithNoRetry())
		if !shareFirst {
			// A client the executor already holds takes the card too.
			e.getOrCreateClientWithConfig(descs[0].A2AConfig)
		}
		bridge.ShareCards(e)
		_, err = e.Execute(context.Background(), descs[0], json.RawMessage(`{"query":"q"}`))
		require.NoError(t, err)
		_ = e.Close()
	}
	assert.Equal(t, []string{"/", "/"}, agent.posted())
	assert.Equal(t, 1, agent.gets(), "the bridge's card is used; no second fetch")
}

func TestExecutor_RetriesAFailedDiscoveryLater(t *testing.T) {
	var mu sync.Mutex
	cardFetches := 0
	var posts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodGet {
			cardFetches++
			if cardFetches == 1 {
				w.WriteHeader(http.StatusServiceUnavailable) // a blip
				return
			}
			_ = json.NewEncoder(w).Encode(rpcAtRoot("http://" + r.Host))
			return
		}
		posts = append(posts, r.URL.Path)
		req := decodeRPC(r)
		rpcResult(w, req.ID, SendMessageResponse{Task: &Task{ID: "t", Status: TaskStatus{State: TaskStateCompleted}}})
	}))
	defer srv.Close()

	e := NewExecutor(WithNoRetry())
	defer e.Close()
	cfg := &tools.A2AConfig{AgentURL: srv.URL}
	e.getOrCreateClientWithConfig(cfg).discoverBackoff = time.Millisecond
	desc := &tools.ToolDescriptor{Name: "t", A2AConfig: cfg}

	_, err := e.Execute(context.Background(), desc, json.RawMessage(`{"query":"q"}`))
	require.NoError(t, err)
	time.Sleep(2 * time.Millisecond) // past the injected backoff
	for i := 0; i < 2; i++ {
		_, err = e.Execute(context.Background(), desc, json.RawMessage(`{"query":"q"}`))
		require.NoError(t, err)
	}

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"/a2a", "/", "/"}, posts, "a failed discovery is retried; then the card is used")
	assert.Equal(t, 2, cardFetches, "a successful discovery is kept")
}

func TestClient_DiscoveryIsNotBoundByTheCallersDeadline(t *testing.T) {
	agent := &cardAgent{card: rpcAtRoot}
	srv := httptest.NewServer(agent)
	defer srv.Close()

	c := NewClient(srv.URL)
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	c.discoverForCalls(expired)
	require.NoError(t, sendHi(t, c))
	assert.Equal(t, []string{"/"}, agent.posted(),
		"a short call deadline must not decide where every later call goes")
}
