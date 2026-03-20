package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

func TestRegistryExecute_RateLimitEnforced(t *testing.T) {
	registry := tools.NewRegistry(tools.WithRateLimit(2))

	desc := &tools.ToolDescriptor{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: json.RawMessage(`{"type": "object", "properties": {"input": {"type": "string"}}}`),
		OutputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {"output": {"type": "string"}}
		}`),
		Mode:      "mock",
		TimeoutMs: 1000,
		MockResult: json.RawMessage(`{
			"output": "hello"
		}`),
	}

	if err := registry.Register(desc); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	ctx := context.Background()
	args := json.RawMessage(`{"input": "test"}`)

	// First two calls should succeed
	for i := 0; i < 2; i++ {
		result, err := registry.Execute(ctx, "test_tool", args)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i+1, err)
		}
		if result.Error != "" {
			t.Fatalf("call %d: unexpected tool error: %s", i+1, result.Error)
		}
	}

	// Third call should be rate limited (returned as a tool error, not a Go error)
	result, err := registry.Execute(ctx, "test_tool", args)
	if err != nil {
		t.Fatalf("rate-limited call should not return Go error, got: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected rate limit error in result, got empty error")
	}
	if !strings.Contains(result.Error, "rate limit exceeded") {
		t.Errorf("expected rate limit message, got: %s", result.Error)
	}
}

func TestRegistryExecuteAsync_RateLimitEnforced(t *testing.T) {
	registry := tools.NewRegistry(tools.WithRateLimit(1))

	desc := &tools.ToolDescriptor{
		Name:        "async_tool",
		Description: "An async test tool",
		InputSchema: json.RawMessage(`{"type": "object", "properties": {"input": {"type": "string"}}}`),
		OutputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {"output": {"type": "string"}}
		}`),
		Mode:      "mock",
		TimeoutMs: 1000,
		MockResult: json.RawMessage(`{
			"output": "hello"
		}`),
	}

	if err := registry.Register(desc); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	ctx := context.Background()
	args := json.RawMessage(`{"input": "test"}`)

	// First call should succeed
	result, err := registry.ExecuteAsync(ctx, "async_tool", args)
	if err != nil {
		t.Fatalf("first call: unexpected error: %v", err)
	}
	if result.Status == tools.ToolStatusFailed {
		t.Fatalf("first call: unexpected failure: %s", result.Error)
	}

	// Second call should be rate limited
	result, err = registry.ExecuteAsync(ctx, "async_tool", args)
	if err != nil {
		t.Fatalf("rate-limited call should not return Go error, got: %v", err)
	}
	if result.Status != tools.ToolStatusFailed {
		t.Fatalf("expected ToolStatusFailed, got: %s", result.Status)
	}
	if !strings.Contains(result.Error, "rate limit exceeded") {
		t.Errorf("expected rate limit message, got: %s", result.Error)
	}
}

func TestRegistryExecute_NoRateLimitByDefault(t *testing.T) {
	// Default registry should have no rate limit
	registry := tools.NewRegistry()

	desc := &tools.ToolDescriptor{
		Name:        "unlimited_tool",
		Description: "A tool with no rate limit",
		InputSchema: json.RawMessage(`{"type": "object", "properties": {"input": {"type": "string"}}}`),
		OutputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {"output": {"type": "string"}}
		}`),
		Mode:      "mock",
		TimeoutMs: 1000,
		MockResult: json.RawMessage(`{
			"output": "hello"
		}`),
	}

	if err := registry.Register(desc); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	ctx := context.Background()
	args := json.RawMessage(`{"input": "test"}`)

	// Many calls should all succeed without rate limiting
	for i := 0; i < 100; i++ {
		result, err := registry.Execute(ctx, "unlimited_tool", args)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i+1, err)
		}
		if result.Error != "" {
			t.Fatalf("call %d: unexpected tool error: %s", i+1, result.Error)
		}
	}
}

func TestErrRateLimitExceeded_IsWrappable(t *testing.T) {
	// Verify the sentinel error works with errors.Is
	err := tools.ErrRateLimitExceeded
	if !errors.Is(err, tools.ErrRateLimitExceeded) {
		t.Fatal("ErrRateLimitExceeded should match itself via errors.Is")
	}
}
