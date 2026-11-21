package a2aserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
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

	// defaultIdleTimeout is the maximum amount of time to wait for the
	// next request when keep-alives are enabled.
	defaultIdleTimeout = 120 * time.Second

	// defaultCardMaxAge is how long a client or cache may reuse the served
	// agent card before revalidating it (WithCardMaxAge).
	defaultCardMaxAge = 5 * time.Minute

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

	// defaultInterruptedTaskTTL is how long a task waiting for the caller
	// (input-required, auth-required) is kept before eviction.
	defaultInterruptedTaskTTL = 24 * time.Hour

	// defaultConversationTTL is how long idle conversations are kept
	// before eviction.
	defaultConversationTTL = 1 * time.Hour

	// evictionInterval is how often the background eviction loop runs.
	evictionInterval = 1 * time.Minute
)

// Authenticator validates incoming requests to POST /a2a. Return a non-nil
// error to reject the request, which the server answers with HTTP 401 and a
// JSON-RPC error. The agent card and health endpoints are not authenticated.
type Authenticator interface {
	Authenticate(r *http.Request) error
}

// AgentCardProvider returns the agent card to serve at
// /.well-known/agent-card.json (and the legacy /.well-known/agent.json). The
// server completes the returned card as WithCard describes.
//
// A card from a provider may differ per request (by tenant, cookie or
// forwarded header), so unless WithCardMaxAge is set it is served with
// Cache-Control: no-cache: clients revalidate every fetch, and its ETag makes
// an unchanged card a 304. A *StaticCard is cached like a WithCard card.
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
// The server completes a copy of the card before serving it. A card with no
// interfaces gets a JSON-RPC interface pointing at this server's /a2a endpoint,
// derived from the request's Host; JSON-RPC interfaces are declared for both
// A2A 1.0 and 0.3; Streaming is set to true (false WithoutStreaming) and
// PushNotifications to false.
// Declare SecuritySchemes and SecurityRequirements on the card when
// WithAuthenticator is in use, so callers can discover how to authenticate.
func WithCard(card *a2a.AgentCard) Option {
	return func(s *Server) { s.cardProvider = &StaticCard{Card: *card} }
}

// WithCardProvider sets a dynamic agent card provider.
func WithCardProvider(p AgentCardProvider) Option {
	return func(s *Server) { s.cardProvider = p }
}

// WithExtendedCardProvider sets the extended agent card: the card
// GetExtendedAgentCard returns to a caller that has authenticated (A2A 1.0
// §3.1.11), which may name skills or details the public card leaves out. The
// public card then declares capabilities.extendedAgentCard, and the server
// completes the extended card as WithCard describes. The provider is given
// the request, so it can tailor the card to the caller; use WithAuthenticator
// so that only authenticated callers reach it. A *StaticCard serves the same
// card to everyone.
func WithExtendedCardProvider(p AgentCardProvider) Option {
	return func(s *Server) { s.extendedCard = p }
}

// WithCardMaxAge sets the max-age of the Cache-Control header on the served
// agent card (A2A 1.0 §8.6): how long a client or cache may reuse the card
// before asking again. Without it, a card set with WithCard (or none) gets
// five minutes and a card from a dynamic AgentCardProvider gets "no-cache".
// Zero or less sends "no-cache", so clients revalidate every time; the card's
// ETag keeps that to a 304 while the card is unchanged.
func WithCardMaxAge(d time.Duration) Option {
	return func(s *Server) {
		s.cardMaxAge = d
		s.cardMaxAgeSet = true
	}
}

// WithPort sets the TCP port for ListenAndServe. It has no effect on Serve.
func WithPort(port int) Option {
	return func(s *Server) { s.port = port }
}

