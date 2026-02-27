package a2aserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
)

const (
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

	// defaultPageSize is used when ListTasksRequest.PageSize is 0.
	defaultPageSize = 100

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

// AgentCardProvider returns the agent card to serve at /.well-known/agent.json.
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

// WithCard sets the agent card served at /.well-known/agent.json.
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

// Server is an HTTP server that exposes a Conversation as an
// A2A-compliant JSON-RPC endpoint.
type Server struct {
	opener        ConversationOpener
	taskStore     TaskStore
	cardProvider  AgentCardProvider
	authenticator Authenticator
	port          int
	httpSrv       *http.Server
	httpSrvMu     sync.Mutex

	readTimeout  time.Duration
	writeTimeout time.Duration
	idleTimeout  time.Duration
	maxBodySize  int64

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

	subsMu sync.Mutex
	subs   map[string]*taskBroadcaster // task_id → broadcaster
}

// NewServer creates a new A2A server.
func NewServer(opener ConversationOpener, opts ...Option) *Server {
	s := &Server{
		opener:       opener,
		convs:        make(map[string]Conversation),
		convLastUse:  make(map[string]time.Time),
		cancels:      make(map[string]context.CancelFunc),
		subs:         make(map[string]*taskBroadcaster),
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

	// Start background eviction if at least one TTL is enabled.
	if s.taskTTL > 0 || s.convTTL > 0 {
		go s.evictionLoop()
	}

	return s
}

// Handler returns an http.Handler implementing the A2A protocol.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/agent.json", s.handleAgentCard)
	mux.HandleFunc("POST /a2a", s.handleRPC)
	return otelhttp.NewHandler(mux, "a2a-server")
}

// ListenAndServe starts the HTTP server on the configured port.
func (s *Server) ListenAndServe() error {
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", s.port),
		Handler:           s.Handler(),
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		ReadTimeout:       s.readTimeout,
		WriteTimeout:      s.writeTimeout,
		IdleTimeout:       s.idleTimeout,
	}

	s.httpSrvMu.Lock()
	s.httpSrv = srv
	s.httpSrvMu.Unlock()

	return srv.ListenAndServe()
}

// Shutdown gracefully shuts down the server: stops the eviction goroutine,
// drains HTTP requests, cancels in-flight tasks, and closes all conversations.
func (s *Server) Shutdown(ctx context.Context) error {
	// Stop the eviction goroutine.
	s.stopOnce.Do(func() { close(s.stopCh) })

	var firstErr error

	s.httpSrvMu.Lock()
	srv := s.httpSrv
	s.httpSrvMu.Unlock()

	if srv != nil {
		firstErr = srv.Shutdown(ctx)
	}

	// Close all broadcasters.
	s.closeAllBroadcasters()

	// Cancel all in-flight tasks.
	s.cancelsMu.Lock()
	for _, cancel := range s.cancels {
		cancel()
	}
	s.cancels = make(map[string]context.CancelFunc)
	s.cancelsMu.Unlock()

	// Close all conversations.
	s.convsMu.Lock()
	for id, conv := range s.convs {
		if err := conv.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(s.convs, id)
	}
	s.convsMu.Unlock()

	return firstErr
}

// Serve starts the HTTP server on the given listener.
func (s *Server) Serve(ln net.Listener) error {
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		ReadTimeout:       s.readTimeout,
		WriteTimeout:      s.writeTimeout,
		IdleTimeout:       s.idleTimeout,
	}

	s.httpSrvMu.Lock()
	s.httpSrv = srv
	s.httpSrvMu.Unlock()

	return srv.Serve(ln)
}

// handleAgentCard serves the agent card as JSON.
func (s *Server) handleAgentCard(w http.ResponseWriter, r *http.Request) {
	if s.cardProvider == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(a2a.AgentCard{})
		return
	}
	card, err := s.cardProvider.AgentCard(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(card)
}

// handleRPC dispatches a JSON-RPC 2.0 request to the appropriate handler.
func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	// Authenticate if configured.
	if s.authenticator != nil {
		if err := s.authenticator.Authenticate(r); err != nil {
			writeRPCError(w, nil, -32000, fmt.Sprintf("Authentication failed: %v", err))
			return
		}
	}

	r.Body = http.MaxBytesReader(w, r.Body, s.maxBodySize)

	var req a2a.JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPCError(w, nil, -32700, "Parse error")
		return
	}

	switch req.Method {
	case a2a.MethodSendMessage:
		s.handleSendMessage(w, r, &req)
	case a2a.MethodSendStreamingMessage:
		s.handleStreamMessage(w, r, &req)
	case a2a.MethodGetTask:
		s.handleGetTask(w, &req)
	case a2a.MethodCancelTask:
		s.handleCancelTask(w, &req)
	case a2a.MethodListTasks:
		s.handleListTasks(w, &req)
	case a2a.MethodTaskSubscribe:
		s.handleTaskSubscribe(w, r, &req)
	default:
		writeRPCError(w, req.ID, -32601, "Method not found")
	}
}

