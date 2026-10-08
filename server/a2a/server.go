package a2aserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
)

const (
	// jsonRPCVersion is the JSON-RPC protocol version every response carries.
	jsonRPCVersion = "2.0"

	// idBytes is the number of random bytes used to generate task/context IDs.
	idBytes = 16

	// defaultReadHeaderTimeout prevents Slowloris attacks.
	defaultReadHeaderTimeout = 10 * time.Second

	// defaultReadTimeout is the maximum duration for reading the entire
	// request, including the body.
	defaultReadTimeout = 30 * time.Second

	// defaultWriteTimeout is the maximum duration before timing out
	// writes of the response.
	defaultWriteTimeout = 60 * time.Second

	// defaultIdleTimeout is the maximum amount of time to wait for the
	// next request when keep-alives are enabled.
	defaultIdleTimeout = 120 * time.Second

	// defaultMaxBodySize is the maximum allowed size of a request body (10 MB).
	defaultMaxBodySize int64 = 10 << 20

	// defaultPageSize is used when a 1.0 ListTasksRequest.PageSize is unset,
	// and maxPageSize caps it, both as the A2A 1.0 proto specifies. The legacy
	// 0.3 tasks/list, which only PromptKit answers, keeps its default of
	// maxPageSize.
	defaultPageSize = 50
	maxPageSize     = 100

	// sendSettleTime is how long handleSendMessage waits for fast calls
	// to complete before returning the task in its current state.
	sendSettleTime = 5 * time.Millisecond

	// defaultTaskTTL is how long completed/failed/canceled tasks are kept
	// before eviction.
	defaultTaskTTL = 1 * time.Hour

	// defaultConversationTTL is how long idle conversations are kept
	// before eviction.
	defaultConversationTTL = 1 * time.Hour

	// evictionInterval is how often the background eviction loop runs.
	evictionInterval = 1 * time.Minute
)

// Authenticator validates incoming requests. Return a non-nil error to reject.
type Authenticator interface {
	Authenticate(r *http.Request) error
}

// AgentCardProvider returns the agent card to serve at
// /.well-known/agent-card.json (and the legacy /.well-known/agent.json).
type AgentCardProvider interface {
	AgentCard(r *http.Request) (*a2a.AgentCard, error)
}

// StaticCard is an AgentCardProvider that always returns the same card.
type StaticCard struct {
	Card a2a.AgentCard
}

// AgentCard returns the static card.
func (s *StaticCard) AgentCard(*http.Request) (*a2a.AgentCard, error) {
	return &s.Card, nil
}

// Option configures a [Server].
type Option func(*Server)

// WithCard sets the agent card served at /.well-known/agent-card.json.
//
// The server completes the card's JSON-RPC interface declarations for the
// protocol versions it speaks (see servedCard); declare SecuritySchemes and
// SecurityRequirements on it when WithAuthenticator is in use, so callers can
// discover how to authenticate.
func WithCard(card *a2a.AgentCard) Option {
	return func(s *Server) { s.cardProvider = &StaticCard{Card: *card} }
}

// WithCardProvider sets a dynamic agent card provider.
func WithCardProvider(p AgentCardProvider) Option {
	return func(s *Server) { s.cardProvider = p }
}

// WithPort sets the TCP port for ListenAndServe.
func WithPort(port int) Option {
	return func(s *Server) { s.port = port }
}

// WithTaskStore sets a custom task store. Defaults to an in-memory store.
func WithTaskStore(store TaskStore) Option {
	return func(s *Server) { s.taskStore = store }
}

// WithReadTimeout sets the maximum duration for reading the entire request.
// Default: 30s.
func WithReadTimeout(d time.Duration) Option {
	return func(s *Server) { s.readTimeout = d }
}

// WithWriteTimeout sets the maximum duration before timing out writes of
// the response. Default: 60s.
func WithWriteTimeout(d time.Duration) Option {
	return func(s *Server) { s.writeTimeout = d }
}

// WithMaxBlockingWait caps how long a blocking SendMessage holds its request
// open. When the turn has not finished or been interrupted by then, the server
// answers with the task in its current (working) state, and the turn runs on:
// the caller polls GetTask or subscribes for the rest. Default: 0, no cap — a
// 1.0 SendMessage waits for the turn, as the spec requires, until the turn
// ends or the caller disconnects.
//
// Set it below the timeout of whatever sits in front of the server (a load
// balancer's idle timeout, a proxy's read timeout), so the caller gets a task
// to follow rather than a gateway error.
func WithMaxBlockingWait(d time.Duration) Option {
	return func(s *Server) { s.maxBlockingWait = d }
}

// WithIdleTimeout sets the maximum amount of time to wait for the next
// request when keep-alives are enabled. Default: 120s.
func WithIdleTimeout(d time.Duration) Option {
	return func(s *Server) { s.idleTimeout = d }
}

// WithMaxBodySize sets the maximum allowed request body size in bytes.
// Default: 10 MB.
func WithMaxBodySize(n int64) Option {
	return func(s *Server) { s.maxBodySize = n }
}

// WithTaskTTL sets how long completed/failed/canceled tasks are retained
// before automatic eviction. Default: 1 hour. Set to 0 to disable eviction.
func WithTaskTTL(d time.Duration) Option {
	return func(s *Server) { s.taskTTL = d }
}

// WithConversationTTL sets how long idle conversations are retained before
// automatic eviction. A conversation is considered idle when its last-use
// timestamp exceeds this duration. Default: 1 hour. Set to 0 to disable.
func WithConversationTTL(d time.Duration) Option {
	return func(s *Server) { s.convTTL = d }
}

// WithAuthenticator sets an authenticator for incoming requests.
func WithAuthenticator(auth Authenticator) Option {
	return func(s *Server) { s.authenticator = auth }
}