// WithLogger sets the logger the server writes its diagnostics to. Without
// it, the server uses PromptKit's logger (logger.GetLogger), so
// logger.SetLogger and logger.Configure apply.
func WithLogger(l *slog.Logger) Option {
	return func(s *Server) { s.customLogger = l }
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

// WithWriteTimeout sets the HTTP server's WriteTimeout for ListenAndServe and
// Serve: the most time a response may take, from the end of reading the
// request headers. Default: 0, no write timeout. SSE streams
// (SendStreamingMessage, SubscribeToTask) lift it for their own connection, so
// they stay open for as long as the turn runs; a blocking SendMessage does
// not, so set it above the longest turn a caller waits on, or cap that wait
// with WithMaxBlockingWait.
func WithWriteTimeout(d time.Duration) Option {
	return func(s *Server) { s.writeTimeout = d }
}

// WithMaxBlockingWait caps how long a blocking SendMessage holds its request
// open. When the turn has not finished or been interrupted by then, the server
// answers with the task in its current (working) state, and the turn runs on:
// the caller polls GetTask or subscribes for the rest. Default: 0, no cap. A
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

// WithMaxBodySize sets the maximum allowed request body size in bytes for
// JSON-RPC requests. Default: 10 MB.
func WithMaxBodySize(n int64) Option {
	return func(s *Server) { s.maxBodySize = n }
}

// WithTaskTTL sets how long completed/failed/canceled tasks are retained
// before automatic eviction. Default: 1 hour. Set to 0 to disable eviction.
func WithTaskTTL(d time.Duration) Option {
	return func(s *Server) { s.taskTTL = d }
}

// WithInterruptedTaskTTL sets how long a task left waiting for the caller
// (input-required or auth-required) is kept, from its last status change,
// before it is evicted; GetTask then reports it not found. A caller that never
// comes back would otherwise leave it in the store for good, as does a client
// that resumes without the task's taskId, which starts a new task instead.
// Default: 24 hours. Set to 0 to keep waiting tasks until they are canceled.
//
// The default in-memory store supports it. A custom TaskStore takes part by
// implementing InterruptedTaskEvictor; one that does not is left to bound its
// waiting tasks itself.
func WithInterruptedTaskTTL(d time.Duration) Option {
	return func(s *Server) { s.interruptedTTL = d }
}

// WithConversationTTL sets how long idle conversations are retained before
// automatic eviction. A conversation is considered idle when its last-use
// timestamp exceeds this duration. Default: 1 hour. Set to 0 to disable.
func WithConversationTTL(d time.Duration) Option {
	return func(s *Server) { s.convTTL = d }
}

// WithCompleteOnEviction makes idle eviction (WithConversationTTL) end a
// conversation rather than suspend it: the server calls Complete on an evicted
// conversation that implements CompletableConversation, and Close on any other.
// Set it when the opener starts a fresh conversation for each context ID, so
// an evicted conversation cannot carry on. Leave it unset when the opener
// resumes stored state, where eviction is a pause and completing at each one
// would score the conversation more than once. Shutdown always calls Close.
func WithCompleteOnEviction() Option {
	return func(s *Server) { s.completeOnEviction = true }
}

// WithoutStreaming turns streaming off. The served card declares
// capabilities.streaming false, and SendStreamingMessage and SubscribeToTask
// (0.3: message/stream, tasks/resubscribe) fail with UnsupportedOperationError
// (-32004), as A2A 1.0 §3.3.4 requires of an agent that does not declare
// streaming. Callers send with SendMessage and poll GetTask. Use it when
// something in front of the server cannot carry Server-Sent Events.
func WithoutStreaming() Option {
	return func(s *Server) { s.noStreaming = true }
}

// WithAuthenticator sets an authenticator for incoming JSON-RPC requests.
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
	opener        RequestConversationOpener
	handler       MessageHandler
	taskStore     TaskStore
	cardProvider  AgentCardProvider
	extendedCard  AgentCardProvider
	cardMaxAge    time.Duration
	cardMaxAgeSet bool
	authenticator Authenticator
	port          int
	httpSrv       *http.Server
	httpSrvMu     sync.Mutex

	readTimeout     time.Duration
	writeTimeout    time.Duration
	maxBlockingWait time.Duration
	idleTimeout     time.Duration
	maxBodySize     int64

	// noStreaming turns streaming off (WithoutStreaming).
	noStreaming bool

	// Readiness flag: set to true after NewServer completes, false on Shutdown.
	isReady atomic.Bool

	// customLogger is the WithLogger logger; nil means PromptKit's logger.
	customLogger *slog.Logger

	// Optional health checkers evaluated by /readyz.
	healthChecks []namedChecker

	// TTL-based eviction configuration.
	taskTTL        time.Duration // 0 disables task eviction
	interruptedTTL time.Duration // 0 disables eviction of waiting tasks
	convTTL        time.Duration // 0 disables conversation eviction
	// completeOnEviction makes eviction call Complete (WithCompleteOnEviction).
	completeOnEviction bool
	stopOnce           sync.Once
	stopCh             chan struct{} // closed to stop the eviction goroutine

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
	// convOpening (under convsMu) holds a channel per context whose
	// conversation is being opened, closed when the open finishes.
	convOpening map[string]chan struct{}
	// convCaller, when set (NewServerFor), identifies the caller a
	// conversation acts for; a context's conversation then belongs to the
	// caller who opened it.
	convCaller OwnerFunc

	// handlers serves each protocol operation.
	handlers map[a2a.Operation]func(*rpcCall)
}

