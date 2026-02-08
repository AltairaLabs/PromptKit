// Package mock provides a configurable mock A2A server for use in tests.
//
// It serves canned responses per skill with optional input matching,
// latency injection, and error injection.
package mock

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
)

// Response holds the parts returned by a matched rule.
type Response struct {
	Parts []a2a.Part
}

// mockRule is an internal rule evaluated in order during message/send handling.
type mockRule struct {
	skillID  string
	matcher  func(a2a.Message) bool
	response *Response
	errMsg   string
}

// A2AServer is a lightweight mock A2A server backed by httptest.Server.
type A2AServer struct {
	card    a2a.AgentCard
	rules   []mockRule
	latency time.Duration
	taskSeq atomic.Int64
	ts      *httptest.Server
}

// Option configures an A2AServer.
type Option func(*A2AServer)

// WithSkillResponse adds a rule that returns response for the given skill ID.
func WithSkillResponse(skillID string, response Response) Option {
	return func(m *A2AServer) {
		m.rules = append(m.rules, mockRule{
			skillID:  skillID,
			response: &response,
		})
	}
}

// WithSkillError adds a rule that returns a failed task for the given skill ID.
func WithSkillError(skillID, errMsg string) Option {
	return func(m *A2AServer) {
		m.rules = append(m.rules, mockRule{
			skillID: skillID,
			errMsg:  errMsg,
		})
	}
}

// WithLatency adds a delay before processing each message/send request.
func WithLatency(d time.Duration) Option {
	return func(m *A2AServer) {
		m.latency = d
	}
}

// WithInputMatcher adds a rule that fires when the matcher returns true for the
// given skill ID. Rules are evaluated in order; first match wins.
func WithInputMatcher(skillID string, fn func(a2a.Message) bool, response Response) Option {
	return func(m *A2AServer) {
		m.rules = append(m.rules, mockRule{
			skillID:  skillID,
			matcher:  fn,
			response: &response,
		})
	}
}

// NewA2AServer creates a mock server with the given card and options.
// Call Start to begin serving.
func NewA2AServer(card *a2a.AgentCard, opts ...Option) *A2AServer {
	m := &A2AServer{card: *card}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Start starts the underlying httptest.Server and returns its URL.
func (m *A2AServer) Start() (string, error) {
	m.ts = httptest.NewServer(m.handler())
	return m.ts.URL, nil
}

// Close shuts down the server.
func (m *A2AServer) Close() {
	if m.ts != nil {
		m.ts.Close()
	}
}

// URL returns the URL of the running server. Panics if not started.
func (m *A2AServer) URL() string {
	if m.ts == nil {
		panic("mock: server not started")
	}
	return m.ts.URL
}

// handler builds the http.Handler for the mock server.
func (m *A2AServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/agent.json", m.handleAgentCard)
	mux.HandleFunc("POST /a2a", m.handleRPC)
	return mux
}

// handleAgentCard serves the agent card as JSON.
func (m *A2AServer) handleAgentCard(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(m.card)
}

// handleRPC dispatches a JSON-RPC 2.0 request.
func (m *A2AServer) handleRPC(w http.ResponseWriter, r *http.Request) {
	var req a2a.JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPCError(w, nil, -32700, "Parse error")
		return
	}

	switch req.Method {
	case a2a.MethodSendMessage:
		m.handleSendMessage(w, &req)
	default:
		writeRPCError(w, req.ID, -32601, "Method not found")
	}
}

// handleSendMessage processes a message/send request.
func (m *A2AServer) handleSendMessage(w http.ResponseWriter, req *a2a.JSONRPCRequest) {
	var params a2a.SendMessageRequest
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeRPCError(w, req.ID, -32602, "Invalid params")
		return
	}

	if m.latency > 0 {
		time.Sleep(m.latency)
	}

	skillID := extractSkillID(&params)
	msg := params.Message

	for _, rule := range m.rules {
		if rule.skillID != "" && rule.skillID != skillID {
			continue
		}
		if rule.matcher != nil && !rule.matcher(msg) {
			continue
		}

		taskID := fmt.Sprintf("mock-task-%d", m.taskSeq.Add(1))

		if rule.errMsg != "" {
			writeRPCResult(w, req.ID, m.failedTask(taskID, rule.errMsg))
			return
		}

		if rule.response != nil {
			writeRPCResult(w, req.ID, m.completedTask(taskID, rule.response.Parts))
			return
		}
	}

	writeRPCError(w, req.ID, -32000, "no matching rule")
}

// extractSkillID pulls the skill ID from message metadata or request metadata.
func extractSkillID(params *a2a.SendMessageRequest) string {
	if v, ok := params.Message.Metadata["skillId"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	if v, ok := params.Metadata["skillId"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// completedTask builds a Task with completed status and the given parts as an artifact.
func (m *A2AServer) completedTask(taskID string, parts []a2a.Part) *a2a.Task {
	return &a2a.Task{
		ID:        taskID,
		ContextID: "mock-ctx",
		Status: a2a.TaskStatus{
			State: a2a.TaskStateCompleted,
		},
		Artifacts: []a2a.Artifact{
			{
				ArtifactID: "artifact-1",
				Parts:      parts,
			},
		},
	}
}

// failedTask builds a Task with failed status and an error message.
func (m *A2AServer) failedTask(taskID, errMsg string) *a2a.Task {
	return &a2a.Task{
		ID:        taskID,
		ContextID: "mock-ctx",
		Status: a2a.TaskStatus{
			State: a2a.TaskStateFailed,
			Message: &a2a.Message{
				Role:  a2a.RoleAgent,
				Parts: []a2a.Part{{Text: &errMsg}},
			},
		},
	}
}

// messageText concatenates all text parts in a message.
func messageText(msg *a2a.Message) string {
	var b strings.Builder
	for _, p := range msg.Parts {
		if p.Text != nil {
			b.WriteString(*p.Text)
		}
	}
	return b.String()
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
