package sdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
)

// The executor bridge tools run on gets the card each bridge discovered, so
// calls reach the interface the card declares without a second card fetch
// (#2101).
func TestA2ACapability_BridgeExecutorUsesTheDiscoveredCard(t *testing.T) {
	var mu sync.Mutex
	var gets int
	var posts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodGet {
			gets++
			_ = json.NewEncoder(w).Encode(a2a.AgentCard{
				Name:   "agent",
				Skills: []a2a.AgentSkill{{ID: "s", Name: "S"}},
				SupportedInterfaces: []a2a.AgentInterface{{
					URL: "http://" + r.Host + "/rpc", ProtocolBinding: a2a.ProtocolBindingJSONRPC, ProtocolVersion: "1.0",
				}},
			})
			return
		}
		posts = append(posts, r.URL.Path)
		var req a2a.JSONRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		result, _ := json.Marshal(a2a.SendMessageResponse{Task: &a2a.Task{
			ID: "t", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
		}})
		_ = json.NewEncoder(w).Encode(a2a.JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
	}))
	defer srv.Close()

	bridge := a2a.NewToolBridge(a2a.NewClient(srv.URL))
	c := NewA2ACapability()
	c.agentBridges = []*a2a.ToolBridge{bridge}
	c.agentSettings = map[*a2a.ToolBridge]a2aBridgeSettings{bridge: {url: srv.URL}}
	require.NoError(t, c.discoverAgents())
	descs := bridge.GetToolDescriptors()
	require.Len(t, descs, 1)

	exec := c.bridgeExecutor()
	if closer, ok := exec.(interface{ Close() error }); ok {
		defer func() { _ = closer.Close() }()
	}
	_, err := exec.Execute(context.Background(), descs[0], json.RawMessage(`{"query":"q"}`))
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"/rpc"}, posts, "the call goes where the card says")
	assert.Equal(t, 1, gets, "the bridge's card is reused, not fetched again")
}
