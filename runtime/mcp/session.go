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
var legacyProtocolVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

// errRequestTimeout is the context cause set when a request outlives
// ClientOptions.RequestTimeout, so it can be told apart from the caller's own
// deadline or cancellation.
var errRequestTimeout = errors.New("mcp: request timeout")

// session is the MCP protocol spoken over one conn: which era and version
// the server speaks, request ids, timeouts and cancellation, retries, and
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
	era        era
	version    string // the protocol version in use; "" before it is agreed
	serverInfo *InitializeResponse
	// toolHeaders caches each tool's x-mcp-header designations from the
	// last tools/list (modern era), for mirroring into Mcp-Param headers.
	toolHeaders map[string][]paramHeader
}

func newSession(name string, opts ClientOptions) *session {
	return &session{name: name, opts: opts}
}

// negotiatedVersion returns the protocol version in use.
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

	s.mu.Lock()
	s.era, s.version = eraLegacy, ""
	s.mu.Unlock()
	if ea, ok := s.conn.(eraAware); ok {
		ea.setModern(false)
	}

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

// listTools returns every tool the server offers, following pagination. On
// a modern server it also caches each tool's x-mcp-header designations, and
// excludes tools whose designations are invalid (2026-07-28: a client on
// Streamable HTTP MUST).
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
			return s.indexToolHeaders(tools), nil
		}
		if seen[resp.NextCursor] {
			return nil, fmt.Errorf("mcp: server %s repeated tools/list cursor %q", s.name, resp.NextCursor)
		}
		seen[resp.NextCursor] = true
		cursor = resp.NextCursor
	}
	return nil, fmt.Errorf("mcp: server %s returned more than %d pages of tools", s.name, maxToolPages)
}