// HealthChecker performs a named health check. Implementations should return
// nil when healthy and a non-nil error describing the problem otherwise.
type HealthChecker interface {
	Check(ctx context.Context) error
}

// HealthCheckerFunc adapts an ordinary function to the [HealthChecker] interface.
type HealthCheckerFunc func(ctx context.Context) error

// Check calls f(ctx).
func (f HealthCheckerFunc) Check(ctx context.Context) error { return f(ctx) }

// WithHealthCheck registers a named health checker that is evaluated by the
// /readyz endpoint. Multiple checkers can be registered; each is reported
// individually in the response body.
func WithHealthCheck(name string, checker HealthChecker) Option {
	return func(s *Server) {
		s.healthChecks = append(s.healthChecks, namedChecker{name: name, checker: checker})
	}
}

// namedChecker pairs a human-readable name with a [HealthChecker].
type namedChecker struct {
	name    string
	checker HealthChecker
}

// Server is an HTTP server that exposes a Conversation as an
// A2A-compliant JSON-RPC endpoint.
type Server struct {
	// Exactly one of opener and handler is set; see NewServer and
	// NewStatelessServer. opener means the server owns conversations, handler
	// means the embedder does.
	opener        ConversationOpener
	handler       MessageHandler
	taskStore     TaskStore
	cardProvider  AgentCardProvider
	authenticator Authenticator
	port          int
	httpSrv       *http.Server
	httpSrvMu     sync.Mutex

	readTimeout     time.Duration
	writeTimeout    time.Duration
	maxBlockingWait time.Duration
	idleTimeout     time.Duration
	maxBodySize     int64

	// Readiness flag: set to true after NewServer completes, false on Shutdown.
	isReady atomic.Bool

	// Optional health checkers evaluated by /readyz.
	healthChecks []namedChecker

	// TTL-based eviction configuration.
	taskTTL  time.Duration // 0 disables task eviction
	convTTL  time.Duration // 0 disables conversation eviction
	stopOnce sync.Once
	stopCh   chan struct{} // closed to stop the eviction goroutine

	convsMu     sync.RWMutex
	convs       map[string]Conversation // context_id → conversation
	convLastUse map[string]time.Time    // context_id → last activity timestamp

	cancelsMu sync.Mutex
	cancels   map[string]context.CancelFunc // task_id → cancel for in-flight Send
	// cancelRegs identifies each entry in cancels by the registration that
	// made it, so a finished turn removes only its own (see unregisterCancel).
	cancelRegs map[string]uint64
	cancelSeq  uint64

	// events carries task updates to SubscribeToTask callers; canceler
	// reaches the instance running a task. Both default to in-process.
	events         TaskEventBus
	canceler       TaskCanceler
	stopCancelFeed func()

	// owner, when set, scopes every task to the caller that created it;
	// convOwner (under convsMu) records who opened each conversation.
	owner     OwnerFunc
	convOwner map[string]string

	// handlers serves each protocol operation.
	handlers map[a2a.Operation]func(*rpcCall)
}

// NewServer creates a new A2A server that OWNS its conversations: it opens one
// per context id through the supplied opener, caches it, and reuses it when
// that id returns. Suits an embedder running A2A and the runtime in one
// process.
//
// For an embedder that owns conversations itself (because the runtime lives
// elsewhere, or it already tracks sessions), see [NewStatelessServer].
func NewServer(opener ConversationOpener, opts ...Option) *Server {
	s := newServer(opts...)
	s.opener = opener
	return s
}

// newServer builds the parts both modes share.
func newServer(opts ...Option) *Server {
	s := &Server{
		convs:        make(map[string]Conversation),
		convLastUse:  make(map[string]time.Time),
		convOwner:    make(map[string]string),
		cancels:      make(map[string]context.CancelFunc),
		cancelRegs:   make(map[string]uint64),
		readTimeout:  defaultReadTimeout,
		writeTimeout: defaultWriteTimeout,
		idleTimeout:  defaultIdleTimeout,
		maxBodySize:  defaultMaxBodySize,
		taskTTL:      defaultTaskTTL,
		convTTL:      defaultConversationTTL,
		stopCh:       make(chan struct{}),
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.taskStore == nil {
		s.taskStore = NewInMemoryTaskStore()
	}
	s.checkOwnershipConfig()
	if s.events == nil {
		s.events = newLocalTaskEvents()
	}
	if s.canceler == nil {
		s.canceler = &localCanceler{}
	}
	s.stopCancelFeed = s.canceler.Listen(s.cancelLocal)
	s.handlers = s.rpcHandlers()

	// Start background eviction if at least one TTL is enabled.
	if s.taskTTL > 0 || s.convTTL > 0 {
		go s.evictionLoop()
	}

	s.isReady.Store(true)

	return s
}

// Handler returns an http.Handler implementing the A2A protocol.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+a2a.AgentCardPath, s.handleAgentCard)
	mux.HandleFunc("GET "+a2a.LegacyAgentCardPath, s.handleAgentCard)
	mux.HandleFunc("POST /a2a", s.handleRPC)
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	return otelhttp.NewHandler(recoveryMiddleware(mux), "a2a-server")
}