// NewServer creates a new A2A server that owns its conversations: it opens one
// per context id through the supplied opener, caches it, and reuses it when
// that id returns. Suits an embedder running A2A and the runtime in one
// process. A background goroutine evicts expired tasks and idle conversations
// until Shutdown.
//
// For an embedder that owns conversations itself (because the runtime lives
// elsewhere, or it already tracks sessions), see [NewStatelessServer].
func NewServer(opener ConversationOpener, opts ...Option) *Server {
	s := newServer(opts...)
	s.opener = func(_ *http.Request, contextID string) (Conversation, error) {
		return opener(contextID)
	}
	return s
}

// NewServerFor is NewServer for conversations that act for the caller of
// the request opening them. callerID identifies the caller of each request,
// typically from what the host's authentication middleware put on it; a
// request it gives no identity is refused. opener is given the request
// opening each context, so the conversation can act for that caller.
//
// A context's conversation belongs to the caller who opened it: a message
// from another caller naming that context is refused, whether or not tasks
// are scoped with WithTaskOwner. Tasks are scoped by callerID as
// WithTaskOwner scopes them: GetTask, CancelTask, ListTasks and
// SubscribeToTask see only the caller's own tasks, and another caller's task
// is reported as not found. A WithTaskOwner in opts overrides callerID for
// task scoping, and like it the TaskStore must implement OwnedTaskStore.
// Openers run outside the server's lock, one at a time per context, so a slow
// open does not hold up other contexts.
func NewServerFor(callerID OwnerFunc, opener RequestConversationOpener, opts ...Option) *Server {
	// Tasks are scoped by the same identity unless WithTaskOwner says
	// otherwise: it comes later in the options, so it overrides.
	scopeTasks := func(s *Server) { s.owner = callerID }
	s := newServer(append([]Option{scopeTasks}, opts...)...)
	s.opener = opener
	s.convCaller = callerID
	return s
}

// newServer builds the parts both modes share.
func newServer(opts ...Option) *Server {
	s := &Server{
		convs:          make(map[string]Conversation),
		convLastUse:    make(map[string]time.Time),
		convOwner:      make(map[string]string),
		convOpening:    make(map[string]chan struct{}),
		cancels:        make(map[string]context.CancelFunc),
		cancelRegs:     make(map[string]uint64),
		readTimeout:    defaultReadTimeout,
		idleTimeout:    defaultIdleTimeout,
		maxBodySize:    defaultMaxBodySize,
		taskTTL:        defaultTaskTTL,
		interruptedTTL: defaultInterruptedTaskTTL,
		convTTL:        defaultConversationTTL,
		stopCh:         make(chan struct{}),
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.taskStore == nil {
		s.taskStore = NewInMemoryTaskStore()
	}
	s.checkOwnershipConfig()
	if s.events == nil {
		s.events = newLocalTaskEvents(s.log)
	}
	if s.canceler == nil {
		s.canceler = &localCanceler{}
	}
	s.stopCancelFeed = s.canceler.Listen(s.cancelLocal)
	s.handlers = s.rpcHandlers()

	// Start background eviction if at least one TTL is enabled.
	if s.taskTTL > 0 || s.interruptedTTL > 0 || s.convTTL > 0 {
		go s.evictionLoop()
	}

	s.isReady.Store(true)

	return s
}

// Handler returns an http.Handler implementing the A2A protocol. It serves the
// agent card at GET /.well-known/agent-card.json and /.well-known/agent.json,
// JSON-RPC at POST /a2a, and the probes GET /healthz and GET /readyz. Requests
// are traced with OpenTelemetry, and a panic in a handler is recovered and
// answered with a JSON-RPC internal error.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+a2a.AgentCardPath, s.handleAgentCard)
	mux.HandleFunc("GET "+a2a.LegacyAgentCardPath, s.handleAgentCard)
	mux.HandleFunc("POST /a2a", s.handleRPC)
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	return otelhttp.NewHandler(recoveryMiddleware(mux, s.log), "a2a-server")
}

