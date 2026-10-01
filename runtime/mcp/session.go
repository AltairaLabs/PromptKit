package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/version"
)

// clientImplName identifies PromptKit in the clientInfo it sends.
const clientImplName = "promptkit"

// maxToolPages bounds tools/list pagination, so a server that never stops
// returning a nextCursor cannot hold the client in a loop.
const maxToolPages = 1000

// cancelNotifyTimeout bounds the best-effort cancellation notification send
// after a request times out.
const cancelNotifyTimeout = 2 * time.Second

// legacyProtocolVersions are the handshake-era revisions the client can
// speak, newest first. initialize offers the first; a server may answer with
// any of them.
var legacyProtocolVersions = []string{ProtocolVersion, "2025-06-18", "2025-03-26", "2024-11-05"}

// errRequestTimeout is the context cause set when a request outlives
// ClientOptions.RequestTimeout, so it can be told apart from the caller's own
// deadline or cancellation.
var errRequestTimeout = errors.New("mcp: request timeout")

// session is the MCP protocol spoken over one conn: the handshake, the
// negotiated version, request ids, timeouts and cancellation, retries, and
// answering requests the server makes of the client. Every transport's
// client delegates to one, so the protocol is implemented once.
type session struct {
	name string // server name, for logs
	opts ClientOptions
	conn conn

	nextID atomic.Int64

	// handshake serializes (re)initialization.
	handshake sync.Mutex

	mu         sync.RWMutex
	version    string // negotiated protocol version; "" before the handshake
	serverInfo *InitializeResponse
}

func newSession(name string, opts ClientOptions) *session {
	return &session{name: name, opts: opts}
}

// negotiatedVersion returns the protocol version agreed in the handshake.
func (s *session) negotiatedVersion() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.version
}

func (s *session) info() *InitializeResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.serverInfo
}

// clientCapabilities is what the client advertises. It declares only what
// the client implements: advertising a capability invites the server to use
// it.
func (s *session) clientCapabilities() ClientCapabilities {
	var caps ClientCapabilities
	if s.opts.ElicitationHandler != nil {
		caps.Elicitation = &ElicitationCapability{Form: &struct{}{}}
	}
	return caps
}

func clientInfo() Implementation {
	return Implementation{Name: clientImplName, Version: version.GetVersion()}
}

// initialize runs the legacy handshake: initialize, check the version the
// server chose, then notifications/initialized.
func (s *session) initialize(ctx context.Context) (*InitializeResponse, error) {
	s.handshake.Lock()
	defer s.handshake.Unlock()

	initCtx, cancel := context.WithTimeout(ctx, s.opts.InitTimeout)
	defer cancel()

	req := InitializeRequest{
		ProtocolVersion: legacyProtocolVersions[0],
		Capabilities:    s.clientCapabilities(),
		ClientInfo:      clientInfo(),
	}
	var resp InitializeResponse
	if err := s.call(initCtx, methodInitialize, req, &resp, callOpts{idempotent: true, noReinit: true}); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("initialization timeout after %v: %w", s.opts.InitTimeout, err)
		}
		return nil, fmt.Errorf("initialize request failed: %w", err)
	}
	// The server MAY answer with a different version; a client that does not
	// support it SHOULD disconnect rather than speak a protocol it doesn't know.
	if !slices.Contains(legacyProtocolVersions, resp.ProtocolVersion) {
		return nil, fmt.Errorf("mcp: server %s chose protocol version %q, which this client does not support (supports %v)",
			s.name, resp.ProtocolVersion, legacyProtocolVersions)
	}

	s.mu.Lock()
	s.version = resp.ProtocolVersion
	s.serverInfo = &resp
	s.mu.Unlock()

	// The lifecycle requires this notification before any other request;
	// servers may withhold capability-conditional tools until it arrives.
	// A failed send is logged, not fatal.
	if err := s.notify(initCtx, methodNotificationsInitialized, nil); err != nil {
		logger.Warn(msgInitializedNotifyFailed, "server", s.name, "error", err)
	}
	if h, ok := s.conn.(handshakeAware); ok {
		h.handshakeDone()
	}
	return &resp, nil
}

