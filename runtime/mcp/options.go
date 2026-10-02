package mcp

import (
	"errors"
	"time"
)

// ClientOptions configures MCP client behavior.
type ClientOptions struct {
	// RequestTimeout bounds each request (tools/list, tools/call). It does
	// not run out while the client is answering the server's own request
	// for user input: that time is the user's, not the server's. A
	// tools/call's timeout restarts each time the server reports progress on
	// it; the caller's context still bounds the call.
	RequestTimeout time.Duration
	// InitTimeout bounds connecting: starting a stdio server or reaching an
	// HTTP one, and agreeing the protocol.
	InitTimeout time.Duration
	// MaxRetries is the number of times connecting is retried after a
	// failure that is not the server's answer. tools/call is never retried:
	// a tool may have side effects.
	MaxRetries int
	// RetryDelay is the initial delay between retries (exponential backoff).
	RetryDelay time.Duration
	// EnableGracefulDegradation makes a failed tools/list yield no tools
	// rather than an error.
	EnableGracefulDegradation bool
	// MaxReconnectAttempts is how many times the client reconnects when its
	// connection is lost (a stdio server exits, an SSE stream ends) before
	// reporting the server unresponsive. 0 disables reconnection.
	MaxReconnectAttempts int
	// ElicitationHandler, when set, answers servers' requests for user input
	// and makes the client advertise the elicitation capability (form mode).
	// Without it the client does not advertise elicitation, and refuses
	// elicitation requests.
	ElicitationHandler ElicitationHandler
	// DisableModernProtocol skips stateless (2026-07-28) detection and always
	// uses the initialize handshake. For servers that misbehave when probed.
	DisableModernProtocol bool
	// EraProbeTimeout bounds how long the client waits for a stdio server to
	// answer the server/discover probe before restarting it with the
	// initialize handshake: a handshake-era server may never answer a
	// request it does not know. Defaults to 3s. A server slower than that to
	// start is treated as handshake-era; raise it for modern-only servers
	// with slow starts. HTTP servers always answer, so it does not apply.
	EraProbeTimeout time.Duration
	// Authorizer, when set, supplies credentials for an HTTP server and
	// handles its authorization challenges. The host implements it; see
	// Authorizer. Static credentials can go in ServerConfig.Headers instead.
	Authorizer Authorizer
}

// DefaultClientOptions returns sensible defaults.
func DefaultClientOptions() ClientOptions {
	return ClientOptions{
		RequestTimeout:            defaultRequestTimeout,
		InitTimeout:               defaultInitTimeout,
		MaxRetries:                defaultMaxRetries,
		RetryDelay:                defaultRetryDelay,
		EnableGracefulDegradation: true,
		MaxReconnectAttempts:      defaultMaxReconnectAttempts,
	}
}

const (
	defaultRequestTimeout       = 30 * time.Second
	defaultInitTimeout          = 10 * time.Second
	defaultMaxRetries           = 3
	defaultRetryDelay           = 100 * time.Millisecond
	defaultMaxReconnectAttempts = 3
	defaultEraProbeTimeout      = 3 * time.Second
)

var (
	// ErrClientNotInitialized is returned when attempting operations on uninitialized client
	ErrClientNotInitialized = errors.New("mcp: client not initialized")
	// ErrClientClosed is returned when attempting operations on closed client
	ErrClientClosed = errors.New("mcp: client closed")
	// ErrServerUnresponsive is returned when server doesn't respond
	ErrServerUnresponsive = errors.New("mcp: server unresponsive")
	// ErrProcessDied is returned when server process dies unexpectedly
	ErrProcessDied = errors.New("mcp: server process died")
)