// handleSendMessage processes a message/send request.
func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request, req *a2a.JSONRPCRequest) {
	var params a2a.SendMessageRequest
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeRPCError(w, req.ID, -32602, "Invalid params")
		return
	}

	contextID := params.Message.ContextID
	if contextID == "" {
		contextID = generateID()
	}

	conv, err := s.getOrCreateConversation(contextID)
	if err != nil {
		writeRPCError(w, req.ID, -32000, fmt.Sprintf("Failed to open conversation: %v", err))
		return
	}

	pkMsg, err := a2a.MessageToMessage(&params.Message)
	if err != nil {
		writeRPCError(w, req.ID, -32602, fmt.Sprintf("Invalid message: %v", err))
		return
	}

	taskID := generateID()
	if _, err := s.taskStore.Create(taskID, contextID); err != nil {
		writeRPCError(w, req.ID, -32000, fmt.Sprintf("Failed to create task: %v", err))
		return
	}

	// Propagate trace context to the background goroutine. We use a detached
	// context because the goroutine outlives the HTTP handler on the non-blocking path.
	// Copy the OTel span context so downstream spans nest under the inbound trace.
	bgCtx := trace.ContextWithSpanContext(context.Background(),
		trace.SpanContextFromContext(r.Context()))
	done := s.runConversation(bgCtx, taskID, conv, pkMsg)

	if params.Configuration != nil && params.Configuration.Blocking {
		<-done
	} else {
		select {
		case <-done:
		case <-time.After(sendSettleTime):
		}
	}

	task, _ := s.taskStore.Get(taskID)
	writeRPCResult(w, req.ID, task)
}

// runConversation spawns a goroutine that drives the conversation for a task.
// It returns a channel that is closed when the goroutine completes.
func (s *Server) runConversation(parent context.Context, taskID string, conv Conversation, pkMsg any) <-chan struct{} {
	ctx, cancel := context.WithCancel(parent)
	s.cancelsMu.Lock()
	s.cancels[taskID] = cancel
	s.cancelsMu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()

		_ = s.taskStore.SetState(taskID, a2a.TaskStateWorking, nil)

		resp, sendErr := conv.Send(ctx, pkMsg)
		if sendErr != nil {
			// If the context was canceled (e.g. by CancelTask), don't
			// overwrite the task state — the cancel handler sets it to
			// "canceled". Only mark as failed for genuine errors.
			if ctx.Err() == nil {
				errText := sendErr.Error()
				_ = s.taskStore.SetState(taskID, a2a.TaskStateFailed, &a2a.Message{
					Role:  a2a.RoleAgent,
					Parts: []a2a.Part{{Text: &errText}},
				})
			}
			return
		}

		if resp.HasPendingTools() {
			_ = s.taskStore.SetState(taskID, a2a.TaskStateInputRequired, nil)
			return
		}

		artifacts, convErr := a2a.ContentPartsToArtifacts(resp.Parts())
		if convErr == nil && len(artifacts) > 0 {
			_ = s.taskStore.AddArtifacts(taskID, artifacts)
		} else if text := resp.Text(); text != "" {
			// Fallback: if Parts() is empty (see GH-428), use Text() content.
			_ = s.taskStore.AddArtifacts(taskID, []a2a.Artifact{{
				ArtifactID: "artifact-1",
				Parts:      []a2a.Part{{Text: &text}},
			}})
		}
		_ = s.taskStore.SetState(taskID, a2a.TaskStateCompleted, nil)
	}()

	return done
}

// handleGetTask processes a tasks/get request.
func (s *Server) handleGetTask(w http.ResponseWriter, req *a2a.JSONRPCRequest) {
	var params a2a.GetTaskRequest
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeRPCError(w, req.ID, -32602, "Invalid params")
		return
	}

	task, err := s.taskStore.Get(params.ID)
	if err != nil {
		writeRPCError(w, req.ID, -32001, fmt.Sprintf("Task not found: %v", err))
		return
	}

	writeRPCResult(w, req.ID, task)
}

