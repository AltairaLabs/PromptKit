package a2a

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	// The same holds when the server was told its scheme but not the
	// public host: the default path adds nothing, so the base URL stands.
	agent, c = tlsAgent(t, singleInterface("https://internal.invalid/a2a"))
	require.NoError(t, sendHi(t, c))
	assert.Equal(t, []string{"/a2a"}, agent.posted())
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
