package handlers

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestExpandEnvVars(t *testing.T) {
	t.Setenv("TEST_VAR_1", "hello")
	t.Setenv("TEST_VAR_2", "world")

	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"no vars", "plain text", "plain text"},
		{"single var", "Bearer ${TEST_VAR_1}", "Bearer hello"},
		{"multiple vars", "${TEST_VAR_1} ${TEST_VAR_2}", "hello world"},
		{"missing var", "${NONEXISTENT_VAR_XYZ}", ""},
		{"empty string", "", ""},
		{"adjacent vars", "${TEST_VAR_1}${TEST_VAR_2}", "helloworld"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := expandEnvVars(tt.input)
			if got != tt.expect {
				t.Errorf("expandEnvVars(%q) = %q, want %q", tt.input, got, tt.expect)
			}
		})
	}
}

func TestBuildExternalRequest(t *testing.T) {
	t.Parallel()

	evalCtx := &evals.EvalContext{
		CurrentOutput: "test output",
		Messages: []types.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi there"},
		},
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "search", Arguments: map[string]any{"q": "test"}},
		},
		Variables: map[string]any{"lang": "en"},
	}

	t.Run("default includes messages", func(t *testing.T) {
		t.Parallel()
		params := map[string]any{
			"criteria": "be helpful",
		}
		req := buildExternalRequest(evalCtx, params, "test output")
		if req.CurrentOutput != "test output" {
			t.Errorf("CurrentOutput = %q, want %q", req.CurrentOutput, "test output")
		}
		if req.Criteria != "be helpful" {
			t.Errorf("Criteria = %q, want %q", req.Criteria, "be helpful")
		}
		if len(req.Messages) != 2 {
			t.Errorf("Messages length = %d, want 2", len(req.Messages))
		}
		if len(req.ToolCalls) != 0 {
			t.Errorf("ToolCalls length = %d, want 0 (not included by default)", len(req.ToolCalls))
		}
	})

	t.Run("include_tool_calls", func(t *testing.T) {
		t.Parallel()
		params := map[string]any{
			"include_tool_calls": true,
		}
		req := buildExternalRequest(evalCtx, params, "test output")
		if len(req.ToolCalls) != 1 {
			t.Errorf("ToolCalls length = %d, want 1", len(req.ToolCalls))
		}
	})

	t.Run("exclude messages", func(t *testing.T) {
		t.Parallel()
		params := map[string]any{
			"include_messages": false,
		}
		req := buildExternalRequest(evalCtx, params, "test output")
		if len(req.Messages) != 0 {
			t.Errorf("Messages length = %d, want 0", len(req.Messages))
		}
	})

	t.Run("extra forwarded", func(t *testing.T) {
		t.Parallel()
		params := map[string]any{
			"extra": map[string]any{"scenario_id": "test-1"},
		}
		req := buildExternalRequest(evalCtx, params, "test output")
		if req.Extra == nil {
			t.Fatal("Extra is nil")
		}
		if req.Extra["scenario_id"] != "test-1" {
			t.Errorf("Extra[scenario_id] = %v, want test-1", req.Extra["scenario_id"])
		}
	})

	t.Run("variables forwarded", func(t *testing.T) {
		t.Parallel()
		params := map[string]any{}
		req := buildExternalRequest(evalCtx, params, "test output")
		if req.Variables["lang"] != "en" {
			t.Errorf("Variables[lang] = %v, want en", req.Variables["lang"])
		}
	})
}

func TestParseExternalResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		body     string
		minScore *float64
		passed   bool
		hasScore bool
		score    float64
	}{
		{
			name:     "passed true with score",
			body:     `{"passed": true, "score": 0.9, "reasoning": "good"}`,
			passed:   true,
			hasScore: true,
			score:    0.9,
		},
		{
			name:     "passed false",
			body:     `{"passed": false, "score": 0.3, "reasoning": "bad"}`,
			passed:   false,
			hasScore: true,
			score:    0.3,
		},
		{
			name:     "no passed field score above default threshold",
			body:     `{"score": 0.7, "reasoning": "ok"}`,
			passed:   true,
			hasScore: true,
			score:    0.7,
		},
		{
			name:     "no passed field score below default threshold",
			body:     `{"score": 0.3, "reasoning": "bad"}`,
			passed:   false,
			hasScore: true,
			score:    0.3,
		},
		{
			name:     "min_score override pass",
			body:     `{"score": 0.8, "reasoning": "ok"}`,
			minScore: float64Ptr(0.7),
			passed:   true,
			hasScore: true,
			score:    0.8,
		},
		{
			name:     "min_score override fail",
			body:     `{"score": 0.6, "reasoning": "ok"}`,
			minScore: float64Ptr(0.7),
			passed:   false,
			hasScore: true,
			score:    0.6,
		},
		{
			name:   "invalid JSON",
			body:   "not json at all",
			passed: false,
		},
		{
			name:     "JSON in markdown",
			body:     "```json\n{\"passed\": true, \"score\": 0.95, \"reasoning\": \"great\"}\n```",
			passed:   true,
			hasScore: true,
			score:    0.95,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := parseExternalResponse([]byte(tt.body), tt.minScore)
			if result.Passed != tt.passed {
				t.Errorf("Passed = %v, want %v", result.Passed, tt.passed)
			}
			if tt.hasScore && (result.Score == nil || *result.Score != tt.score) {
				var gotScore float64
				if result.Score != nil {
					gotScore = *result.Score
				}
				t.Errorf("Score = %v, want %v", gotScore, tt.score)
			}
		})
	}
}

func TestParseDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		params   map[string]any
		key      string
		defVal   string
		expected string
	}{
		{"valid duration", map[string]any{"timeout": "10s"}, "timeout", "30s", "10s"},
		{"missing key", map[string]any{}, "timeout", "30s", "30s"},
		{"invalid duration", map[string]any{"timeout": "bad"}, "timeout", "30s", "30s"},
		{"wrong type", map[string]any{"timeout": 123}, "timeout", "30s", "30s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseDuration(tt.params, tt.key, defaultExternalTimeout)
			if tt.expected == "30s" && got != defaultExternalTimeout {
				t.Errorf("got %v, want default %v", got, defaultExternalTimeout)
			}
			if tt.expected == "10s" && got.Seconds() != 10 {
				t.Errorf("got %v, want 10s", got)
			}
		})
	}
}

func float64Ptr(v float64) *float64 { return &v }
