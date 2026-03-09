package a2a

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

func TestResolveHeaders(t *testing.T) {
	t.Run("nil inputs returns nil", func(t *testing.T) {
		got := resolveHeaders(nil, nil)
		if got != nil {
			t.Fatalf("expected nil, got %v", got)
		}
	})

	t.Run("static headers only", func(t *testing.T) {
		got := resolveHeaders(map[string]string{"X-Key": "val"}, nil)
		if got["X-Key"] != "val" {
			t.Fatalf("expected X-Key=val, got %v", got)
		}
	})

	t.Run("env headers resolved", func(t *testing.T) {
		t.Setenv("TEST_RESOLVE_HDR", "env-val")
		got := resolveHeaders(nil, []string{"X-Env=TEST_RESOLVE_HDR"})
		if got["X-Env"] != "env-val" {
			t.Fatalf("expected X-Env=env-val, got %v", got)
		}
	})

	t.Run("unset env skipped", func(t *testing.T) {
		got := resolveHeaders(nil, []string{"X-Missing=UNSET_XXXXXX"})
		if got != nil {
			t.Fatalf("expected nil for unset env, got %v", got)
		}
	})

	t.Run("malformed spec skipped", func(t *testing.T) {
		got := resolveHeaders(nil, []string{"no-equals"})
		if got != nil {
			t.Fatalf("expected nil for malformed spec, got %v", got)
		}
	})

	t.Run("mixed static and env", func(t *testing.T) {
		t.Setenv("TEST_MIX_HDR", "from-env")
		got := resolveHeaders(
			map[string]string{"X-Static": "s"},
			[]string{"X-Dynamic=TEST_MIX_HDR"},
		)
		if got["X-Static"] != "s" || got["X-Dynamic"] != "from-env" {
			t.Fatalf("unexpected headers: %v", got)
		}
	})
}

func TestExecutor_GetOrCreateClientWithConfig_Auth(t *testing.T) {
	t.Setenv("TEST_EXEC_TOKEN", "env-token")
	e := NewExecutor()
	defer e.Close()

	cfg := &tools.A2AConfig{
		AgentURL: "https://agent.example.com/a2a",
		Auth: &tools.A2AAuthConfig{
			Scheme:   "Bearer",
			TokenEnv: "TEST_EXEC_TOKEN",
		},
		Headers:        map[string]string{"X-Tenant": "acme"},
		HeadersFromEnv: []string{"X-Key=TEST_EXEC_TOKEN"},
	}

	c := e.getOrCreateClientWithConfig(cfg)
	if c == nil {
		t.Fatal("expected non-nil client")
	}

	// Second call should return cached client.
	c2 := e.getOrCreateClientWithConfig(cfg)
	if c != c2 {
		t.Fatal("expected same cached client")
	}
}

func TestExecutor_GetOrCreateClientWithConfig_NoAuth(t *testing.T) {
	e := NewExecutor()
	defer e.Close()

	cfg := &tools.A2AConfig{AgentURL: "https://agent.example.com/a2a"}
	c := e.getOrCreateClientWithConfig(cfg)
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestExecutor_GetOrCreateClientWithConfig_DirectToken(t *testing.T) {
	e := NewExecutor()
	defer e.Close()

	cfg := &tools.A2AConfig{
		AgentURL: "https://agent.example.com/a2a",
		Auth: &tools.A2AAuthConfig{
			Scheme: "Bearer",
			Token:  "direct-token",
		},
	}
	c := e.getOrCreateClientWithConfig(cfg)
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}
