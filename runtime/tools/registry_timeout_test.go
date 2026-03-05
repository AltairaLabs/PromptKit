package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// slowExecutor simulates a tool that takes a configurable amount of time.
type slowExecutor struct {
	delay time.Duration
}

func (s *slowExecutor) Name() string { return "slow" }

func (s *slowExecutor) Execute(
	ctx context.Context, _ *tools.ToolDescriptor, _ json.RawMessage,
) (json.RawMessage, error) {
	select {
	case <-time.After(s.delay):
		return json.RawMessage(`{"done": true}`), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// slowAsyncExecutor implements AsyncToolExecutor with a configurable delay.
type slowAsyncExecutor struct {
	delay time.Duration
}

func (s *slowAsyncExecutor) Name() string { return "slow-async" }

func (s *slowAsyncExecutor) Execute(
	ctx context.Context, d *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	select {
	case <-time.After(s.delay):
		return json.RawMessage(`{"done": true}`), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *slowAsyncExecutor) ExecuteAsync(
	ctx context.Context, d *tools.ToolDescriptor, args json.RawMessage,
) (*tools.ToolExecutionResult, error) {
	select {
	case <-time.After(s.delay):
		return &tools.ToolExecutionResult{
			Status:  tools.ToolStatusComplete,
			Content: json.RawMessage(`{"async_done": true}`),
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// registerToolWithExecutor is a helper that registers a custom executor and a
// tool that routes to it via the Mode field.
func registerToolWithExecutor(
	t *testing.T, registry *tools.Registry, exec tools.Executor,
	toolName string, timeoutMs int,
) {
	t.Helper()
	registry.RegisterExecutor(exec)
	desc := &tools.ToolDescriptor{
		Name:        toolName,
		Description: "test tool",
		InputSchema: json.RawMessage(`{"type": "object"}`),
		OutputSchema: json.RawMessage(
			`{"type": "object", "properties": {"done": {"type": "boolean"}}}`,
		),
		Mode:      exec.Name(),
		TimeoutMs: timeoutMs,
	}
	if err := registry.Register(desc); err != nil {
		t.Fatalf("Register(%s): %v", toolName, err)
	}
}

func TestExecute_Timeout(t *testing.T) {
	registry := tools.NewRegistry()
	exec := &slowExecutor{delay: 500 * time.Millisecond}
	registerToolWithExecutor(t, registry, exec, "slow_tool", 50) // 50ms timeout

	result, err := registry.Execute(
		context.Background(), "slow_tool", json.RawMessage(`{}`),
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected timeout error in result, got empty")
	}
	if !strings.Contains(result.Error, "timed out") {
		t.Errorf("expected 'timed out' in error, got: %s", result.Error)
	}
	if !strings.Contains(result.Error, "slow_tool") {
		t.Errorf("expected tool name in error, got: %s", result.Error)
	}
	if !strings.Contains(result.Error, "50ms") {
		t.Errorf("expected '50ms' in error, got: %s", result.Error)
	}
}

func TestExecute_NoTimeoutWhenFast(t *testing.T) {
	registry := tools.NewRegistry()
	exec := &slowExecutor{delay: 5 * time.Millisecond}
	registerToolWithExecutor(t, registry, exec, "fast_tool", 500) // 500ms timeout

	result, err := registry.Execute(
		context.Background(), "fast_tool", json.RawMessage(`{}`),
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Error != "" {
		t.Errorf("expected no error, got: %s", result.Error)
	}
}

func TestExecute_ZeroTimeoutNoLimit(t *testing.T) {
	// A tool with TimeoutMs=0 should not have a timeout applied, but
	// validateDescriptor sets a default. To get 0, we use
	// WithDefaultTimeout(0) and register directly.
	registry := tools.NewRegistry(tools.WithDefaultTimeout(0))
	exec := &slowExecutor{delay: 50 * time.Millisecond}
	registerToolWithExecutor(t, registry, exec, "no_limit_tool", 0)

	// Verify descriptor kept TimeoutMs as 0 (since default is 0)
	tool := registry.Get("no_limit_tool")
	if tool == nil {
		t.Fatal("tool not found")
	}
	if tool.TimeoutMs != 0 {
		t.Fatalf("expected TimeoutMs=0, got %d", tool.TimeoutMs)
	}

	result, err := registry.Execute(
		context.Background(), "no_limit_tool", json.RawMessage(`{}`),
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Error != "" {
		t.Errorf("expected no error, got: %s", result.Error)
	}
}

func TestExecuteAsync_Timeout(t *testing.T) {
	registry := tools.NewRegistry()
	exec := &slowAsyncExecutor{delay: 500 * time.Millisecond}
	registerToolWithExecutor(t, registry, exec, "slow_async_tool", 50)

	result, err := registry.ExecuteAsync(
		context.Background(), "slow_async_tool", json.RawMessage(`{}`),
	)
	if err != nil {
		t.Fatalf("ExecuteAsync returned error: %v", err)
	}
	if result.Status != tools.ToolStatusFailed {
		t.Errorf("expected status Failed, got %v", result.Status)
	}
	if !strings.Contains(result.Error, "timed out") {
		t.Errorf("expected 'timed out' in error, got: %s", result.Error)
	}
}

func TestExecuteAsync_SyncFallback_Timeout(t *testing.T) {
	// Use a non-async executor so ExecuteAsync falls back to sync path.
	registry := tools.NewRegistry()
	exec := &slowExecutor{delay: 500 * time.Millisecond}
	registerToolWithExecutor(t, registry, exec, "slow_sync_fb", 50)

	result, err := registry.ExecuteAsync(
		context.Background(), "slow_sync_fb", json.RawMessage(`{}`),
	)
	if err != nil {
		t.Fatalf("ExecuteAsync returned error: %v", err)
	}
	if result.Status != tools.ToolStatusFailed {
		t.Errorf("expected status Failed, got %v", result.Status)
	}
	if !strings.Contains(result.Error, "timed out") {
		t.Errorf("expected 'timed out' in error, got: %s", result.Error)
	}
}

func TestWithDefaultTimeout(t *testing.T) {
	registry := tools.NewRegistry(tools.WithDefaultTimeout(5000))

	desc := &tools.ToolDescriptor{
		Name:         "custom_default",
		Description:  "test",
		InputSchema:  json.RawMessage(`{"type": "object"}`),
		OutputSchema: json.RawMessage(`{"type": "object"}`),
		TimeoutMs:    0, // Should get the registry default
	}
	jsonData, _ := json.Marshal(desc)
	if err := registry.LoadToolFromBytes("tool.json", jsonData); err != nil {
		t.Fatalf("LoadToolFromBytes: %v", err)
	}

	tool := registry.Get("custom_default")
	if tool.TimeoutMs != 5000 {
		t.Errorf("expected TimeoutMs=5000, got %d", tool.TimeoutMs)
	}
}

func TestDefaultToolTimeoutConstant(t *testing.T) {
	if tools.DefaultToolTimeout != 30000 {
		t.Errorf(
			"expected DefaultToolTimeout=30000, got %d",
			tools.DefaultToolTimeout,
		)
	}
}

func TestExecute_TimeoutDoesNotAffectCallerContext(t *testing.T) {
	// Ensure the caller's context is not cancelled by the tool timeout.
	registry := tools.NewRegistry()
	exec := &slowExecutor{delay: 200 * time.Millisecond}
	registerToolWithExecutor(t, registry, exec, "timeout_iso", 50)

	ctx := context.Background()
	_, _ = registry.Execute(ctx, "timeout_iso", json.RawMessage(`{}`))

	// The original context should still be valid.
	if ctx.Err() != nil {
		t.Errorf("caller context should not be cancelled, got: %v", ctx.Err())
	}
}