// handshakeAware is implemented by conns that act once a session exists.
type handshakeAware interface {
	handshakeDone()
}

// listTools returns every tool the server offers, following pagination.
func (s *session) listTools(ctx context.Context) ([]Tool, error) {
	var tools []Tool
	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < maxToolPages; page++ {
		var params any
		if cursor != "" {
			params = ToolsListRequest{Cursor: cursor}
		}
		var resp ToolsListResponse
		if err := s.call(ctx, methodToolsList, params, &resp, callOpts{idempotent: true}); err != nil {
			return nil, err
		}
		tools = append(tools, resp.Tools...)
		if resp.NextCursor == "" {
			return tools, nil
		}
		if seen[resp.NextCursor] {
			return nil, fmt.Errorf("mcp: server %s repeated tools/list cursor %q", s.name, resp.NextCursor)
		}
		seen[resp.NextCursor] = true
		cursor = resp.NextCursor
	}
	return nil, fmt.Errorf("mcp: server %s returned more than %d pages of tools", s.name, maxToolPages)
}

// listToolsDegrading is listTools with the client's graceful-degradation
// option applied: a failure yields an empty list rather than an error.
func (s *session) listToolsDegrading(ctx context.Context) ([]Tool, error) {
	tools, err := s.listTools(ctx)
	if err == nil {
		return tools, nil
	}
	if s.opts.EnableGracefulDegradation {
		logger.Warn("MCP tools/list failed, using graceful degradation", "server", s.name, "error", err)
		return []Tool{}, nil
	}
	return nil, fmt.Errorf("tools/list request failed: %w", err)
}

// callTool invokes a tool. It is never retried: a tool may have side effects,
// and a JSON-RPC error or a timeout does not mean it did not run.
func (s *session) callTool(ctx context.Context, name string, arguments json.RawMessage) (*ToolCallResponse, error) {
	var resp ToolCallResponse
	req := ToolCallRequest{Name: name, Arguments: arguments}
	if err := s.call(ctx, methodToolsCall, req, &resp, callOpts{}); err != nil {
		return nil, fmt.Errorf("tools/call request failed: %w", err)
	}
	return &resp, nil
}

// callOpts tunes one call.
type callOpts struct {
	// idempotent requests are retried on transport failures.
	idempotent bool
	// noReinit stops a session-expired failure from triggering a new
	// handshake (the handshake itself).
	noReinit bool
}

// call sends one request and decodes its result into out.
//
// Retries: only idempotent requests, and only on transport failures. A
// JSON-RPC error is the server's answer, and a timeout may have been
// processed; neither is retried.
func (s *session) call(ctx context.Context, method string, params, out any, o callOpts) error {
	raw, err := marshalParams(params)
	if err != nil {
		return err
	}
	reinitialized := false
	for attempt := 0; ; attempt++ {
		resp, err := s.roundTrip(ctx, method, raw)
		switch {
		case err == nil:
			return decodeResult(resp, out)
		case errors.Is(err, errSessionExpired) && !o.noReinit && !reinitialized:
			// The request was not processed: the server discarded the session
			// it was sent on. Start a new one and send it again.
			reinitialized = true
			logger.Info("MCP session expired, re-initializing", "server", s.name)
			if _, ierr := s.initialize(ctx); ierr != nil {
				return fmt.Errorf("mcp: re-initialize after session expiry: %w", ierr)
			}
			continue
		case o.idempotent && attempt < s.opts.MaxRetries && isTransportFailure(err):
			logger.Warn("MCP request failed, retrying",
				"server", s.name, "method", method, "attempt", attempt+1, "error", err)
			delay := s.opts.RetryDelay * time.Duration(1<<uint(attempt))
			if werr := sleepCtx(ctx, delay); werr != nil {
				return werr
			}
			continue
		default:
			return err
		}
	}
}