// indexToolHeaders records the tools' x-mcp-header designations and drops
// tools whose designations are invalid. It applies to modern servers only:
// the annotation does not exist in earlier revisions.
func (s *session) indexToolHeaders(tools []Tool) []Tool {
	if !s.isModern() {
		return tools
	}
	index := make(map[string][]paramHeader, len(tools))
	kept := tools[:0]
	for _, t := range tools {
		headers, err := toolParamHeaders(t.InputSchema)
		if err != nil {
			logger.Warn("MCP excluding tool with an invalid x-mcp-header", "server", s.name, "tool", t.Name, "reason", err)
			continue
		}
		index[t.Name] = headers
		kept = append(kept, t)
	}
	s.mu.Lock()
	s.toolHeaders = index
	s.mu.Unlock()
	return kept
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

// callOpts tunes one call.
type callOpts struct {
	// idempotent requests are retried on transport failures.
	idempotent bool
	// noReinit stops a session-expired failure from triggering a new
	// handshake (the handshake itself).
	noReinit bool
}

// callState tracks the one-shot recoveries a call may make.
type callState struct {
	reinitialized    bool
	versionRetried   bool
	headersRefreshed bool
	reissued         bool
}

// call sends one request and decodes its result into out.
//
// Retries: only idempotent requests, and only on transport failures. A
// JSON-RPC error is the server's answer, and a timeout may have been
// processed; neither is retried. The exceptions are failures that say the
// request was not processed — an expired session, a protocol version or
// header the server rejected — and, on a modern server, a broken response
// stream, which the spec requires the client to re-issue.
func (s *session) call(ctx context.Context, method string, params, out any, o callOpts) error {
	raw, err := marshalParams(params)
	if err != nil {
		return err
	}
	var st callState
	for attempt := 0; ; attempt++ {
		resp, err := s.roundTrip(ctx, method, raw)
		if err == nil {
			err = s.decode(method, resp, out)
		}
		if err == nil {
			return nil
		}
		retry, rerr := s.recover(ctx, method, err, o, &st)
		switch {
		case rerr != nil:
			return rerr
		case retry:
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

// recover handles a failure that means the request was not processed and
// can be sent again once the cause is fixed. It reports whether to retry.
func (s *session) recover(ctx context.Context, method string, err error, o callOpts, st *callState) (bool, error) {
	var rpcErr *RPCError
	switch {
	case errors.Is(err, errSessionExpired) && !o.noReinit && !st.reinitialized:
		// The server discarded the session the request was sent on.
		st.reinitialized = true
		logger.Info("MCP session expired, re-initializing", "server", s.name)
		if _, ierr := s.initialize(ctx); ierr != nil {
			return false, fmt.Errorf("mcp: re-initialize after session expiry: %w", ierr)
		}
		return true, nil
	case errors.As(err, &rpcErr) && rpcErr.Code == codeUnsupportedProtocolVersion && s.isModern() && !st.versionRetried:
		st.versionRetried = true
		return s.renegotiate(ctx, rpcErr)
	case errors.As(err, &rpcErr) && rpcErr.Code == codeHeaderMismatch && method == methodToolsCall &&
		!st.headersRefreshed:
		// The tool's x-mcp-header designations changed; re-list and resend.
		st.headersRefreshed = true
		if _, lerr := s.listTools(ctx); lerr != nil {
			return false, err
		}
		return true, nil
	case errors.Is(err, errStreamBroken) && !st.reissued:
		// 2026-07-28: a broken response stream loses the request, and the
		// server treats it as canceled; the client MUST re-issue it.
		st.reissued = true
		return true, nil
	}
	return false, nil
}

// renegotiate switches to a version the server says it supports, after an
// UnsupportedProtocolVersionError.
func (s *session) renegotiate(ctx context.Context, rpcErr *RPCError) (bool, error) {
	next, legacy, ok := s.pickVersion(rpcErr)
	switch {
	case !ok:
		return false, rpcErr
	case legacy:
		if _, err := s.initialize(ctx); err != nil {
			return false, fmt.Errorf("mcp: fall back to a handshake-era version: %w", err)
		}
	default:
		s.mu.Lock()
		s.version = next
		s.mu.Unlock()
	}
	return true, nil
}

// decode checks a response's resultType and decodes its result into out.
// An absent resultType means "complete" (earlier revisions omit it); an
// input_required result, valid only for some methods, is returned as an
// *inputRequiredError for the caller to fulfill.
func (s *session) decode(method string, resp *JSONRPCMessage, out any) error {
	if resp.Error != nil {
		return rpcErrorFrom(resp.Error)
	}
	var shape struct {
		ResultType *string `json:"resultType"`
	}
	if len(resp.Result) > 0 {
		_ = json.Unmarshal(resp.Result, &shape)
	}
	if shape.ResultType != nil {
		switch *shape.ResultType {
		case resultTypeComplete:
		case resultTypeInputRequired:
			if !mrtrMethods[method] {
				return fmt.Errorf("mcp: server returned input_required for %s, which does not allow it", method)
			}
			return &inputRequiredError{result: resp.Result}
		default:
			return fmt.Errorf("mcp: server returned unrecognized resultType %q", *shape.ResultType)
		}
	}
	return decodeResult(resp, out)
}

// roundTrip sends one request with a fresh id, applying the request timeout.
// If the client stops waiting — timeout or the caller's cancellation — the
// server is told the request is abandoned.
func (s *session) roundTrip(ctx context.Context, method string, params json.RawMessage) (*JSONRPCMessage, error) {
	req, err := s.buildRequest(method, params)
	if err != nil {
		return nil, err
	}

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

// buildRequest shapes a request for the era in use. Every request carries
// Mcp-Method (and Mcp-Name where defined) on HTTP; after the version is
// agreed, MCP-Protocol-Version. A modern request also carries the protocol
// metadata in _meta, and a tools/call mirrors the tool's x-mcp-header
// parameters into Mcp-Param headers.
func (s *session) buildRequest(method string, params json.RawMessage) (*request, error) {
	modern, ver := s.isModern(), s.negotiatedVersion()
	if modern {
		var err error
		if params, err = withMeta(params, s.requestMeta(ver)); err != nil {
			return nil, err
		}
	}
	req := &request{id: s.nextID.Add(1), method: method, params: params, header: http.Header{}}
	if ver != "" && method != methodInitialize {
		req.header.Set(headerProtocolVersion, ver)
	}
	setStandardHeaders(req.header, method, params)
	if modern && method == methodToolsCall {
		if err := s.setToolParamHeaders(req.header, params); err != nil {
			return nil, err
		}
	}
	return req, nil
}

// setToolParamHeaders mirrors a tool call's designated arguments into
// Mcp-Param headers.
func (s *session) setToolParamHeaders(h http.Header, params json.RawMessage) error {
	var call ToolCallRequest
	if json.Unmarshal(params, &call) != nil {
		return nil
	}
	s.mu.RLock()
	headers := s.toolHeaders[call.Name]
	s.mu.RUnlock()
	return setParamHeaders(h, headers, call.Arguments)
}

// cancel sends a best-effort cancellation notification for a request the
// client has stopped waiting for.
func (s *session) cancel(id int64, header http.Header) {
	ctx, done := context.WithTimeout(context.Background(), cancelNotifyTimeout)
	defer done()
	s.conn.cancelRequest(ctx, id, "client stopped waiting for the response", header)
}

// notify sends a notification.
func (s *session) notify(ctx context.Context, method string, params any) error {
	raw, err := marshalParams(params)
	if err != nil {
		return err
	}
	h := http.Header{}
	if v := s.negotiatedVersion(); v != "" {
		h.Set(headerProtocolVersion, v)
	}
	h.Set(headerMcpMethod, method)
	return s.conn.notify(ctx, &request{method: method, params: raw, header: h})
}

// serverRequest answers a request the server makes of the client (legacy
// servers only; modern servers ask through input_required results). The
// client answers ping, and refuses every method it has not advertised a
// capability for with Method not found, so a server never waits on a
// request nobody will answer.
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
	var ir *inputRequiredError
	switch {
	case errors.As(err, &rpcErr),
		errors.As(err, &ir),
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
