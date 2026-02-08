package a2a

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

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	// idBytes is the number of random bytes used to generate task/context IDs.
	idBytes = 16

	// defaultReadHeaderTimeout prevents Slowloris attacks.
	defaultReadHeaderTimeout = 10 * time.Second

	// defaultPageSize is used when ListTasksRequest.PageSize is 0.
	defaultPageSize = 100

	// sendSettleTime is how long handleSendMessage waits for fast calls
	// to complete before returning the task in its current state.
	sendSettleTime = 5 * time.Millisecond
)

// ConversationResult holds the outcome of a Send call.
type ConversationResult struct {
	Parts        []types.ContentPart
	PendingTools bool
}

// Conversation abstracts a single conversation session.
type Conversation interface {
	Send(ctx context.Context, msg *types.Message) (*ConversationResult, error)
	Close() error
}

// ConversationOpener creates or retrieves a Conversation for a context ID.
type ConversationOpener func(contextID string) (Conversation, error)

// ServerOption configures a [Server].
type ServerOption func(*Server)

// WithCard sets the agent card served at /.well-known/agent.json.
func WithCard(card *AgentCard) ServerOption {
	return func(s *Server) { s.card = *card }
}

// WithPort sets the TCP port for ListenAndServe.
func WithPort(port int) ServerOption {
	return func(s *Server) { s.port = port }
}

// WithTaskStore sets a custom task store. Defaults to an in-memory store.
func WithTaskStore(store TaskStore) ServerOption {
	return func(s *Server) { s.taskStore = store }
}

// Server is an HTTP server that exposes a PromptKit Conversation as an
// A2A-compliant JSON-RPC endpoint.
type Server struct {
	opener    ConversationOpener
	taskStore TaskStore
	card      AgentCard
	port      int
	httpSrv   *http.Server

	convsMu sync.RWMutex
	convs   map[string]Conversation // context_id → Conversation

	cancelsMu sync.Mutex
	cancels   map[string]context.CancelFunc // task_id → cancel for in-flight Send
}

// NewServer creates a new A2A server.
func NewServer(opener ConversationOpener, opts ...ServerOption) *Server {
	s := &Server{
		opener:  opener,
		convs:   make(map[string]Conversation),
		cancels: make(map[string]context.CancelFunc),
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.taskStore == nil {
		s.taskStore = NewInMemoryTaskStore()
	}
	return s
}

// Handler returns an http.Handler implementing the A2A protocol.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/agent.json", s.handleAgentCard)
	mux.HandleFunc("POST /a2a", s.handleRPC)
	return mux
}

// ListenAndServe starts the HTTP server on the configured port.
func (s *Server) ListenAndServe() error {
	s.httpSrv = &http.Server{
		Addr:              fmt.Sprintf(":%d", s.port),
		Handler:           s.Handler(),
		ReadHeaderTimeout: defaultReadHeaderTimeout,
	}
	return s.httpSrv.ListenAndServe()
}

// Shutdown gracefully shuts down the server: drains HTTP requests, cancels
// in-flight tasks, and closes all conversations.
func (s *Server) Shutdown(ctx context.Context) error {
	var firstErr error

	if s.httpSrv != nil {
		firstErr = s.httpSrv.Shutdown(ctx)
	}

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
	s.httpSrv = &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: defaultReadHeaderTimeout,
	}
	return s.httpSrv.Serve(ln)
}

// handleAgentCard serves the agent card as JSON.
func (s *Server) handleAgentCard(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.card)
}

// handleRPC dispatches a JSON-RPC 2.0 request to the appropriate handler.
func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPCError(w, nil, -32700, "Parse error")
		return
	}

	switch req.Method {
	case MethodSendMessage:
		s.handleSendMessage(w, &req)
	case MethodGetTask:
		s.handleGetTask(w, &req)
	case MethodCancelTask:
		s.handleCancelTask(w, &req)
	case MethodListTasks:
		s.handleListTasks(w, &req)
	default:
		writeRPCError(w, req.ID, -32601, "Method not found")
	}
}

// handleSendMessage processes a message/send request.
func (s *Server) handleSendMessage(w http.ResponseWriter, req *JSONRPCRequest) {
	var params SendMessageRequest
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

	pkMsg, err := MessageToMessage(&params.Message)
	if err != nil {
		writeRPCError(w, req.ID, -32602, fmt.Sprintf("Invalid message: %v", err))
		return
	}

	taskID := generateID()
	if _, err := s.taskStore.Create(taskID, contextID); err != nil {
		writeRPCError(w, req.ID, -32000, fmt.Sprintf("Failed to create task: %v", err))
		return
	}

	done := s.runConversation(taskID, conv, pkMsg)

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
func (s *Server) runConversation(taskID string, conv Conversation, pkMsg *types.Message) <-chan struct{} {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelsMu.Lock()
	s.cancels[taskID] = cancel
	s.cancelsMu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()

		_ = s.taskStore.SetState(taskID, TaskStateWorking, nil)

		result, sendErr := conv.Send(ctx, pkMsg)
		if sendErr != nil {
			errText := sendErr.Error()
			_ = s.taskStore.SetState(taskID, TaskStateFailed, &Message{
				Role:  RoleAgent,
				Parts: []Part{{Text: &errText}},
			})
			return
		}

		if result.PendingTools {
			_ = s.taskStore.SetState(taskID, TaskStateInputRequired, nil)
			return
		}

		artifacts, convErr := ContentPartsToArtifacts(result.Parts)
		if convErr == nil && len(artifacts) > 0 {
			_ = s.taskStore.AddArtifacts(taskID, artifacts)
		}
		_ = s.taskStore.SetState(taskID, TaskStateCompleted, nil)
	}()

	return done
}

// handleGetTask processes a tasks/get request.
func (s *Server) handleGetTask(w http.ResponseWriter, req *JSONRPCRequest) {
	var params GetTaskRequest
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
func (s *Server) handleCancelTask(w http.ResponseWriter, req *JSONRPCRequest) {
	var params CancelTaskRequest
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
func (s *Server) handleListTasks(w http.ResponseWriter, req *JSONRPCRequest) {
	var params ListTasksRequest
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
	taskList := make([]Task, len(tasks))
	for i, t := range tasks {
		taskList[i] = *t
	}

	writeRPCResult(w, req.ID, ListTasksResponse{
		Tasks:    taskList,
		PageSize: limit,
	})
}

// getOrCreateConversation retrieves an existing conversation for the context ID
// or creates a new one via the opener (double-check lock pattern).
func (s *Server) getOrCreateConversation(contextID string) (Conversation, error) {
	s.convsMu.RLock()
	if conv, ok := s.convs[contextID]; ok {
		s.convsMu.RUnlock()
		return conv, nil
	}
	s.convsMu.RUnlock()

	s.convsMu.Lock()
	defer s.convsMu.Unlock()

	// Double-check after acquiring write lock.
	if conv, ok := s.convs[contextID]; ok {
		return conv, nil
	}

	conv, err := s.opener(contextID)
	if err != nil {
		return nil, err
	}
	s.convs[contextID] = conv
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
	_ = json.NewEncoder(w).Encode(JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  data,
	})
}

// writeRPCError writes a JSON-RPC 2.0 error response.
func writeRPCError(w http.ResponseWriter, id any, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &JSONRPCError{Code: code, Message: msg},
	})
}