// recoveryMiddleware wraps an http.Handler with panic recovery.
// On panic it logs the stack trace and returns a 500 JSON-RPC error response.
func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				stack := debug.Stack()
				log.Printf("a2a: panic recovered: %v\n%s", rec, stack)
				writeRPCError(w, nil, a2a.ErrCodeInternal, "Internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ListenAndServe starts the HTTP server on the configured port.
//
// WriteTimeout is set to 0 (disabled) because SSE streaming endpoints
// (message/stream, tasks/subscribe) hold the connection open indefinitely.
// A non-zero WriteTimeout would kill long-lived SSE connections. Non-streaming
// endpoints rely on the request context deadline for timeout enforcement.
func (s *Server) ListenAndServe() error {
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", s.port),
		Handler:           s.Handler(),
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		ReadTimeout:       s.readTimeout,
		// TODO: WriteTimeout: 0 disables write timeouts on all endpoints, not
		// just SSE. Consider per-handler timeouts via http.TimeoutHandler for
		// non-streaming endpoints.
		WriteTimeout: 0,
		IdleTimeout:  s.idleTimeout,
	}

	s.httpSrvMu.Lock()
	s.httpSrv = srv
	s.httpSrvMu.Unlock()

	return srv.ListenAndServe()
}

// Shutdown gracefully shuts down the server: stops the eviction goroutine,
// drains HTTP requests, cancels in-flight tasks, and closes all conversations.
func (s *Server) Shutdown(ctx context.Context) error {
	// Mark as not ready so /readyz returns 503 immediately.
	s.isReady.Store(false)

	// Stop the eviction goroutine.
	s.stopOnce.Do(func() { close(s.stopCh) })

	var firstErr error

	s.httpSrvMu.Lock()
	srv := s.httpSrv
	s.httpSrvMu.Unlock()

	if srv != nil {
		firstErr = srv.Shutdown(ctx)
	}

	// Stop taking cancel requests, and end this process's subscriptions.
	if s.stopCancelFeed != nil {
		s.stopCancelFeed()
	}
	if local, ok := s.events.(*localTaskEvents); ok {
		local.closeAll()
	}

	// Cancel all in-flight tasks.
	s.cancelsMu.Lock()
	for _, cancel := range s.cancels {
		cancel()
	}
	s.cancels = make(map[string]context.CancelFunc)
	s.cancelRegs = make(map[string]uint64)
	s.cancelsMu.Unlock()

	// Close all conversations.
	s.convsMu.Lock()
	for id, conv := range s.convs {
		if err := conv.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(s.convs, id)
		delete(s.convOwner, id)
	}
	s.convsMu.Unlock()

	return firstErr
}

// Serve starts the HTTP server on the given listener.
// See ListenAndServe for the rationale behind WriteTimeout: 0.
func (s *Server) Serve(ln net.Listener) error {
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		ReadTimeout:       s.readTimeout,
		// TODO: WriteTimeout: 0 disables write timeouts on all endpoints, not
		// just SSE. Consider per-handler timeouts via http.TimeoutHandler for
		// non-streaming endpoints.
		WriteTimeout: 0,
		IdleTimeout:  s.idleTimeout,
	}

	s.httpSrvMu.Lock()
	s.httpSrv = srv
	s.httpSrvMu.Unlock()

	return srv.Serve(ln)
}

