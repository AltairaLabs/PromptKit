package a2a

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

func TestExecutor_Name(t *testing.T) {
	e := NewExecutor()
	if got := e.Name(); got != "a2a" {
		t.Errorf("Name() = %q, want %q", got, "a2a")
	}
}

func TestExecutor_Execute_NoA2AConfig(t *testing.T) {
	e := NewExecutor()
	desc := &tools.ToolDescriptor{Name: "test-tool"}
	_, err := e.Execute(desc, json.RawMessage(`{"query":"hello"}`))
	if err == nil {
		t.Fatal("expected error for missing A2AConfig")
	}
}

func TestExecutor_Execute_BasicTextQuery(t *testing.T) {
	text := "Found 3 papers"
	task := &Task{
		ID: "task-1",
		Status: TaskStatus{
			State: TaskStateCompleted,
			Message: &Message{
				Role:  RoleAgent,
				Parts: []Part{{Text: &text}},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPC(r)
		if req.Method != MethodSendMessage {
			t.Errorf("method = %q, want %q", req.Method, MethodSendMessage)
		}
		rpcResult(w, req.ID, task)
	}))
	defer srv.Close()

	e := NewExecutor()
	desc := &tools.ToolDescriptor{
		Name: "a2a__research_agent__search",
		A2AConfig: &tools.A2AConfig{
			AgentURL: srv.URL,
			SkillID:  "search",
		},
	}

	result, err := e.Execute(desc, json.RawMessage(`{"query":"search for papers"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out map[string]string
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if out["response"] != "Found 3 papers" {
		t.Errorf("response = %q, want %q", out["response"], "Found 3 papers")
	}
}

func TestExecutor_Execute_ArtifactFallback(t *testing.T) {
	text1 := "Part 1"
	text2 := "Part 2"
	task := &Task{
		ID: "task-2",
		Status: TaskStatus{
			State: TaskStateCompleted,
		},
		Artifacts: []Artifact{
			{Parts: []Part{{Text: &text1}}},
			{Parts: []Part{{Text: &text2}}},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPC(r)
		rpcResult(w, req.ID, task)
	}))
	defer srv.Close()

	e := NewExecutor()
	desc := &tools.ToolDescriptor{
		Name:      "test-tool",
		A2AConfig: &tools.A2AConfig{AgentURL: srv.URL},
	}

	result, err := e.Execute(desc, json.RawMessage(`{"query":"test"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out map[string]string
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if out["response"] != "Part 1\nPart 2" {
		t.Errorf("response = %q, want %q", out["response"], "Part 1\nPart 2")
	}
}

func TestExecutor_Execute_EmptyResponse(t *testing.T) {
	task := &Task{
		ID: "task-3",
		Status: TaskStatus{
			State: TaskStateCompleted,
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPC(r)
		rpcResult(w, req.ID, task)
	}))
	defer srv.Close()

	e := NewExecutor()
	desc := &tools.ToolDescriptor{
		Name:      "test-tool",
		A2AConfig: &tools.A2AConfig{AgentURL: srv.URL},
	}

	result, err := e.Execute(desc, json.RawMessage(`{"query":"test"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out map[string]string
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if out["response"] != "" {
		t.Errorf("response = %q, want empty", out["response"])
	}
}

func TestExecutor_Execute_InvalidArgs(t *testing.T) {
	e := NewExecutor()
	desc := &tools.ToolDescriptor{
		Name:      "test-tool",
		A2AConfig: &tools.A2AConfig{AgentURL: "http://localhost:1"},
	}

	_, err := e.Execute(desc, json.RawMessage(`not-json`))
	if err == nil {
		t.Fatal("expected error for invalid args")
	}
}

func TestExecutor_Execute_WithSkillIDMetadata(t *testing.T) {
	var receivedMetadata map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPC(r)
		var params SendMessageRequest
		raw, _ := json.Marshal(req.Params)
		_ = json.Unmarshal(raw, &params)
		receivedMetadata = params.Message.Metadata

		text := "ok"
		task := &Task{
			ID: "task-4",
			Status: TaskStatus{
				State:   TaskStateCompleted,
				Message: &Message{Role: RoleAgent, Parts: []Part{{Text: &text}}},
			},
		}
		rpcResult(w, req.ID, task)
	}))
	defer srv.Close()

	e := NewExecutor()
	desc := &tools.ToolDescriptor{
		Name: "test-tool",
		A2AConfig: &tools.A2AConfig{
			AgentURL: srv.URL,
			SkillID:  "my_skill",
		},
	}

	_, err := e.Execute(desc, json.RawMessage(`{"query":"hello"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if receivedMetadata == nil {
		t.Fatal("expected metadata to be set")
	}
	if receivedMetadata["skillId"] != "my_skill" {
		t.Errorf("skillId = %v, want %q", receivedMetadata["skillId"], "my_skill")
	}
}

func TestExecutor_Execute_NoSkillIDMetadata(t *testing.T) {
	var receivedMetadata map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPC(r)
		var params SendMessageRequest
		raw, _ := json.Marshal(req.Params)
		_ = json.Unmarshal(raw, &params)
		receivedMetadata = params.Message.Metadata

		text := "ok"
		task := &Task{
			ID: "task-5",
			Status: TaskStatus{
				State:   TaskStateCompleted,
				Message: &Message{Role: RoleAgent, Parts: []Part{{Text: &text}}},
			},
		}
		rpcResult(w, req.ID, task)
	}))
	defer srv.Close()

	e := NewExecutor()
	desc := &tools.ToolDescriptor{
		Name:      "test-tool",
		A2AConfig: &tools.A2AConfig{AgentURL: srv.URL},
	}

	_, err := e.Execute(desc, json.RawMessage(`{"query":"hello"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if receivedMetadata != nil {
		t.Errorf("expected nil metadata when no skillId, got %v", receivedMetadata)
	}
}

func TestExecutor_Execute_WithMediaParts(t *testing.T) {
	var receivedParts []Part

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPC(r)
		var params SendMessageRequest
		raw, _ := json.Marshal(req.Params)
		_ = json.Unmarshal(raw, &params)
		receivedParts = params.Message.Parts

		text := "ok"
		task := &Task{
			ID: "task-6",
			Status: TaskStatus{
				State:   TaskStateCompleted,
				Message: &Message{Role: RoleAgent, Parts: []Part{{Text: &text}}},
			},
		}
		rpcResult(w, req.ID, task)
	}))
	defer srv.Close()

	e := NewExecutor()
	desc := &tools.ToolDescriptor{
		Name:      "test-tool",
		A2AConfig: &tools.A2AConfig{AgentURL: srv.URL},
	}

	args := `{"query":"analyze","image_url":"http://example.com/img.png","image_data":"base64data","audio_data":"audiodata"}`
	_, err := e.Execute(desc, json.RawMessage(args))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	// Should have 4 parts: text query + image_url + image_data + audio_data
	if len(receivedParts) != 4 {
		t.Fatalf("got %d parts, want 4", len(receivedParts))
	}
	if *receivedParts[0].Text != "analyze" {
		t.Errorf("part 0 text = %q, want %q", *receivedParts[0].Text, "analyze")
	}
	if receivedParts[1].URL == nil || *receivedParts[1].URL != "http://example.com/img.png" {
		t.Errorf("part 1 URL = %v, want image URL", receivedParts[1].URL)
	}
	if receivedParts[2].MediaType != "image/*" {
		t.Errorf("part 2 media type = %q, want %q", receivedParts[2].MediaType, "image/*")
	}
	if receivedParts[3].MediaType != "audio/*" {
		t.Errorf("part 3 media type = %q, want %q", receivedParts[3].MediaType, "audio/*")
	}
}

func TestExecutor_ClientCaching(t *testing.T) {
	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		req := decodeRPC(r)
		text := "ok"
		task := &Task{
			ID: "task-7",
			Status: TaskStatus{
				State:   TaskStateCompleted,
				Message: &Message{Role: RoleAgent, Parts: []Part{{Text: &text}}},
			},
		}
		rpcResult(w, req.ID, task)
	}))
	defer srv.Close()

	e := NewExecutor()
	desc := &tools.ToolDescriptor{
		Name:      "test-tool",
		A2AConfig: &tools.A2AConfig{AgentURL: srv.URL},
	}

	// Execute twice with same URL - should reuse client
	_, _ = e.Execute(desc, json.RawMessage(`{"query":"first"}`))
	_, _ = e.Execute(desc, json.RawMessage(`{"query":"second"}`))

	if callCount != 2 {
		t.Errorf("server received %d calls, want 2", callCount)
	}

	// Verify client was cached (only 1 client for the URL)
	e.mu.RLock()
	clientCount := len(e.clients)
	e.mu.RUnlock()
	if clientCount != 1 {
		t.Errorf("cached %d clients, want 1", clientCount)
	}
}

func TestExecutor_Execute_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPC(r)
		rpcErrorResp(w, req.ID, -32600, "bad request")
	}))
	defer srv.Close()

	e := NewExecutor()
	desc := &tools.ToolDescriptor{
		Name:      "test-tool",
		A2AConfig: &tools.A2AConfig{AgentURL: srv.URL},
	}

	_, err := e.Execute(desc, json.RawMessage(`{"query":"test"}`))
	if err == nil {
		t.Fatal("expected error from server error response")
	}
}

func TestExtractResponseText_StatusMessage(t *testing.T) {
	text := "status text"
	task := &Task{
		Status: TaskStatus{
			Message: &Message{
				Parts: []Part{{Text: &text}},
			},
		},
	}
	if got := ExtractResponseText(task); got != "status text" {
		t.Errorf("got %q, want %q", got, "status text")
	}
}

func TestExtractResponseText_Artifacts(t *testing.T) {
	t1 := "first"
	t2 := "second"
	task := &Task{
		Status: TaskStatus{State: TaskStateCompleted},
		Artifacts: []Artifact{
			{Parts: []Part{{Text: &t1}}},
			{Parts: []Part{{Text: &t2}}},
		},
	}
	if got := ExtractResponseText(task); got != "first\nsecond" {
		t.Errorf("got %q, want %q", got, "first\nsecond")
	}
}

func TestExtractResponseText_Empty(t *testing.T) {
	task := &Task{
		Status: TaskStatus{State: TaskStateCompleted},
	}
	if got := ExtractResponseText(task); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestExtractResponseText_StatusPrecedence(t *testing.T) {
	statusText := "from status"
	artifactText := "from artifact"
	task := &Task{
		Status: TaskStatus{
			Message: &Message{
				Parts: []Part{{Text: &statusText}},
			},
		},
		Artifacts: []Artifact{
			{Parts: []Part{{Text: &artifactText}}},
		},
	}
	// Status message text takes precedence over artifacts
	if got := ExtractResponseText(task); got != "from status" {
		t.Errorf("got %q, want %q", got, "from status")
	}
}
