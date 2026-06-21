package web

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
)

// jsonKeyError and jsonKeyTaskType are map keys used in JSON responses across
// the interactive handlers. Defined as constants to satisfy goconst (these
// strings also appear in event_adapter.go and server.go, pushing the
// package-wide occurrence count above the linter threshold).
const (
	jsonKeyError    = "error"
	jsonKeyTaskType = "taskType"
)

// interactiveSessions holds live chat sessions keyed by conversation ID.
type interactiveSessions struct {
	mu sync.Mutex
	m  map[string]*engine.InteractiveSession
}

func newInteractiveSessions() *interactiveSessions {
	return &interactiveSessions{m: map[string]*engine.InteractiveSession{}}
}

func (s *interactiveSessions) put(sess *engine.InteractiveSession) {
	s.mu.Lock()
	s.m[sess.ConversationID()] = sess
	s.mu.Unlock()
}

func (s *interactiveSessions) get(id string) (*engine.InteractiveSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[id]
	return v, ok
}

func (s *Server) handleInteractiveOptions(w http.ResponseWriter, _ *http.Request) {
	if s.interactiveEngine == nil {
		http.Error(w, "engine not configured", http.StatusServiceUnavailable)
		return
	}
	agents := s.interactiveEngine.Agents()
	out := struct {
		Agents    []map[string]string `json:"agents"`
		Providers []string            `json:"providers"`
		HasEvals  bool                `json:"hasEvals"`
	}{
		Providers: s.interactiveEngine.ProviderIDs(),
		HasEvals:  s.interactiveEngine.HasConfigEvals(),
	}
	for _, a := range agents {
		out.Agents = append(out.Agents, map[string]string{jsonKeyTaskType: a.TaskType, "description": a.Description})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleInteractiveSession(w http.ResponseWriter, r *http.Request) {
	if s.interactiveEngine == nil {
		http.Error(w, "engine not configured", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Agent     string            `json:"agent"`
		Provider  string            `json:"provider"`
		Variables map[string]string `json:"variables"`
		Evals     bool              `json:"evals"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	missing, err := s.interactiveEngine.MissingRequiredVars(body.Agent, body.Variables)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{jsonKeyError: err.Error()})
		return
	}
	if len(missing) > 0 {
		writeJSON(w, http.StatusOK, map[string]any{"missingVars": missing})
		return
	}
	sess, err := s.interactiveEngine.NewInteractiveSession(engine.InteractiveSessionOptions{
		ProviderID: body.Provider,
		TaskType:   body.Agent,
		Variables:  body.Variables,
		RunEvals:   body.Evals,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{jsonKeyError: err.Error()})
		return
	}
	s.interactive.put(sess)
	writeJSON(w, http.StatusOK, map[string]any{"sessionId": sess.ConversationID()})
}

func (s *Server) handleInteractiveMessage(w http.ResponseWriter, r *http.Request) {
	if s.interactiveEngine == nil {
		http.Error(w, "engine not configured", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		SessionID string `json:"sessionId"`
		Text      string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	sess, ok := s.interactive.get(body.SessionID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{jsonKeyError: "unknown session"})
		return
	}
	ch, err := sess.SendUserMessage(r.Context(), body.Text)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{jsonKeyError: err.Error()})
		return
	}
	for chunk := range ch { // messages render live via SSE; drain + surface first error
		if chunk.Error != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{jsonKeyError: chunk.Error.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
