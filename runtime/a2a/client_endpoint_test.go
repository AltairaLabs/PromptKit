package a2a

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// Following the card's interface must never make things worse than the
// {base}/a2a the client always used: behind a TLS-terminating proxy a server
// that derives its interface URL from the request describes itself as
// http://<internal host>/a2a.

// tlsAgent serves card behind TLS, as an agent behind a TLS-terminating
// proxy is reached, and returns a client for it that has discovered the card.
func tlsAgent(t *testing.T, card func(base string) AgentCard) (*cardAgent, *Client) {
	t.Helper()
	agent := &cardAgent{card: card}
	srv := httptest.NewTLSServer(agent)
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, WithHTTPClient(srv.Client()))
	_, err := c.Discover(context.Background())
	require.NoError(t, err)
	return agent, c
}

func singleInterface(url string) func(string) AgentCard {
	return func(string) AgentCard {
		return AgentCard{Name: "a", SupportedInterfaces: []AgentInterface{
			{URL: url, ProtocolBinding: ProtocolBindingJSONRPC, ProtocolVersion: "1.0"},
		}}
	}
}

func TestClient_DefaultPathKeepsTheCallersBaseURL(t *testing.T) {
	// An older PromptKit server behind a TLS-terminating proxy, no forwarded
	// headers: its card names the internal, plain-http address.
	agent, c := tlsAgent(t, singleInterface("http://internal.invalid:8080/a2a"))
	require.NoError(t, sendHi(t, c))
	assert.Equal(t, []string{"/a2a"}, agent.posted(), "the call reaches the caller's https base")

}

func TestCallURL(t *testing.T) {
	for _, tc := range []struct {
		name, base, declared, want string
	}{
		// The caller's scheme, host and port are authoritative; the card
		// contributes only a path on that same host.
		{"default path on another host keeps the base", "https://gw.example/agents/x",
			"http://internal.invalid:8080/a2a", "https://gw.example/agents/x/a2a"},
		{"default path on the same host keeps the base", "https://gw.example/agents/x",
			"https://gw.example/a2a", "https://gw.example/agents/x/a2a"},
		{"another host over https is not followed", "https://agents.example.com/x",
			"https://rpc.example.com/rpc", "https://agents.example.com/x/a2a"},
		{"a public https host is not followed from an internal base", "http://agent.internal:8080",
			"https://agent.example.com/rpc", "http://agent.internal:8080/a2a"},
		{"a distinct path on the same host is followed", "http://agent.example",
			"http://agent.example/", "http://agent.example/"},
		{"plain http on the same host keeps the caller's scheme", "https://agent.example",
			"http://agent.example/rpc", "https://agent.example/rpc"},
		{"another port on the same host keeps the caller's port", "https://agent.example:8443",
			"https://agent.example:9000/rpc", "https://agent.example:8443/rpc"},
		{"plain http on another host falls back", "https://agent.example",
			"http://elsewhere.example/rpc", "https://agent.example/a2a"},
		{"an unparseable interface falls back", "https://agent.example",
			"://nope", "https://agent.example/a2a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, NewClient(tc.base).callURL(tc.declared))
		})
	}
}

// An interface that is not followed lends nothing: its tenant and declared
// version belong to the other server.
func TestEndpoint_TenantAndVersionComeOnlyFromTheCallersHost(t *testing.T) {
	for _, tc := range []struct {
		name, base, iface   string
		wantURL, wantTenant string
		wantVersion         ProtocolVersion
	}{
		{"same host lends its tenant and version", "https://agent.example",
			"https://agent.example/rpc", "https://agent.example/rpc", "acme", ProtocolVersion03},
		{"another host lends neither", "https://agent.example",
			"https://other.example/rpc", "https://agent.example/a2a", "", ProtocolVersion10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClient(tc.base)
			c.useCard(&AgentCard{Name: "a", SupportedInterfaces: []AgentInterface{
				{URL: tc.iface, ProtocolBinding: ProtocolBindingJSONRPC, ProtocolVersion: "0.3", Tenant: "acme"},
			}})
			assert.Equal(t, tc.wantVersion, c.ProtocolVersion())

			url, iface, err := c.endpoint(c.ProtocolVersion())
			require.NoError(t, err)
			assert.Equal(t, tc.wantURL, url)
			tenant := ""
			if iface != nil {
				tenant = iface.Tenant
			}
			assert.Equal(t, tc.wantTenant, tenant)
			params, err := json.Marshal(GetTaskRequest{ID: "t"})
			require.NoError(t, err)
			if iface != nil && c.ProtocolVersion() != ProtocolVersion03 {
				params, err = withTenant(params, iface.Tenant)
				require.NoError(t, err)
			}
			if tc.wantTenant == "" {
				assert.NotContains(t, string(params), "tenant")
			}
		})
	}
}