// handleHealthz is a liveness probe: it returns 200 whenever the HTTP server
// is accepting connections.
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleReadyz is a readiness probe: it returns 200 when the server is ready
// to accept traffic and 503 otherwise. It checks the isReady flag and all
// registered health checkers.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if !s.isReady.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "not_ready",
			"reason": "server is shutting down or not yet initialized",
		})
		return
	}

	// Run registered health checks.
	type checkResult struct {
		Status string `json:"status"`
		Error  string `json:"error,omitempty"`
	}
	checks := make(map[string]checkResult, len(s.healthChecks))
	allOK := true
	for _, nc := range s.healthChecks {
		if err := nc.checker.Check(r.Context()); err != nil {
			checks[nc.name] = checkResult{Status: "fail", Error: err.Error()}
			allOK = false
		} else {
			checks[nc.name] = checkResult{Status: "pass"}
		}
	}

	if !allOK {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "not_ready",
			"reason": "one or more health checks failed",
			"checks": checks,
		})
		return
	}

	resp := map[string]any{"status": "ready"}
	if len(checks) > 0 {
		resp["checks"] = checks
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// handleAgentCard serves the agent card in the caller's protocol version: the
// 1.0 card when the request asks for 1.0, otherwise the 0.3 card, which also
// carries supportedInterfaces so a 1.0 client that sent no header still finds
// its interface.
func (s *Server) handleAgentCard(w http.ResponseWriter, r *http.Request) {
	card, err := s.card(r)
	if err != nil {
		log.Printf("a2a: failed to get agent card: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	v, err := requestVersion(r, a2a.ProtocolVersion03)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v.WireAgentCard(card))
}

// card returns the agent card this server publishes for r.
func (s *Server) card(r *http.Request) (*a2a.AgentCard, error) {
	card := &a2a.AgentCard{}
	if s.cardProvider != nil {
		provided, err := s.cardProvider.AgentCard(r)
		if err != nil {
			return nil, err
		}
		card = provided
	}
	return servedCard(card, r), nil
}

// handleGetExtendedAgentCard processes GetExtendedAgentCard (0.3:
// agent/getAuthenticatedExtendedCard). The server holds no extended card, so
// the answer depends only on whether the card declares one (A2A 1.0 §3.3.4):
// UnsupportedOperation when it does not, ExtendedAgentCardNotConfigured when it
// does.
func (s *Server) handleGetExtendedAgentCard(call *rpcCall) {
	card, err := s.card(call.r)
	if err != nil {
		call.internalError("failed to get agent card", err)
		return
	}
	if !card.Capabilities.ExtendedAgentCard {
		call.fail(a2a.ErrCodeUnsupportedOperation, "This agent does not declare an extended agent card")
		return
	}
	call.fail(a2a.ErrCodeExtendedAgentCardNotConfigured, "No extended agent card is configured")
}

// handleRPC dispatches a JSON-RPC 2.0 request to the appropriate handler.
func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	// Authenticate if configured.
	if s.authenticator != nil {
		if err := s.authenticator.Authenticate(r); err != nil {
			log.Printf("a2a: authentication failed: %v", err)
			writeRPCErrorWithStatus(w, http.StatusUnauthorized, nil, -32000, "Authentication failed")
			return
		}
	}

	r.Body = http.MaxBytesReader(w, r.Body, s.maxBodySize)

	var req a2a.JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPCError(w, nil, a2a.ErrCodeParse, "Parse error")
		return
	}

	op, methodVersion, known := a2a.LookupMethod(req.Method)
	if !known {
		writeRPCError(w, req.ID, a2a.ErrCodeMethodNotFound, "Method not found")
		return
	}
	v, err := requestVersion(r, methodVersion)
	if err != nil {
		writeRPCError(w, req.ID, a2a.ErrCodeVersionNotSupported, err.Error())
		return
	}
	call := &rpcCall{w: w, r: r, req: &req, v: v}
	if s.owner != nil {
		if call.owner = s.owner(r); call.owner == "" {
			writeRPCErrorWithStatus(w, http.StatusUnauthorized, req.ID, -32000, "Caller identity required")
			return
		}
	}
	s.handlers[op](call)
}

// rpcHandlers maps every operation to the handler that serves it. It is
// built once, in newServer.
func (s *Server) rpcHandlers() map[a2a.Operation]func(*rpcCall) {
	return map[a2a.Operation]func(*rpcCall){
		a2a.OpSendMessage:          s.handleSendMessage,
		a2a.OpSendStreamingMessage: s.handleStreamMessage,
		a2a.OpGetTask:              s.handleGetTask,
		a2a.OpCancelTask:           s.handleCancelTask,
		a2a.OpListTasks:            s.handleListTasks,
		a2a.OpSubscribeToTask:      s.handleTaskSubscribe,
		a2a.OpPushNotificationConfig: func(call *rpcCall) {
			call.fail(a2a.ErrCodePushNotificationNotSupported, "Push notifications are not supported")
		},
		a2a.OpGetExtendedAgentCard: s.handleGetExtendedAgentCard,
		a2a.OpUnknown: func(call *rpcCall) {
			call.fail(a2a.ErrCodeMethodNotFound, "Method not found")
		},
	}
}

// turnTarget is the task a message runs on: a new task, or the existing one
// the message's taskId continues (A2A 1.0 §3.4).
type turnTarget struct {
	taskID    string
	contextID string
	// existing is true when the message continues taskID; prior is the
	// status it was waiting in.
	existing bool
	prior    a2a.TaskStatus
	// claimed is true once beginTask has made the task this turn's.
	claimed bool
}

// resolveTarget works out which task a message runs on. A message without a
// taskId starts a new task in its context (a new context when it names
// none). One with a taskId continues that task, which must exist and be
// visible to the caller (else TaskNotFound), must not have finished (else
// UnsupportedOperation, §3.1.1), and supplies the context — a contextId that
// differs from the task's is refused (§3.4.3). ok is false when the call has
// been answered with an error.
func (s *Server) resolveTarget(call *rpcCall, msg *a2a.Message) (target turnTarget, ok bool) {
	if msg.TaskID == "" {
		target.contextID = msg.ContextID
		if target.contextID == "" {
			target.contextID = generateID()
		}
		return target, true
	}
	task := s.getTaskFor(call, msg.TaskID)
	if task == nil {
		return target, false
	}
	if task.Status.State.IsTerminal() {
		call.fail(a2a.ErrCodeUnsupportedOperation,
			fmt.Sprintf("Task is %s; a finished task takes no further messages", task.Status.State.V03Name()))
		return target, false
	}
	if msg.ContextID != "" && msg.ContextID != task.ContextID {
		call.fail(a2a.ErrCodeInvalidParams, "Invalid params: contextId does not match the task's context")
		return target, false
	}
	return turnTarget{taskID: task.ID, contextID: task.ContextID, existing: true, prior: task.Status}, true
}

// beginTask readies the target's task for a turn and marks it working: it
// creates a new task, or claims the existing one. Only a task waiting for the
// caller (input-required or auth-required) can be claimed; one already
// running a turn is refused, so two messages never drive one task at once.
// It answers the call itself and returns false when it cannot.
func (s *Server) beginTask(call *rpcCall, target *turnTarget) bool {
	if target.claimed {
		return true
	}
	if !target.existing {
		target.taskID = generateID()
		if err := s.createTask(call, target.taskID, target.contextID); err != nil {
			call.internalError(fmt.Sprintf("failed to create task for context %s", target.contextID), err)
			return false
		}
		s.setState(target.taskID, target.contextID, a2a.TaskStateWorking, nil)
		target.claimed = true
		return true
	}

	switch err := s.taskStore.SetState(target.taskID, a2a.TaskStateWorking, nil); {
	case err == nil:
		s.publishStatus(target.taskID, target.contextID, a2a.TaskStatus{State: a2a.TaskStateWorking})
		target.claimed = true
		return true
	case errors.Is(err, ErrTaskNotFound):
		call.fail(a2a.ErrCodeTaskNotFound, "Task not found")
	case errors.Is(err, ErrTaskTerminal), errors.Is(err, ErrInvalidTransition):
		call.fail(a2a.ErrCodeUnsupportedOperation,
			"Task is not waiting for input; it cannot take a message now")
	default:
		call.internalError(fmt.Sprintf("failed to resume task %s", target.taskID), err)
	}
	return false
}

// handleSendMessage processes SendMessage (0.3: message/send).
func (s *Server) handleSendMessage(call *rpcCall) {
	var params a2a.SendMessageRequest
	if !call.decodeParams(&params) {
		return
	}

	target, ok := s.resolveTarget(call, &params.Message)
	if !ok {
		return
	}

	// Stateless mode short-circuits everything about conversation ownership:
	// there is nothing to open, nothing to cache, and the handler sees the
	// request it arrived on.
	if s.handler != nil {
		s.handleSendViaHandler(call, &target, params)
		return
	}

	conv := s.openConversation(call, target.contextID)
	if conv == nil {
		return
	}

	// Check if this is a tool-result message for a resumable conversation.
	if toolResults := extractToolResults(params.Message.Parts); len(toolResults) > 0 {
		s.handleToolResultMessage(call, conv, &target, toolResults, params.Configuration)
		return
	}

	pkMsg, err := a2a.MessageToMessage(&params.Message)
	if err != nil {
		call.fail(a2a.ErrCodeInvalidParams, fmt.Sprintf("Invalid message: %v", err))
		return
	}

	if !s.beginTask(call, &target) {
		return
	}

	// Detach from the request's cancellation because the goroutine outlives the
	// HTTP handler on the non-blocking path, but keep its values: caller
	// middleware (identity, tenant, request-scoped config) and the OTel span
	// context both ride along, so downstream spans still nest under the inbound
	// trace and SendMessage behaves like SendStreamingMessage.
	bgCtx := context.WithoutCancel(call.r.Context())
	done := s.runConversation(bgCtx, target.taskID, target.contextID, conv, pkMsg)
	s.awaitTurn(call, target.taskID, done, params.Configuration)
}

// toolResultEntry represents a single client tool result extracted from an A2A message.
type toolResultEntry struct {
	CallID   string
	Result   any
	Rejected bool
	Reason   string
}

// extractToolResults inspects message parts for client tool results.
// Parts with metadata containing "tool_call_id" are treated as tool results.
// The result is the metadata's "tool_result", or else the part's own data.
func extractToolResults(parts []a2a.Part) []toolResultEntry {
	var results []toolResultEntry
	for _, p := range parts {
		callID, ok := p.Metadata["tool_call_id"].(string)
		if !ok || callID == "" {
			continue
		}
		entry := toolResultEntry{CallID: callID}
		if reason, rejected := p.Metadata["rejected"].(string); rejected {
			entry.Rejected = true
			entry.Reason = reason
		} else if result, given := p.Metadata["tool_result"]; given {
			entry.Result = result
		} else if p.Data != nil {
			entry.Result = p.Data
		} else {
			entry.Result = p.DataValue
		}
		results = append(results, entry)
	}
	return results
}

// claimAndSubmit claims the target's task, then hands the client tool
// results to the conversation — in that order, so a message the task cannot
// take never leaves its results in the conversation. When the results cannot
// be submitted the task is released (see releaseTask). It answers the call
// itself and returns nil when the turn cannot go ahead.
func (s *Server) claimAndSubmit(
	call *rpcCall, target *turnTarget, conv Conversation, results []toolResultEntry,
) ResumableConversation {
	resumable, ok := conv.(ResumableConversation)
	if !ok {
		call.fail(a2a.ErrCodeUnsupportedOperation, "Conversation does not support client tool results")
		return nil
	}
	if !s.beginTask(call, target) {
		return nil
	}
	for _, tr := range results {
		if tr.Rejected {
			resumable.RejectClientTool(tr.CallID, tr.Reason)
			continue
		}
		if err := resumable.SendToolResult(tr.CallID, tr.Result); err != nil {
			s.releaseTask(target, err)
			call.internalError(fmt.Sprintf("failed to submit tool result %s", tr.CallID), err)
			return nil
		}
	}
	return resumable
}

// releaseTask undoes a claim whose turn never started: a continued task goes
// back to the status it was waiting in, request included, and a new task is
// failed with cause.
func (s *Server) releaseTask(target *turnTarget, cause error) {
	if !target.existing {
		s.failTask(target.taskID, target.contextID, cause)
		return
	}
	s.setState(target.taskID, target.contextID, target.prior.State, target.prior.Message)
}

// handleToolResultMessage processes a SendMessage that carries client tool
// results: it submits each result to the ResumableConversation and resumes.
//
// With the message's taskId, the input-required task continues; without one
// (as older clients send them), the resumed turn gets a task of its own.
func (s *Server) handleToolResultMessage(
	call *rpcCall, conv Conversation, target *turnTarget,
	results []toolResultEntry, cfg *a2a.SendMessageConfiguration,
) {
	resumable := s.claimAndSubmit(call, target, conv, results)
	if resumable == nil {
		return
	}

	// Detach from the request's cancellation but keep its values; see
	// handleSendMessage for why.
	bgCtx := context.WithoutCancel(call.r.Context())
	done := s.runTurn(bgCtx, target.taskID, target.contextID, resumable.Resume)
	s.awaitTurn(call, target.taskID, done, cfg)
}

// runConversation spawns a goroutine that drives the conversation for a task.
// It returns a channel that is closed when the goroutine completes.
func (s *Server) runConversation(
	parent context.Context, taskID, contextID string, conv Conversation, pkMsg any,
) <-chan struct{} {
	return s.runTurn(parent, taskID, contextID, func(ctx context.Context) (SendResult, error) {
		return conv.Send(ctx, pkMsg)
	})
}

// runTurn drives one turn in the background and closes the returned channel
// when it is done. The task is already working (see beginTask).
//
// Every way a turn can be produced — a conversation's Send, its Resume, a
// stateless handler's stream — needs the same surrounding care: register the
// cancel func so CancelTask can reach it, and on failure avoid overwriting a
// state the cancel handler already set. Three copies of that is how the
// copies drift.
func (s *Server) runTurn(
	parent context.Context, taskID, contextID string, produce func(context.Context) (SendResult, error),
) <-chan struct{} {
	ctx, cancel := context.WithCancel(parent)
	reg := s.registerCancel(taskID, cancel)

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		defer s.unregisterCancel(taskID, reg)

		resp, err := produce(ctx)
		if err != nil {
			// A canceled context means CancelTask already set the state to
			// "canceled"; only a genuine error marks the task failed.
			if ctx.Err() == nil {
				s.failTask(taskID, contextID, err)
			}
			return
		}
		if ctx.Err() != nil {
			return
		}

		s.finalizeTask(taskID, contextID, resp)
	}()

	return done
}