// handleCancelTask processes a tasks/cancel request.
func (s *Server) handleCancelTask(w http.ResponseWriter, req *a2a.JSONRPCRequest) {
	var params a2a.CancelTaskRequest
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeRPCError(w, req.ID, -32602, "Invalid params")
		return
	}

	// Cancel in-flight Send if running.
	s.cancelsMu.Lock()
	if cancel, ok := s.cancels[params.ID]; ok {
		cancel()
		delete(s.cancels, params.ID)
	}
	s.cancelsMu.Unlock()

	if err := s.taskStore.Cancel(params.ID); err != nil {
		writeRPCError(w, req.ID, -32001, fmt.Sprintf("Cancel failed: %v", err))
		return
	}

	task, _ := s.taskStore.Get(params.ID)
	writeRPCResult(w, req.ID, task)
}

// handleListTasks processes a tasks/list request.
func (s *Server) handleListTasks(w http.ResponseWriter, req *a2a.JSONRPCRequest) {
	var params a2a.ListTasksRequest
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeRPCError(w, req.ID, -32602, "Invalid params")
		return
	}

	limit := params.PageSize
	if limit <= 0 {
		limit = defaultPageSize
	}

	tasks, err := s.taskStore.List(params.ContextID, limit, 0)
	if err != nil {
		writeRPCError(w, req.ID, -32000, fmt.Sprintf("List failed: %v", err))
		return
	}

	// Convert []*Task to []Task for the response.
	taskList := make([]a2a.Task, len(tasks))
	for i, t := range tasks {
		taskList[i] = *t
	}

	writeRPCResult(w, req.ID, a2a.ListTasksResponse{
		Tasks:    taskList,
		PageSize: limit,
	})
}

// getOrCreateConversation retrieves an existing conversation for the context ID
// or creates a new one via the opener (double-check lock pattern).
// It also updates the last-use timestamp for conversation TTL tracking.
func (s *Server) getOrCreateConversation(contextID string) (Conversation, error) {
	s.convsMu.RLock()
	if conv, ok := s.convs[contextID]; ok {
		s.convsMu.RUnlock()
		// Upgrade to write lock to update last-use timestamp.
		s.convsMu.Lock()
		s.convLastUse[contextID] = time.Now()
		s.convsMu.Unlock()
		return conv, nil
	}
	s.convsMu.RUnlock()

	s.convsMu.Lock()
	defer s.convsMu.Unlock()

	// Double-check after acquiring write lock.
	if conv, ok := s.convs[contextID]; ok {
		s.convLastUse[contextID] = time.Now()
		return conv, nil
	}

	conv, err := s.opener(contextID)
	if err != nil {
		return nil, err
	}
	s.convs[contextID] = conv
	s.convLastUse[contextID] = time.Now()
	return conv, nil
}

// generateID returns a random hex string suitable for task and context IDs.
func generateID() string {
	b := make([]byte, idBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// writeRPCResult writes a JSON-RPC 2.0 success response.
func writeRPCResult(w http.ResponseWriter, id, result any) {
	data, _ := json.Marshal(result)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(a2a.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  data,
	})
}

// writeRPCError writes a JSON-RPC 2.0 error response.
func writeRPCError(w http.ResponseWriter, id any, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(a2a.JSONRPCResponse{
		JSONRPC: "2.0",
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

// evictOnce runs a single eviction pass. It is safe to call concurrently.
func (s *Server) evictOnce() {
	now := time.Now()
	s.evictTerminalTasks(now)
	s.evictClosedBroadcasters()
	s.evictIdleConversations(now)
}

// evictTerminalTasks removes expired terminal tasks and their associated
// cancel functions and broadcasters.
func (s *Server) evictTerminalTasks(now time.Time) {
	if s.taskTTL <= 0 {
		return
	}
	evicted := s.taskStore.EvictTerminal(now.Add(-s.taskTTL))
	for _, taskID := range evicted {
		s.cancelsMu.Lock()
		delete(s.cancels, taskID)
		s.cancelsMu.Unlock()

		s.subsMu.Lock()
		if b, ok := s.subs[taskID]; ok {
			b.close()
			delete(s.subs, taskID)
		}
		s.subsMu.Unlock()
	}
}

// evictClosedBroadcasters removes broadcasters that have already been closed.
func (s *Server) evictClosedBroadcasters() {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	for id, b := range s.subs {
		b.mu.Lock()
		closed := b.closed
		b.mu.Unlock()
		if closed {
			delete(s.subs, id)
		}
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
		}
	}
}