// recoveryMiddleware wraps an http.Handler with panic recovery.
// On panic it logs the stack trace and returns a 500 JSON-RPC error response.
func recoveryMiddleware(next http.Handler, log func() *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				stack := debug.Stack()
				log().ErrorContext(r.Context(), "a2a: panic recovered", "panic", fmt.Sprint(rec), "stack", string(stack))
				writeRPCError(w, nil, a2a.ErrCodeInternal, "Internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ListenAndServe starts the HTTP server on the configured port and blocks until
// it stops. The server applies the read, write and idle timeouts the options
// set; see WithWriteTimeout for how SSE streams treat the write timeout.
func (s *Server) ListenAndServe() error {
	return s.newHTTPServer(fmt.Sprintf(":%d", s.port)).ListenAndServe()
}

// newHTTPServer builds the http.Server that ListenAndServe and Serve run, and
// records it for Shutdown.
func (s *Server) newHTTPServer(addr string) *http.Server {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		ReadTimeout:       s.readTimeout,
		WriteTimeout:      s.writeTimeout,
		IdleTimeout:       s.idleTimeout,
	}

	s.httpSrvMu.Lock()
	s.httpSrv = srv
	s.httpSrvMu.Unlock()
	return srv
}

// Shutdown gracefully shuts down the server: it marks the server not ready,
// stops the eviction goroutine, drains HTTP requests, ends task subscriptions,
// cancels in-flight tasks, and closes all conversations. It returns the first
// error from stopping the HTTP server or closing a conversation.
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

// Serve starts the HTTP server on the given listener and blocks until it stops.
// It applies the same timeouts as ListenAndServe.
func (s *Server) Serve(ln net.Listener) error {
	return s.newHTTPServer("").Serve(ln)
}

// JSON keys and values used in the health probe response bodies.
const (
	healthKeyStatus      = "status"
	healthKeyReason      = "reason"
	healthStatusNotReady = "not_ready"
)

// handleHealthz is a liveness probe: it returns 200 whenever the HTTP server
// is accepting connections.
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{healthKeyStatus: "ok"})
}