// registerCancel records the cancel func of a turn running in this process,
// and returns the registration that turn later unregisters with.
func (s *Server) registerCancel(taskID string, cancel context.CancelFunc) uint64 {
	s.cancelsMu.Lock()
	defer s.cancelsMu.Unlock()
	s.cancelSeq++
	s.cancels[taskID] = cancel
	s.cancelRegs[taskID] = s.cancelSeq
	return s.cancelSeq
}

// unregisterCancel forgets a finished turn's cancel func — only if it is
// still that turn's registration. A continuation may have claimed the task
// and registered its own turn before this one's cleanup ran.
func (s *Server) unregisterCancel(taskID string, reg uint64) {
	s.cancelsMu.Lock()
	defer s.cancelsMu.Unlock()
	if s.cancelRegs[taskID] != reg {
		return
	}
	delete(s.cancels, taskID)
	delete(s.cancelRegs, taskID)
}

// forgetCancel drops whatever cancel func is registered for taskID.
func (s *Server) forgetCancel(taskID string) {
	s.cancelsMu.Lock()
	delete(s.cancels, taskID)
	delete(s.cancelRegs, taskID)
	s.cancelsMu.Unlock()
}

// cancelLocal stops taskID's turn if it runs in this process.
func (s *Server) cancelLocal(taskID string) {
	s.cancelsMu.Lock()
	cancel, ok := s.cancels[taskID]
	delete(s.cancels, taskID)
	delete(s.cancelRegs, taskID)
	s.cancelsMu.Unlock()
	if ok {
		cancel()
	}
}