// roundTrip sends one request with a fresh id, applying the request timeout.
// If the client stops waiting — timeout or the caller's cancellation — the
// server is told the request is abandoned.
func (s *session) roundTrip(ctx context.Context, method string, params json.RawMessage) (*JSONRPCMessage, error) {
	req := &request{id: s.nextID.Add(1), method: method, params: params, header: s.requestHeader(method)}

	reqCtx, cancel := ctx, context.CancelFunc(func() {})
	if s.opts.RequestTimeout > 0 {
		reqCtx, cancel = context.WithTimeoutCause(ctx, s.opts.RequestTimeout, errRequestTimeout)
	}
	defer cancel()

	resp, err := s.conn.send(reqCtx, req)
	if err == nil {
		return resp, nil
	}
	if reqCtx.Err() == nil {
		return nil, err
	}
	if method != methodInitialize { // the spec forbids canceling initialize
		s.cancel(req.id, req.header)
	}
	if errors.Is(context.Cause(reqCtx), errRequestTimeout) && ctx.Err() == nil {
		return nil, fmt.Errorf("%w: request timeout after %v", ErrServerUnresponsive, s.opts.RequestTimeout)
	}
	return nil, ctx.Err()
}

// cancel sends a best-effort cancellation notification for a request the
// client has stopped waiting for.
func (s *session) cancel(id int64, header http.Header) {
	ctx, done := context.WithTimeout(context.Background(), cancelNotifyTimeout)
	defer done()
	s.conn.cancelRequest(ctx, id, "client stopped waiting for the response", header)
}

// requestHeader returns the per-request transport headers. After the
// handshake, HTTP requests carry the negotiated protocol version.
func (s *session) requestHeader(method string) http.Header {
	h := http.Header{}
	if v := s.negotiatedVersion(); v != "" && method != methodInitialize {
		h.Set(headerProtocolVersion, v)
	}
	return h
}

// notify sends a notification.
func (s *session) notify(ctx context.Context, method string, params any) error {
	raw, err := marshalParams(params)
	if err != nil {
		return err
	}
	return s.conn.notify(ctx, &request{method: method, params: raw, header: s.requestHeader(method)})
}

// serverRequest answers a request the server makes of the client. The client
// answers ping, and refuses every method it has not advertised a capability
// for with Method not found, so a server never waits on a request nobody
// will answer.
func (s *session) serverRequest(ctx context.Context, msg *JSONRPCMessage) *JSONRPCMessage {
	switch msg.Method {
	case methodPing:
		return replyTo(msg.ID, struct{}{}, nil)
	case methodElicitationCreate:
		res, rpcErr := s.elicit(ctx, msg.Params)
		if rpcErr != nil {
			return replyTo(msg.ID, nil, rpcErr)
		}
		return replyTo(msg.ID, res, nil)
	}
	logger.Debug("MCP refusing unsupported server request", "server", s.name, "method", msg.Method)
	return replyTo(msg.ID, nil, &JSONRPCError{Code: codeMethodNotFound, Message: "Method not found: " + msg.Method})
}

// notification handles a server notification.
func (s *session) notification(msg *JSONRPCMessage) {
	logger.Debug("MCP server notification", "server", s.name, "method", msg.Method)
}

// decodeResult turns a response into out, or into an *RPCError.
func decodeResult(resp *JSONRPCMessage, out any) error {
	if resp.Error != nil {
		return rpcErrorFrom(resp.Error)
	}
	if out == nil || len(resp.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(resp.Result, out); err != nil {
		return fmt.Errorf("failed to unmarshal result: %w", err)
	}
	return nil
}

// isTransportFailure reports whether err is a failure to exchange messages
// at all — not the server's answer, and not the client giving up.
func isTransportFailure(err error) bool {
	var rpcErr *RPCError
	switch {
	case errors.As(err, &rpcErr),
		errors.Is(err, context.Canceled),
		errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, ErrServerUnresponsive),
		errors.Is(err, ErrClientClosed):
		return false
	}
	return true
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