// handleReadyz is a readiness probe: it returns 200 when the server is ready
// to accept traffic and 503 otherwise. It checks the isReady flag and all
// registered health checkers.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if !s.isReady.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{
			healthKeyStatus: healthStatusNotReady,
			healthKeyReason: "server is shutting down or not yet initialized",
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
			healthKeyStatus: healthStatusNotReady,
			healthKeyReason: "one or more health checks failed",
			"checks":        checks,
		})
		return
	}

	resp := map[string]any{healthKeyStatus: "ready"}
	if len(checks) > 0 {
		resp["checks"] = checks
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// handleAgentCard serves the agent card in the caller's protocol version: the
// 1.0 card when the request asks for 1.0, otherwise the 0.3 card, which also
// carries supportedInterfaces so a 1.0 client that sent no header still finds
// its interface. The response is cacheable (A2A 1.0 §8.6): see writeCacheable
// and WithCardMaxAge.
func (s *Server) handleAgentCard(w http.ResponseWriter, r *http.Request) {
	card, err := s.card(r)
	if err != nil {
		s.log().ErrorContext(r.Context(), "a2a: failed to get agent card", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	v, _, err := a2a.RequestVersion(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	body, err := json.Marshal(v.WireAgentCard(card))
	if err != nil {
		s.log().ErrorContext(r.Context(), "a2a: failed to encode agent card", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	body = append(body, '\n')
	writeCacheable(w, r, body, s.cardCacheMaxAge())
}

// cardCacheMaxAge is the served card's max-age: WithCardMaxAge when set,
// otherwise the default for a static card and none (no-cache) for a dynamic
// provider's, which may vary per request.
func (s *Server) cardCacheMaxAge() time.Duration {
	if s.cardMaxAgeSet {
		return s.cardMaxAge
	}
	if _, static := s.cardProvider.(*StaticCard); s.cardProvider != nil && !static {
		return 0
	}
	return defaultCardMaxAge
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
	return s.serve(card, r), nil
}

// serve completes card for publishing to r (see servedCard), declaring the
// extended card when there is one.
func (s *Server) serve(card *a2a.AgentCard, r *http.Request) *a2a.AgentCard {
	served := servedCard(card, r, !s.noStreaming)
	if s.extendedCard != nil {
		served.Capabilities.ExtendedAgentCard = true
	}
	return served
}

// handleGetExtendedAgentCard processes GetExtendedAgentCard (0.3:
// agent/getAuthenticatedExtendedCard): the WithExtendedCardProvider card, in
// the caller's version. Without one, the answer depends only on whether the
// public card declares one (A2A 1.0 §3.3.4): UnsupportedOperation when it
// does not, ExtendedAgentCardNotConfigured when it does.
func (s *Server) handleGetExtendedAgentCard(call *rpcCall) {
	if s.extendedCard != nil {
		extended, err := s.extendedCard.AgentCard(call.r)
		if err != nil {
			call.internalError("failed to get extended agent card", err)
			return
		}
		call.result(call.v.WireAgentCard(s.serve(extended, call.r)))
		return
	}
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
			s.log().DebugContext(r.Context(), "a2a: authentication failed", "error", err)
			writeRPCErrorWithStatus(w, http.StatusUnauthorized, nil, -32000, "Authentication failed")
			return
		}
	}

	r.Body = http.MaxBytesReader(w, r.Body, s.maxBodySize)

	in, refusal := a2a.ReadServerRequest(r)
	if refusal != nil {
		writeRPCResponse(w, http.StatusOK, refusal)
		return
	}
	req, op, v := &in.JSONRPCRequest, in.Operation, in.Version
	call := &rpcCall{w: w, r: r, req: req, v: v, log: s.log}
	if v != a2a.ProtocolVersion03 {
		call.tenant = requestTenant(req.Params)
	}
	if s.owner != nil {
		if call.owner = s.owner(r); call.owner == "" {
			writeRPCErrorWithStatus(w, http.StatusUnauthorized, req.ID, -32000, "Caller identity required")
			return
		}
	}
	if s.convCaller != nil {
		if call.caller = s.convCaller(r); call.caller == "" {
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
		a2a.OpSendStreamingMessage: s.ifStreaming(s.handleStreamMessage),
		a2a.OpGetTask:              s.handleGetTask,
		a2a.OpCancelTask:           s.handleCancelTask,
		a2a.OpListTasks:            s.handleListTasks,
		a2a.OpSubscribeToTask:      s.ifStreaming(s.handleTaskSubscribe),
		a2a.OpPushNotificationConfig: func(call *rpcCall) {
			call.fail(a2a.ErrCodePushNotificationNotSupported, errPushNotSupported)
		},
		a2a.OpGetExtendedAgentCard: s.handleGetExtendedAgentCard,
		a2a.OpUnknown: func(call *rpcCall) {
			call.fail(a2a.ErrCodeMethodNotFound, "Method not found")
		},
	}
}

// ifStreaming serves a streaming method with handle, or, when streaming is
// off, refuses it as A2A 1.0 §3.3.4 requires.
func (s *Server) ifStreaming(handle func(*rpcCall)) func(*rpcCall) {
	if !s.noStreaming {
		return handle
	}
	return func(call *rpcCall) {
		call.fail(a2a.ErrCodeUnsupportedOperation, "Streaming is not supported by this agent")
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
	// msg is the caller's message, recorded in the task's history once the
	// turn begins.
	msg *a2a.Message
}

// resolveTarget works out which task a message runs on. A message without a
// taskId starts a new task in its context (a new context when it names
// none). One with a taskId continues that task, which must exist and be
// visible to the caller (else TaskNotFound), must not have finished (else
// UnsupportedOperation, §3.1.1), and supplies the context — a contextId that
// differs from the task's is refused (§3.4.3). ok is false when the call has
// been answered with an error.
func (s *Server) resolveTarget(call *rpcCall, msg *a2a.Message) (target turnTarget, ok bool) {
	target.msg = msg
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
	return turnTarget{taskID: task.ID, contextID: task.ContextID, existing: true, prior: task.Status, msg: msg}, true
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
		s.recordCallerMessage(target)
		s.setState(target.taskID, target.contextID, a2a.TaskStateWorking, nil)
		target.claimed = true
		return true
	}

	switch err := s.taskStore.SetState(target.taskID, a2a.TaskStateWorking, nil); {
	case err == nil:
		s.recordCallerMessage(target)
		s.publishStatus(target.taskID, target.contextID, s.recordedStatus(target.taskID, a2a.TaskStateWorking, nil))
		target.claimed = true
		return true
	case errors.Is(err, ErrTaskNotFound):
		call.failTask(a2a.ErrCodeTaskNotFound, "Task not found", target.taskID)
	case errors.Is(err, ErrTaskTerminal), errors.Is(err, ErrInvalidTransition), errors.Is(err, ErrTransitionRefused):
		if errors.Is(err, ErrTransitionRefused) {
			s.logStateWriteError(target.taskID, a2a.TaskStateWorking, err)
		}
		call.fail(a2a.ErrCodeUnsupportedOperation,
			"Task is not waiting for input; it cannot take a message now")
	default:
		call.internalError(fmt.Sprintf("failed to resume task %s", target.taskID), err)
	}
	return false
}

// recordCallerMessage records the message a turn begins with in its task's
// history, naming the task and its context.
func (s *Server) recordCallerMessage(target *turnTarget) {
	if target.msg == nil {
		return
	}
	msg := *target.msg
	msg.TaskID, msg.ContextID = target.taskID, target.contextID
	s.recordHistory(target.taskID, &msg)
}

// recordHistory appends msg to the task's history, when the store keeps one
// (TaskHistoryStore) and there is a message.
func (s *Server) recordHistory(taskID string, msg *a2a.Message) {
	history, ok := s.taskStore.(TaskHistoryStore)
	if !ok || msg == nil {
		return
	}
	if err := history.AppendHistory(taskID, *msg); err != nil {
		s.log().Warn("a2a: failed to record task history", "task_id", taskID, "error", err)
	}
}

// handleSendMessage processes SendMessage (0.3: message/send).
func (s *Server) handleSendMessage(call *rpcCall) {
	var params a2a.SendMessageRequest
	if !call.decodeSendParams(&params) {
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

	// A tool-result message resumes a conversation; any other message is
	// converted before a conversation is opened for it, so one the server
	// refuses opens nothing.
	toolResults := extractToolResults(params.Message.Parts)
	var pkMsg *types.Message
	if len(toolResults) == 0 {
		if pkMsg = convertMessage(call, &params.Message); pkMsg == nil {
			return
		}
	}

	conv := s.openConversation(call, target.contextID)
	if conv == nil {
		return
	}

	if len(toolResults) > 0 {
		s.handleToolResultMessage(call, conv, &target, toolResults, params.Configuration)
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

// convertMessage converts an inbound message for a conversation. Content the
// server cannot type fails the call with ContentTypeNotSupportedError, and
// any other unconvertible part with InvalidParams; it returns nil then.
func convertMessage(call *rpcCall, msg *a2a.Message) *types.Message {
	pkMsg, err := a2a.MessageToMessage(msg)
	if err == nil {
		return pkMsg
	}
	code := a2a.ErrCodeInvalidParams
	if errors.Is(err, a2a.ErrContentTypeNotSupported) {
		code = a2a.ErrCodeContentTypeNotSupported
	}
	call.fail(code, fmt.Sprintf("Invalid message: %v", err))
	return nil
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
	// The message becomes history only once its results are taken.
	msg := target.msg
	target.msg = nil
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
	target.msg = msg
	s.recordCallerMessage(target)
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
	// Not setState: the status message being restored is in the history
	// already.
	if err := s.taskStore.SetState(target.taskID, target.prior.State, target.prior.Message); err != nil {
		s.logStateWriteError(target.taskID, target.prior.State, err)
		return
	}
	s.publishStatus(target.taskID, target.contextID,
		s.recordedStatus(target.taskID, target.prior.State, target.prior.Message))
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

// setState records a state change, adds its status message to the task's
// history, and publishes it to the task's subscribers. A failed store write
// is logged and not published: a subscriber must never see a state the store
// does not hold.
func (s *Server) setState(taskID, contextID string, state a2a.TaskState, msg *a2a.Message) {
	if err := s.taskStore.SetState(taskID, state, msg); err != nil {
		s.logStateWriteError(taskID, state, err)
		return
	}
	s.recordHistory(taskID, msg)
	s.publishStatus(taskID, contextID, s.recordedStatus(taskID, state, msg))
}

// recordedStatus returns the status the store holds for taskID just after it
// recorded state, so an emitted status carries exactly the timestamp GetTask
// returns. If the store cannot be read, or the task has moved on already (a
// CancelTask got in between), the status is state and msg, stamped now.
func (s *Server) recordedStatus(taskID string, state a2a.TaskState, msg *a2a.Message) a2a.TaskStatus {
	if task, err := s.taskStore.Get(taskID); err == nil && task.Status.State == state {
		return stamped(task.Status)
	}
	return stamped(a2a.TaskStatus{State: state, Message: msg})
}

// log returns the logger the server writes to: the WithLogger logger, or
// PromptKit's logger, read on each call so a later logger.SetLogger applies.
func (s *Server) log() *slog.Logger {
	if s.customLogger != nil {
		return s.customLogger
	}
	return logger.GetLogger()
}

// logStateWriteError logs a task store's refusal to record a state. A
// refusal the store marks with ErrTransitionRefused is intended (a task held
// for approval, say) and is logged at debug; anything else is a warning.
func (s *Server) logStateWriteError(taskID string, state a2a.TaskState, err error) {
	if errors.Is(err, ErrTransitionRefused) {
		s.log().Debug("a2a: task state change refused by the store",
			"task_id", taskID, "state", string(state), "error", err)
		return
	}
	s.log().Warn("a2a: failed to set task state", "task_id", taskID, "state", string(state), "error", err)
}

// publishStatus publishes a status update to the task's subscribers.
func (s *Server) publishStatus(taskID, contextID string, status a2a.TaskStatus) {
	s.publish(taskID, TaskEvent{StatusUpdate: &a2a.TaskStatusUpdateEvent{
		TaskID: taskID, ContextID: contextID, Status: stamped(status),
	}})
}

// stamped returns status with a timestamp: its own, or now when it has none.
// Every status update says when its status was recorded.
func stamped(status a2a.TaskStatus) a2a.TaskStatus {
	if status.Timestamp == nil {
		now := time.Now().UTC()
		status.Timestamp = &now
	}
	return status
}

// publish delivers an event to the task's subscribers.
func (s *Server) publish(taskID string, evt TaskEvent) {
	if err := s.events.Publish(context.Background(), taskID, evt); err != nil {
		s.log().Warn("a2a: failed to publish task event", "task_id", taskID, "error", err)
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
			s.log().Warn("a2a: failed to add task artifacts", "task_id", taskID, "error", err)
		}
	} else if text := resp.Text(); text != "" {
		// Fallback: if Parts() is empty (see GH-428), use Text() content.
		artifacts = []a2a.Artifact{{
			ArtifactID: "artifact-1",
			Parts:      []a2a.Part{{Text: &text}},
		}}
		if err := s.taskStore.AddArtifacts(taskID, artifacts); err != nil {
			s.log().Warn("a2a: failed to add task text artifact", "task_id", taskID, "error", err)
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
		call.failTask(a2a.ErrCodeTaskNotCancelable,
			fmt.Sprintf("Task cannot be canceled: it is %s", task.Status.State.V03Name()), params.ID)
		return
	}

	switch cancelErr := s.taskStore.Cancel(params.ID); {
	case errors.Is(cancelErr, ErrTaskTerminal):
		// Finished between the read and the cancel.
		call.failTask(a2a.ErrCodeTaskNotCancelable, "Task cannot be canceled: it has already finished", params.ID)
		return
	case errors.Is(cancelErr, ErrTaskNotFound):
		call.failTask(a2a.ErrCodeTaskNotFound, "Task not found", params.ID)
		return
	case cancelErr != nil:
		call.internalError(fmt.Sprintf("task cancel failed for %s", params.ID), cancelErr)
		return
	}

	if err := s.canceler.Cancel(context.WithoutCancel(call.r.Context()), params.ID); err != nil {
		// The task is canceled in the store either way; the turn may run on
		// until it next checks, but its result can no longer be recorded.
		s.log().Warn("a2a: failed to reach a task's turn to cancel it", "task_id", params.ID, "error", err)
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
	if err := checkStatusFilter(call.req.Params); err != nil {
		call.fail(a2a.ErrCodeInvalidParams, "Invalid params: status: "+err.Error())
		return
	}
	if params.ContextID == "" && s.owner == nil {
		call.fail(a2a.ErrCodeInvalidParams,
			"Invalid params: contextId is required unless the server scopes tasks by caller")
		return
	}

	limit := listPageSize(call.v, params.PageSize)
	status := statusFilter(call.v, params.Status)
	offset, err := decodePageToken(params.PageToken)
	if err != nil {
		call.fail(a2a.ErrCodeInvalidParams, "Invalid params: "+err.Error())
		return
	}

	page, err := queryTasks(s.taskStore, TaskQuery{
		Owner:       call.owner,
		Tenant:      call.tenant,
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

// listPageSize is the page size a ListTasks in version v gets for the size it
// asked for: the default when unset, capped at the maximum.
func listPageSize(v a2a.ProtocolVersion, requested int) int {
	switch {
	case requested <= 0 && v == a2a.ProtocolVersion03:
		return maxPageSize
	case requested <= 0:
		return defaultPageSize
	case requested > maxPageSize:
		return maxPageSize
	}
	return requested
}

// statusFilter is the state a ListTasks in version v filters by, nil for
// none. In 1.0, TASK_STATE_UNSPECIFIED is the proto's zero value: no status
// filter. In 0.3 the same TaskState is the real "unknown" state.
func statusFilter(v a2a.ProtocolVersion, status *a2a.TaskState) *a2a.TaskState {
	if status != nil && *status == a2a.TaskStateUnknown && v != a2a.ProtocolVersion03 {
		return nil
	}
	return status
}

// getOrCreateConversation retrieves an existing conversation for the context ID
// or creates a new one via the opener. It also updates the last-use timestamp
// for conversation TTL tracking.
//
// A conversation belongs to the caller that opened it (conversationOwner):
// another caller naming its context gets errContextTaken, however the two
// race. The opener runs outside convsMu, one at a time per context
// (convOpening), so a slow open holds up only messages for its own context.
func (s *Server) getOrCreateConversation(call *rpcCall, contextID string) (Conversation, error) {
	owner := s.conversationOwner(call)
	for {
		s.convsMu.Lock()
		if cached, ok := s.convs[contextID]; ok {
			conv, err := s.reuseConversation(cached, contextID, owner)
			s.convsMu.Unlock()
			return conv, err
		}
		if opening, ok := s.convOpening[contextID]; ok {
			s.convsMu.Unlock()
			// Wait for the other open, then look again; a client that goes
			// away stops waiting.
			select {
			case <-opening:
				continue
			case <-call.r.Context().Done():
				return nil, call.r.Context().Err()
			}
		}
		if err := s.checkContextTasks(call, contextID); err != nil {
			s.convsMu.Unlock()
			return nil, err
		}
		opening := make(chan struct{})
		s.convOpening[contextID] = opening
		s.convsMu.Unlock()
		return s.openNewConversation(call, contextID, owner, opening)
	}
}

// reuseConversation hands back contextID's cached conversation conv when
// owner may use it, marking it used. Caller must hold convsMu.
func (s *Server) reuseConversation(conv Conversation, contextID, owner string) (Conversation, error) {
	if s.convOwner[contextID] != owner {
		return nil, errContextTaken
	}
	s.convLastUse[contextID] = time.Now()
	return conv, nil
}

// openNewConversation runs the opener for contextID, outside convsMu, and
// records the conversation for owner. However the opener ends (a
// conversation, an error or a panic), the open is cleared and opening closed,
// so messages waiting on the same context look again and the next one opens
// it afresh.
func (s *Server) openNewConversation(
	call *rpcCall, contextID, owner string, opening chan struct{},
) (Conversation, error) {
	var conv Conversation
	defer func() {
		s.convsMu.Lock()
		defer s.convsMu.Unlock()
		delete(s.convOpening, contextID)
		close(opening)
		if conv == nil {
			return
		}
		s.convs[contextID] = conv
		s.convLastUse[contextID] = time.Now()
		s.convOwner[contextID] = owner
	}()

	opened, err := s.opener(call.r, contextID)
	if err != nil {
		return nil, err
	}
	conv = opened
	return conv, nil
}

// conversationOwner is who a conversation the call opens belongs to: the
// caller NewServerFor identified, else the task owner (both "" when the
// server does not tell callers apart), in the request's tenant.
func (s *Server) conversationOwner(call *rpcCall) string {
	owner := call.owner
	if s.convCaller != nil {
		owner = call.caller
	}
	if call.tenant == "" {
		return owner
	}
	return owner + "\x00" + call.tenant
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
func writeRPCResult(w http.ResponseWriter, id, result any, log *slog.Logger) {
	data, err := json.Marshal(result)
	if err != nil {
		log.Error("a2a: failed to marshal RPC result", "error", err)
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
	writeRPCErrorObject(w, http.StatusOK, id, &a2a.JSONRPCError{Code: code, Message: msg})
}

// writeRPCErrorWithStatus writes a JSON-RPC 2.0 error response with a specific HTTP status code.
func writeRPCErrorWithStatus(w http.ResponseWriter, status int, id any, code int, msg string) {
	writeRPCErrorObject(w, status, id, &a2a.JSONRPCError{Code: code, Message: msg})
}

// writeRPCErrorObject writes a JSON-RPC 2.0 error response carrying e, with
// HTTP status status.
func writeRPCErrorObject(w http.ResponseWriter, status int, id any, e *a2a.JSONRPCError) {
	writeRPCResponse(w, status, &a2a.JSONRPCResponse{JSONRPC: jsonRPCVersion, ID: id, Error: e})
}

// writeRPCResponse writes resp with HTTP status status.
func writeRPCResponse(w http.ResponseWriter, status int, resp *a2a.JSONRPCResponse) {
	w.Header().Set("Content-Type", "application/json")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	_ = json.NewEncoder(w).Encode(resp)
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
	s.evictInterruptedTasks(now)
	s.evictIdleConversations(now)
}

// evictTerminalTasks removes expired terminal tasks and their associated
// cancel functions.
func (s *Server) evictTerminalTasks(now time.Time) {
	if s.taskTTL <= 0 {
		return
	}
	for _, taskID := range s.taskStore.EvictTerminal(now.Add(-s.taskTTL)) {
		s.forgetCancel(taskID)
	}
}

// evictInterruptedTasks removes tasks left waiting for the caller past
// WithInterruptedTaskTTL, when the store can.
func (s *Server) evictInterruptedTasks(now time.Time) {
	evictor, ok := s.taskStore.(InterruptedTaskEvictor)
	if !ok || s.interruptedTTL <= 0 {
		return
	}
	for _, taskID := range evictor.EvictInterrupted(now.Add(-s.interruptedTTL)) {
		s.forgetCancel(taskID)
	}
}

// endEvicted releases an evicted conversation: it completes it when
// WithCompleteOnEviction is set and the conversation can be completed, and
// closes it otherwise.
func (s *Server) endEvicted(contextID string, conv Conversation) {
	end, verb := conv.Close, "close"
	if c, ok := conv.(CompletableConversation); ok && s.completeOnEviction {
		end, verb = c.Complete, "complete"
	}
	if err := end(); err != nil {
		s.log().Warn("a2a: failed to "+verb+" evicted conversation", "context_id", contextID, "error", err)
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
				s.endEvicted(id, conv)
				delete(s.convs, id)
			}
			delete(s.convLastUse, id)
			delete(s.convOwner, id)
		}
	}
}