// setState records a state change and publishes it to the task's
// subscribers. A failed store write is logged and not published: a
// subscriber must never see a state the store does not hold.
func (s *Server) setState(taskID, contextID string, state a2a.TaskState, msg *a2a.Message) {
	if err := s.taskStore.SetState(taskID, state, msg); err != nil {
		log.Printf("a2a: task %s: failed to set %s state: %v", taskID, state, err)
		return
	}
	s.publishStatus(taskID, contextID, a2a.TaskStatus{State: state, Message: msg})
}

// publishStatus publishes a status update to the task's subscribers.
func (s *Server) publishStatus(taskID, contextID string, status a2a.TaskStatus) {
	if status.Timestamp == nil {
		now := time.Now().UTC()
		status.Timestamp = &now
	}
	s.publish(taskID, TaskEvent{StatusUpdate: &a2a.TaskStatusUpdateEvent{
		TaskID: taskID, ContextID: contextID, Status: status,
	}})
}

// publish delivers an event to the task's subscribers.
func (s *Server) publish(taskID string, evt TaskEvent) {
	if err := s.events.Publish(context.Background(), taskID, evt); err != nil {
		log.Printf("a2a: task %s: failed to publish event: %v", taskID, err)
	}
}

// failTask records a turn's error as the task's terminal state.
func (s *Server) failTask(taskID, contextID string, cause error) {
	errText := cause.Error()
	s.setState(taskID, contextID, a2a.TaskStateFailed, &a2a.Message{
		MessageID: generateID(),
		ContextID: contextID,
		TaskID:    taskID,
		Role:      a2a.RoleAgent,
		Parts:     []a2a.Part{{Text: &errText}},
	})
}

// awaitTurn waits for a turn the way SendMessage does — until it finishes or
// is interrupted when the caller's version and configuration ask for that
// (1.0's default), otherwise only until the settle time — then answers with
// the task as it stands. A blocking wait ends early if the caller disconnects,
// or after WithMaxBlockingWait when set.
func (s *Server) awaitTurn(
	call *rpcCall, taskID string, done <-chan struct{}, cfg *a2a.SendMessageConfiguration,
) {
	wait := sendSettleTime
	if cfg.WaitsForCompletion(call.v) {
		wait = s.maxBlockingWait
	}
	var timeout <-chan time.Time
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		timeout = timer.C
	}
	select {
	case <-done:
	case <-timeout:
		// Answer with the task as it stands; the turn runs on, and the caller
		// polls GetTask or subscribes for the rest.
	case <-call.r.Context().Done():
		// The caller left. The turn runs on (it is detached from the
		// request); there is just no one to answer.
		return
	}

	task, err := s.taskStore.Get(taskID)
	if err != nil {
		call.internalError(fmt.Sprintf("failed to retrieve task %s after processing", taskID), err)
		return
	}
	if cfg != nil {
		applyHistoryLength(task, cfg.HistoryLength)
	}
	call.result(call.v.WireSendResult(task))
}