func TestClient_AcceptsJSONRPCBindingAliases(t *testing.T) {
	agent := &cardAgent{card: func(base string) AgentCard {
		// A pre-1.0 PromptKit server's spelling of the binding.
		return AgentCard{Name: "a", SupportedInterfaces: []AgentInterface{
			{URL: base + "/rpc", ProtocolBinding: "jsonrpc+http", ProtocolVersion: "0.3"},
		}}
	}}
	srv := httptest.NewServer(agent)
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Discover(context.Background())
	require.NoError(t, err)
	assert.Equal(t, ProtocolVersion03, c.ProtocolVersion(), "the aliased interface declares the version")
	require.NoError(t, sendHi(t, c))
	assert.Equal(t, []string{"/rpc"}, agent.posted())

	for _, alias := range []string{"JSONRPC", "jsonrpc", "JSONRPC+HTTP", "json-rpc", "jsonrpc2"} {
		assert.True(t, IsJSONRPCBinding(alias), alias)
	}
	assert.False(t, IsJSONRPCBinding("GRPC"))
}

// slowCardAgent never answers a card fetch until released, and counts them.
type slowCardAgent struct {
	release chan struct{}
	mu      sync.Mutex
	fetches int
}

func (a *slowCardAgent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		a.mu.Lock()
		a.fetches++
		a.mu.Unlock()
		<-a.release
		http.NotFound(w, r)
		return
	}
	req := decodeRPC(r)
	rpcResult(w, req.ID, SendMessageResponse{Task: &Task{ID: "t", Status: TaskStatus{State: TaskStateCompleted}}})
}

func (a *slowCardAgent) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.fetches
}

func hangingCard(t *testing.T) (*slowCardAgent, *httptest.Server) {
	t.Helper()
	agent := &slowCardAgent{release: make(chan struct{})}
	srv := httptest.NewServer(agent)
	t.Cleanup(func() {
		close(agent.release)
		srv.Close()
	})
	return agent, srv
}

// returnsWithin runs f and reports whether it returned within d.
func returnsWithin(d time.Duration, f func()) bool {
	done := make(chan struct{})
	go func() {
		f()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

func TestExecutor_HangingCardDoesNotOutlastTheCallTimeout(t *testing.T) {
	_, srv := hangingCard(t)
	e := NewExecutor(WithNoRetry())
	defer e.Close()
	desc := &tools.ToolDescriptor{Name: "t", A2AConfig: &tools.A2AConfig{AgentURL: srv.URL, TimeoutMs: 20}}

	assert.True(t, returnsWithin(time.Second, func() {
		_, _ = e.Execute(context.Background(), desc, json.RawMessage(`{"query":"q"}`))
	}), "the call must end within its own timeout, not the discovery's")
}

func TestDiscoverForCalls_CancellationReleasesAWaiter(t *testing.T) {
	_, srv := hangingCard(t)
	c := NewClient(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	released := make(chan struct{})
	go func() {
		c.discoverForCalls(ctx)
		close(released)
	}()
	cancel()
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("a canceled caller kept waiting on discovery")
	}
}

func TestDiscoverForCalls_ConcurrentCallersShareOneFetchAndDoNotQueue(t *testing.T) {
	agent, srv := hangingCard(t)
	c := NewClient(srv.URL)

	patient, stop := context.WithCancel(context.Background())
	defer stop()
	go c.discoverForCalls(patient)
	require.Eventually(t, func() bool { return agent.count() == 1 }, time.Second, time.Millisecond)

	hurried, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	assert.True(t, returnsWithin(time.Second, func() { c.discoverForCalls(hurried) }),
		"a second caller is not stuck behind the first one's fetch")
	assert.Equal(t, 1, agent.count(), "one fetch in flight serves every caller")
}

func TestClient_NeverDowngradesToPlainHTTP(t *testing.T) {
	t.Run("same host keeps https", func(t *testing.T) {
		var host string
		agent, c := tlsAgent(t, func(base string) AgentCard {
			host = strings.TrimPrefix(base, "http://")
			return singleInterface("http://" + host + "/rpc")(base)
		})
		require.NoError(t, sendHi(t, c))
		assert.Equal(t, []string{"/rpc"}, agent.posted(), "the card's path, over the caller's https")
	})
	t.Run("another host falls back", func(t *testing.T) {
		agent, c := tlsAgent(t, singleInterface("http://elsewhere.invalid/rpc"))
		require.NoError(t, sendHi(t, c))
		assert.Equal(t, []string{"/a2a"}, agent.posted(), "credentials are never sent over plain http")
	})
}

func TestClient_DistinctPathIsFollowed(t *testing.T) {
	// a2a-python serves JSON-RPC at the root: the card adds information, so
	// it is followed — on the caller's host, which is the same one.
	var host string
	agent, c := tlsAgent(t, func(base string) AgentCard {
		host = strings.TrimPrefix(base, "http://")
		return singleInterface("https://" + host + "/")(base)
	})
	require.NoError(t, sendHi(t, c))
	assert.Equal(t, []string{"/"}, agent.posted())
}

func TestExecutor_BlackholedCardStillLeavesTimeForTheCall(t *testing.T) {
	agent, srv := hangingCard(t)
	e := NewExecutor(WithNoRetry())
	defer e.Close()
	desc := &tools.ToolDescriptor{Name: "t", A2AConfig: &tools.A2AConfig{AgentURL: srv.URL, TimeoutMs: 200}}

	_, err := e.Execute(context.Background(), desc, json.RawMessage(`{"query":"q"}`))
	require.NoError(t, err, "the discovery wait must not use up the call's deadline")
	assert.Equal(t, 1, agent.count())
}
