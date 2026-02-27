package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestRestEvalHandler_Type(t *testing.T) {
	t.Parallel()
	h := &RestEvalHandler{}
	if h.Type() != "rest_eval" {
		t.Errorf("got %q, want %q", h.Type(), "rest_eval")
	}
}

func TestRestEvalSessionHandler_Type(t *testing.T) {
	t.Parallel()
	h := &RestEvalSessionHandler{}
	if h.Type() != "rest_eval_session" {
		t.Errorf("got %q, want %q", h.Type(), "rest_eval_session")
	}
}

func TestRestEvalHandler_MissingURL(t *testing.T) {
	t.Parallel()
	h := &RestEvalHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected Passed=false for missing URL")
	}
	if result.Explanation != "rest_eval requires a 'url' param" {
		t.Errorf("unexpected explanation: %s", result.Explanation)
	}
}

func TestRestEvalHandler_SuccessfulEval(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json content type")
		}

		body, _ := io.ReadAll(r.Body)
		var req ExternalEvalRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("failed to unmarshal request: %v", err)
		}
		if req.CurrentOutput != "great answer" {
			t.Errorf("expected current_output='great answer', got %q", req.CurrentOutput)
		}
		if req.Criteria != "Is it helpful?" {
			t.Errorf("expected criteria='Is it helpful?', got %q", req.Criteria)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"passed":    true,
			"score":     0.95,
			"reasoning": "Very helpful response",
		})
	}))
	defer server.Close()

	h := &RestEvalHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: "great answer",
		Messages: []types.Message{
			{Role: "user", Content: "help me"},
			{Role: "assistant", Content: "great answer"},
		},
	}
	params := map[string]any{
		"url":      server.URL,
		"criteria": "Is it helpful?",
	}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("expected Passed=true")
	}
	if result.Score == nil || *result.Score != 0.95 {
		t.Errorf("expected score 0.95, got %v", result.Score)
	}
	if result.Explanation != "Very helpful response" {
		t.Errorf("unexpected explanation: %s", result.Explanation)
	}
	if result.Type != "rest_eval" {
		t.Errorf("expected type rest_eval, got %s", result.Type)
	}
}

func TestRestEvalHandler_FailedEval(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"passed":    false,
			"score":     0.2,
			"reasoning": "Not helpful at all",
		})
	}))
	defer server.Close()

	h := &RestEvalHandler{}
	evalCtx := &evals.EvalContext{CurrentOutput: "bad answer"}
	params := map[string]any{"url": server.URL}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected Passed=false")
	}
	if result.Score == nil || *result.Score != 0.2 {
		t.Errorf("expected score 0.2, got %v", result.Score)
	}
}

func TestRestEvalHandler_NonOKStatus(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	h := &RestEvalHandler{}
	evalCtx := &evals.EvalContext{CurrentOutput: "test"}
	params := map[string]any{"url": server.URL}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected Passed=false for 500 status")
	}
	if result.Type != "rest_eval" {
		t.Errorf("expected type rest_eval, got %s", result.Type)
	}
}

func TestRestEvalHandler_InvalidJSON(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	h := &RestEvalHandler{}
	evalCtx := &evals.EvalContext{CurrentOutput: "test"}
	params := map[string]any{"url": server.URL}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected Passed=false for invalid JSON")
	}
}

func TestRestEvalHandler_Timeout(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		w.Write([]byte(`{"passed": true, "score": 1.0}`))
	}))
	defer server.Close()

	h := &RestEvalHandler{}
	evalCtx := &evals.EvalContext{CurrentOutput: "test"}
	params := map[string]any{
		"url":     server.URL,
		"timeout": "100ms",
	}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected Passed=false for timeout")
	}
}

func TestRestEvalHandler_EnvVarHeaders(t *testing.T) {
	t.Setenv("REST_EVAL_TEST_TOKEN", "secret-token-123")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer secret-token-123" {
			t.Errorf("expected auth header 'Bearer secret-token-123', got %q", auth)
		}
		custom := r.Header.Get("X-Custom")
		if custom != "value" {
			t.Errorf("expected X-Custom header 'value', got %q", custom)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"passed": true, "score": 1.0})
	}))
	defer server.Close()

	h := &RestEvalHandler{}
	evalCtx := &evals.EvalContext{CurrentOutput: "test"}
	params := map[string]any{
		"url": server.URL,
		"headers": map[string]any{
			"Authorization": "Bearer ${REST_EVAL_TEST_TOKEN}",
			"X-Custom":      "value",
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected Passed=true, got explanation: %s", result.Explanation)
	}
}

func newScoreServer(score float64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"score":     score,
			"reasoning": "decent",
		})
	}))
}

func TestRestEvalHandler_MinScoreOverride(t *testing.T) {
	t.Parallel()

	t.Run("passes with lower threshold", func(t *testing.T) {
		t.Parallel()
		server := newScoreServer(0.7)
		defer server.Close()

		h := &RestEvalHandler{}
		evalCtx := &evals.EvalContext{CurrentOutput: "test"}
		params := map[string]any{
			"url":       server.URL,
			"min_score": 0.6,
		}
		result, err := h.Eval(context.Background(), evalCtx, params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Passed {
			t.Errorf("expected Passed=true with min_score=0.6, score=%v, explanation=%s",
				result.Score, result.Explanation)
		}
	})

	t.Run("fails with higher threshold", func(t *testing.T) {
		t.Parallel()
		server := newScoreServer(0.7)
		defer server.Close()

		h := &RestEvalHandler{}
		evalCtx := &evals.EvalContext{CurrentOutput: "test"}
		params := map[string]any{
			"url":       server.URL,
			"min_score": 0.8,
		}
		result, err := h.Eval(context.Background(), evalCtx, params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Passed {
			t.Error("expected Passed=false with min_score=0.8")
		}
	})
}

func TestRestEvalSessionHandler_AggregatesContent(t *testing.T) {
	t.Parallel()
	var receivedOutput string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req ExternalEvalRequest
		json.Unmarshal(body, &req)
		receivedOutput = req.CurrentOutput

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"passed": true, "score": 0.9})
	}))
	defer server.Close()

	h := &RestEvalSessionHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			{Role: "user", Content: "question 1"},
			{Role: "assistant", Content: "answer 1"},
			{Role: "user", Content: "question 2"},
			{Role: "assistant", Content: "answer 2"},
		},
	}
	params := map[string]any{"url": server.URL}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("expected Passed=true")
	}
	if result.Type != "rest_eval_session" {
		t.Errorf("expected type rest_eval_session, got %s", result.Type)
	}
	if receivedOutput != "answer 1\nanswer 2" {
		t.Errorf("expected aggregated assistant content, got %q", receivedOutput)
	}
}

func TestRestEvalHandler_CustomMethod(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"passed": true, "score": 1.0})
	}))
	defer server.Close()

	h := &RestEvalHandler{}
	evalCtx := &evals.EvalContext{CurrentOutput: "test"}
	params := map[string]any{
		"url":    server.URL,
		"method": "PUT",
	}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("expected Passed=true")
	}
}

func TestRestEvalHandler_IncludeToolCalls(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req ExternalEvalRequest
		json.Unmarshal(body, &req)

		if len(req.ToolCalls) != 1 {
			t.Errorf("expected 1 tool call, got %d", len(req.ToolCalls))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"passed": true, "score": 1.0})
	}))
	defer server.Close()

	h := &RestEvalHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: "test",
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "search", Arguments: map[string]any{"q": "test"}},
		},
	}
	params := map[string]any{
		"url":                server.URL,
		"include_tool_calls": true,
	}

	_, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