// finalizeTask handles the terminal state of a task based on the SendResult.
// If client tools are pending, it sets input_required with tool metadata.
// Otherwise it stores artifacts and marks the task completed.
func (s *Server) finalizeTask(taskID, contextID string, resp SendResult) {
	if resp.HasPendingTools() {
		msg := buildPendingToolsMessage(resp)
		if msg != nil {
			msg.MessageID, msg.ContextID, msg.TaskID = generateID(), contextID, taskID
		}
		s.setState(taskID, contextID, a2a.TaskStateInputRequired, msg)
		return
	}

	artifacts, convErr := a2a.ContentPartsToArtifacts(resp.Parts())
	if convErr == nil && len(artifacts) > 0 {
		if err := s.taskStore.AddArtifacts(taskID, artifacts); err != nil {
			log.Printf("a2a: task %s: failed to add artifacts: %v", taskID, err)
		}
	} else if text := resp.Text(); text != "" {
		// Fallback: if Parts() is empty (see GH-428), use Text() content.
		artifacts = []a2a.Artifact{{
			ArtifactID: "artifact-1",
			Parts:      []a2a.Part{{Text: &text}},
		}}
		if err := s.taskStore.AddArtifacts(taskID, artifacts); err != nil {
			log.Printf("a2a: task %s: failed to add text artifact: %v", taskID, err)
		}
	}
	// Subscribers get the result before the completion that ends their stream.
	for i := range artifacts {
		s.publish(taskID, TaskEvent{ArtifactUpdate: &a2a.TaskArtifactUpdateEvent{
			TaskID: taskID, ContextID: contextID, Artifact: artifacts[i], LastChunk: true,
		}})
	}
	s.setState(taskID, contextID, a2a.TaskStateCompleted, nil)
}

// buildPendingToolsMessage creates an A2A message describing pending tools.
// For client tools, it includes tool metadata (call ID, name, args, consent)
// in the message parts so the A2A client can fulfill them.
func buildPendingToolsMessage(resp SendResult) *a2a.Message {
	clientTools := resp.PendingClientTools()
	if len(clientTools) == 0 {
		// HITL-only pending tools — no metadata needed.
		return nil
	}

	parts := make([]a2a.Part, len(clientTools))
	for i, t := range clientTools {
		parts[i] = clientToolPart(t)
	}
	return &a2a.Message{
		Role:  a2a.RoleAgent,
		Parts: parts,
	}
}

// clientToolPart describes one pending client tool as a message part.
func clientToolPart(t PendingClientToolInfo) a2a.Part {
	text := fmt.Sprintf("Client tool required: %s", t.ToolName)
	return a2a.Part{
		Text: &text,
		Metadata: map[string]any{
			"tool_call_id":    t.CallID,
			"tool_name":       t.ToolName,
			"tool_args":       t.Args,
			"consent_message": t.ConsentMsg,
		},
	}
}

// handleGetTask processes GetTask (0.3: tasks/get).
func (s *Server) handleGetTask(call *rpcCall) {
	var params a2a.GetTaskRequest
	if !call.decodeParams(&params) {
		return
	}

	task := s.getTaskFor(call, params.ID)
	if task == nil {
		return
	}
	applyHistoryLength(task, params.HistoryLength)
	call.result(call.v.WireTask(task))
}

// handleCancelTask processes CancelTask (0.3: tasks/cancel).
//
// The state is checked before anything is canceled: a task that has already
// finished is not cancelable (-32002), and its turn — if one were somehow
// still registered — must not be interrupted by a request that fails.
func (s *Server) handleCancelTask(call *rpcCall) {
	var params a2a.CancelTaskRequest
	if !call.decodeParams(&params) {
		return
	}

	task := s.getTaskFor(call, params.ID)
	if task == nil {
		return
	}
	if task.Status.State.IsTerminal() {
		call.fail(a2a.ErrCodeTaskNotCancelable,
			fmt.Sprintf("Task cannot be canceled: it is %s", task.Status.State.V03Name()))
		return
	}

	switch cancelErr := s.taskStore.Cancel(params.ID); {
	case errors.Is(cancelErr, ErrTaskTerminal):
		// Finished between the read and the cancel.
		call.fail(a2a.ErrCodeTaskNotCancelable, "Task cannot be canceled: it has already finished")
		return
	case errors.Is(cancelErr, ErrTaskNotFound):
		call.fail(a2a.ErrCodeTaskNotFound, "Task not found")
		return
	case cancelErr != nil:
		call.internalError(fmt.Sprintf("task cancel failed for %s", params.ID), cancelErr)
		return
	}

	if err := s.canceler.Cancel(context.WithoutCancel(call.r.Context()), params.ID); err != nil {
		// The task is canceled in the store either way; the turn may run on
		// until it next checks, but its result can no longer be recorded.
		log.Printf("a2a: task %s: failed to reach its turn to cancel it: %v", params.ID, err)
	}

	task, err := s.taskStore.Get(params.ID)
	if err != nil {
		call.internalError(fmt.Sprintf("failed to retrieve task %s after cancel", params.ID), err)
		return
	}
	s.publishStatus(task.ID, task.ContextID, task.Status)
	call.result(call.v.WireTask(task))
}

// handleListTasks processes ListTasks (1.0; PromptKit's legacy tasks/list).
//
// Tasks are listed most recently updated first, a page at a time, behind an
// opaque cursor, and only the caller's own when WithTaskOwner is set. Without
// it the server cannot tell callers apart, so a request without a contextId
// is refused: listing every task in the store would hand one caller every
// other caller's results.
func (s *Server) handleListTasks(call *rpcCall) {
	var params a2a.ListTasksRequest
	if len(call.req.Params) > 0 && !call.decodeParams(&params) {
		return
	}
	if params.ContextID == "" && s.owner == nil {
		call.fail(a2a.ErrCodeInvalidParams,
			"Invalid params: contextId is required unless the server scopes tasks by caller")
		return
	}

	limit := params.PageSize
	switch {
	case limit <= 0 && call.v == a2a.ProtocolVersion03:
		limit = maxPageSize
	case limit <= 0:
		limit = defaultPageSize
	case limit > maxPageSize:
		limit = maxPageSize
	}
	// In 1.0, TASK_STATE_UNSPECIFIED is the proto's zero value: no status
	// filter. In 0.3 the same TaskState is the real "unknown" state.
	status := params.Status
	if status != nil && *status == a2a.TaskStateUnknown && call.v != a2a.ProtocolVersion03 {
		status = nil
	}
	offset, err := decodePageToken(params.PageToken)
	if err != nil {
		call.fail(a2a.ErrCodeInvalidParams, "Invalid params: "+err.Error())
		return
	}

	page, err := queryTasks(s.taskStore, TaskQuery{
		Owner:       call.owner,
		ContextID:   params.ContextID,
		Status:      status,
		StatusAfter: params.StatusTimestampAfter,
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		call.internalError(fmt.Sprintf("task list failed for context %s", params.ContextID), err)
		return
	}

	// 1.0 omits artifacts unless asked; the legacy tasks/list always sent them.
	includeArtifacts := params.IncludeArtifacts || call.v == a2a.ProtocolVersion03
	tasks := make([]any, len(page.Tasks))
	for i, t := range page.Tasks {
		applyHistoryLength(t, params.HistoryLength)
		if !includeArtifacts {
			t.Artifacts = nil
		}
		tasks[i] = call.v.WireTask(t)
	}
	next := ""
	if offset+len(page.Tasks) < page.Total {
		next = encodePageToken(offset + len(page.Tasks))
	}
	call.result(struct {
		Tasks         []any  `json:"tasks"`
		NextPageToken string `json:"nextPageToken"`
		PageSize      int    `json:"pageSize"`
		TotalSize     int    `json:"totalSize"`
	}{tasks, next, limit, page.Total})
}

// getOrCreateConversation retrieves an existing conversation for the context ID
// or creates a new one via the opener (double-check lock pattern).
// It also updates the last-use timestamp for conversation TTL tracking.
//
// With tasks scoped by caller, a conversation belongs to the caller that
// opened it, checked under the same lock that creates it: another caller
// naming its context gets errContextTaken, however the two race.
func (s *Server) getOrCreateConversation(call *rpcCall, contextID string) (Conversation, error) {
	// Acquire write lock directly to avoid a TOCTOU gap between RUnlock and
	// Lock that could allow duplicate conversation creation.
	s.convsMu.Lock()
	defer s.convsMu.Unlock()

	if conv, ok := s.convs[contextID]; ok {
		if s.owner != nil && s.convOwner[contextID] != call.owner {
			return nil, errContextTaken
		}
		s.convLastUse[contextID] = time.Now()
		return conv, nil
	}

	if err := s.checkContextTasks(call, contextID); err != nil {
		return nil, err
	}
	conv, err := s.opener(contextID)
	if err != nil {
		return nil, err
	}
	s.convs[contextID] = conv
	s.convLastUse[contextID] = time.Now()
	if s.owner != nil {
		s.convOwner[contextID] = call.owner
	}
	return conv, nil
}

// openConversation answers the call itself when the conversation for
// contextID cannot be had, and returns nil.
func (s *Server) openConversation(call *rpcCall, contextID string) Conversation {
	conv, err := s.getOrCreateConversation(call, contextID)
	switch {
	case errors.Is(err, errContextTaken):
		call.fail(a2a.ErrCodeInvalidParams, "Invalid params: contextId is not available to this caller")
		return nil
	case err != nil:
		call.internalError(fmt.Sprintf("failed to open conversation for context %s", contextID), err)
		return nil
	}
	return conv
}

// generateID returns a random hex string suitable for task and context IDs.
func generateID() string {
	b := make([]byte, idBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// writeRPCResult writes a JSON-RPC 2.0 success response.
func writeRPCResult(w http.ResponseWriter, id, result any) {
	data, err := json.Marshal(result)
	if err != nil {
		log.Printf("a2a: failed to marshal RPC result: %v", err)
		writeRPCError(w, id, -32603, "Internal error: failed to encode result")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(a2a.JSONRPCResponse{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Result:  data,
	})
}

// writeRPCError writes a JSON-RPC 2.0 error response.
func writeRPCError(w http.ResponseWriter, id any, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(a2a.JSONRPCResponse{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Error:   &a2a.JSONRPCError{Code: code, Message: msg},
	})
}

// writeRPCErrorWithStatus writes a JSON-RPC 2.0 error response with a specific HTTP status code.
func writeRPCErrorWithStatus(w http.ResponseWriter, status int, id any, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(a2a.JSONRPCResponse{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Error:   &a2a.JSONRPCError{Code: code, Message: msg},
	})
}

// evictionLoop periodically sweeps expired tasks, conversations, and
// broadcasters. It runs until stopCh is closed (via Shutdown).
func (s *Server) evictionLoop() {
	ticker := time.NewTicker(evictionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.evictOnce()
		}
	}
}

// evictOnce runs a single eviction pass. Subscriptions need no sweep: a final
// event or the subscriber's own disconnect ends each one. It is safe to call concurrently.
func (s *Server) evictOnce() {
	now := time.Now()
	s.evictTerminalTasks(now)
	s.evictIdleConversations(now)
}

// evictTerminalTasks removes expired terminal tasks and their associated
// cancel functions.
func (s *Server) evictTerminalTasks(now time.Time) {
	if s.taskTTL <= 0 {
		return
	}
	evicted := s.taskStore.EvictTerminal(now.Add(-s.taskTTL))
	for _, taskID := range evicted {
		s.forgetCancel(taskID)
	}
}

// evictIdleConversations closes and removes conversations whose last-use
// timestamp exceeds the conversation TTL.
func (s *Server) evictIdleConversations(now time.Time) {
	if s.convTTL <= 0 {
		return
	}
	cutoff := now.Add(-s.convTTL)
	s.convsMu.Lock()
	defer s.convsMu.Unlock()
	for id, lastUse := range s.convLastUse {
		if lastUse.Before(cutoff) {
			if conv, ok := s.convs[id]; ok {
				_ = conv.Close()
				delete(s.convs, id)
			}
			delete(s.convLastUse, id)
			delete(s.convOwner, id)
		}
	}
}
