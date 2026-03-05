package stage

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// delayedExecutor is a tool executor that introduces a configurable delay
// and tracks concurrent execution count for verifying parallelism.
type delayedExecutor struct {
	name        string
	delay       time.Duration
	content     []byte
	status      tools.ToolExecutionStatus
	errorMsg    string
	pendingMsg  string
	concurrent  atomic.Int32
	maxObserved atomic.Int32
	callCount   atomic.Int32
}

func (d *delayedExecutor) Name() string { return d.name }

func (d *delayedExecutor) Execute(
	ctx context.Context, _ *tools.ToolDescriptor, _ json.RawMessage,
) (json.RawMessage, error) {
	return d.content, nil
}

func (d *delayedExecutor) ExecuteAsync(
	ctx context.Context, descriptor *tools.ToolDescriptor, _ json.RawMessage,
) (*tools.ToolExecutionResult, error) {
	d.callCount.Add(1)
	cur := d.concurrent.Add(1)
	defer d.concurrent.Add(-1)

	// Track maximum observed concurrency.
	for {
		prev := d.maxObserved.Load()
		if cur <= prev || d.maxObserved.CompareAndSwap(prev, cur) {
			break
		}
	}

	select {
	case <-time.After(d.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	result := &tools.ToolExecutionResult{
		Status:  d.status,
		Content: d.content,
		Error:   d.errorMsg,
	}
	if d.status == tools.ToolStatusPending {
		result.PendingInfo = &tools.PendingToolInfo{
			Reason:   "requires_approval",
			Message:  d.pendingMsg,
			ToolName: descriptor.Name,
		}
	}
	return result, nil
}

// setupDelayedStage creates a ProviderStage with a delayed executor and N tools.
func setupDelayedStage(
	t *testing.T,
	executor *delayedExecutor,
	toolNames []string,
	policy *pipeline.ToolPolicy,
) *ProviderStage {
	t.Helper()
	registry := tools.NewRegistry()
	registry.RegisterExecutor(executor)
	for _, name := range toolNames {
		err := registry.Register(&tools.ToolDescriptor{
			Name:        name,
			Description: "Tool " + name,
			Mode:        executor.Name(),
			InputSchema: []byte(`{"type": "object"}`),
		})
		require.NoError(t, err)
	}
	provider := mock.NewProvider("test", "model", false)
	return NewProviderStage(provider, registry, policy, nil)
}

func TestParallelToolCalls_ExecutesConcurrently(t *testing.T) {
	executor := &delayedExecutor{
		name:    "delayed-exec",
		delay:   50 * time.Millisecond,
		status:  tools.ToolStatusComplete,
		content: []byte(`{"ok":true}`),
	}
	toolNames := []string{"tool_a", "tool_b", "tool_c", "tool_d"}
	stage := setupDelayedStage(t, executor, toolNames, nil)

	calls := []types.MessageToolCall{
		{ID: "c1", Name: "tool_a", Args: json.RawMessage(`{}`)},
		{ID: "c2", Name: "tool_b", Args: json.RawMessage(`{}`)},
		{ID: "c3", Name: "tool_c", Args: json.RawMessage(`{}`)},
		{ID: "c4", Name: "tool_d", Args: json.RawMessage(`{}`)},
	}

	start := time.Now()
	results, err := stage.executeToolCalls(context.Background(), calls)
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.Len(t, results, 4)

	// If sequential, 4 * 50ms = 200ms+. Parallel should be ~50ms.
	assert.Less(t, elapsed, 150*time.Millisecond,
		"parallel execution should be much faster than sequential")

	// Verify at least 2 calls ran concurrently.
	assert.GreaterOrEqual(t, int(executor.maxObserved.Load()), 2,
		"expected concurrent execution")
}

func TestParallelToolCalls_PreservesResultOrder(t *testing.T) {
	executor := &delayedExecutor{
		name:    "order-exec",
		delay:   10 * time.Millisecond,
		status:  tools.ToolStatusComplete,
		content: []byte(`{"ok":true}`),
	}
	toolNames := []string{"t1", "t2", "t3", "t4", "t5"}
	stage := setupDelayedStage(t, executor, toolNames, nil)

	calls := make([]types.MessageToolCall, len(toolNames))
	for i, name := range toolNames {
		calls[i] = types.MessageToolCall{
			ID:   "call-" + name,
			Name: name,
			Args: json.RawMessage(`{}`),
		}
	}

	results, err := stage.executeToolCalls(context.Background(), calls)
	require.NoError(t, err)
	require.Len(t, results, len(toolNames))

	for i, name := range toolNames {
		assert.Equal(t, "call-"+name, results[i].ToolResult.ID,
			"result at index %d should match tool call order", i)
	}
}

func TestParallelToolCalls_MaxConcurrencyLimit(t *testing.T) {
	executor := &delayedExecutor{
		name:    "limit-exec",
		delay:   50 * time.Millisecond,
		status:  tools.ToolStatusComplete,
		content: []byte(`{"ok":true}`),
	}
	toolNames := make([]string, 8)
	for i := range toolNames {
		toolNames[i] = "tool_" + string(rune('a'+i))
	}
	policy := &pipeline.ToolPolicy{MaxParallelToolCalls: 2}
	stage := setupDelayedStage(t, executor, toolNames, policy)

	calls := make([]types.MessageToolCall, len(toolNames))
	for i, name := range toolNames {
		calls[i] = types.MessageToolCall{
			ID:   "c" + name,
			Name: name,
			Args: json.RawMessage(`{}`),
		}
	}

	results, err := stage.executeToolCalls(context.Background(), calls)
	require.NoError(t, err)
	require.Len(t, results, len(toolNames))

	// With limit=2, we should never see more than 2 concurrent.
	assert.LessOrEqual(t, int(executor.maxObserved.Load()), 2,
		"concurrency should respect MaxParallelToolCalls limit")
}

func TestParallelToolCalls_DefaultConcurrencyLimit(t *testing.T) {
	stage := &ProviderStage{}
	assert.Equal(t, defaultMaxParallelToolCalls, stage.getMaxParallelToolCalls(),
		"default should be used when no policy is set")

	stage.toolPolicy = &pipeline.ToolPolicy{}
	assert.Equal(t, defaultMaxParallelToolCalls, stage.getMaxParallelToolCalls(),
		"default should be used when MaxParallelToolCalls is 0")

	stage.toolPolicy = &pipeline.ToolPolicy{MaxParallelToolCalls: 5}
	assert.Equal(t, 5, stage.getMaxParallelToolCalls(),
		"configured value should be returned")
}

func TestParallelToolCalls_OneFailureDoesNotCancelOthers(t *testing.T) {
	// Use two separate executors: one that succeeds and one that fails.
	successExec := &delayedExecutor{
		name:    "success-exec",
		delay:   10 * time.Millisecond,
		status:  tools.ToolStatusComplete,
		content: []byte(`{"ok":true}`),
	}
	failExec := &delayedExecutor{
		name:     "fail-exec",
		delay:    5 * time.Millisecond,
		status:   tools.ToolStatusFailed,
		errorMsg: "network timeout",
	}

	registry := tools.NewRegistry()
	registry.RegisterExecutor(successExec)
	registry.RegisterExecutor(failExec)
	for _, name := range []string{"good_tool_1", "good_tool_2"} {
		err := registry.Register(&tools.ToolDescriptor{
			Name:        name,
			Description: name,
			Mode:        "success-exec",
			InputSchema: []byte(`{"type": "object"}`),
		})
		require.NoError(t, err)
	}
	err := registry.Register(&tools.ToolDescriptor{
		Name:        "bad_tool",
		Description: "A failing tool",
		Mode:        "fail-exec",
		InputSchema: []byte(`{"type": "object"}`),
	})
	require.NoError(t, err)

	provider := mock.NewProvider("test", "model", false)
	stage := NewProviderStage(provider, registry, nil, nil)

	calls := []types.MessageToolCall{
		{ID: "c1", Name: "good_tool_1", Args: json.RawMessage(`{}`)},
		{ID: "c2", Name: "bad_tool", Args: json.RawMessage(`{}`)},
		{ID: "c3", Name: "good_tool_2", Args: json.RawMessage(`{}`)},
	}

	results, execErr := stage.executeToolCalls(context.Background(), calls)
	require.NoError(t, execErr, "individual tool failures should not produce a top-level error")
	require.Len(t, results, 3)

	// First tool succeeded.
	assert.Empty(t, results[0].ToolResult.Error)
	assert.Contains(t, results[0].ToolResult.GetTextContent(), "ok")

	// Second tool failed but is still in results.
	assert.NotEmpty(t, results[1].ToolResult.Error)
	assert.Contains(t, results[1].ToolResult.GetTextContent(), "failed")

	// Third tool succeeded despite failure in the second.
	assert.Empty(t, results[2].ToolResult.Error)
	assert.Contains(t, results[2].ToolResult.GetTextContent(), "ok")
}

func TestParallelToolCalls_MixedBlockedAndExecuted(t *testing.T) {
	executor := &delayedExecutor{
		name:    "mixed-exec",
		delay:   5 * time.Millisecond,
		status:  tools.ToolStatusComplete,
		content: []byte(`{"ok":true}`),
	}
	toolNames := []string{"allowed_tool", "blocked_tool"}
	policy := &pipeline.ToolPolicy{Blocklist: []string{"blocked_tool"}}
	stage := setupDelayedStage(t, executor, toolNames, policy)

	calls := []types.MessageToolCall{
		{ID: "c1", Name: "allowed_tool", Args: json.RawMessage(`{}`)},
		{ID: "c2", Name: "blocked_tool", Args: json.RawMessage(`{}`)},
	}

	results, err := stage.executeToolCalls(context.Background(), calls)
	require.NoError(t, err)
	require.Len(t, results, 2)

	// First result: allowed, should succeed.
	assert.Empty(t, results[0].ToolResult.Error)
	assert.Equal(t, "c1", results[0].ToolResult.ID)

	// Second result: blocked by policy.
	assert.Contains(t, results[1].ToolResult.Error, "blocked by policy")
	assert.Equal(t, "c2", results[1].ToolResult.ID)
}

func TestParallelToolCalls_PendingMixedWithComplete(t *testing.T) {
	completeExec := &delayedExecutor{
		name:    "complete-exec",
		delay:   5 * time.Millisecond,
		status:  tools.ToolStatusComplete,
		content: []byte(`{"ok":true}`),
	}
	pendingExec := &delayedExecutor{
		name:       "pending-exec",
		delay:      5 * time.Millisecond,
		status:     tools.ToolStatusPending,
		pendingMsg: "Needs approval",
	}

	registry := tools.NewRegistry()
	registry.RegisterExecutor(completeExec)
	registry.RegisterExecutor(pendingExec)
	err := registry.Register(&tools.ToolDescriptor{
		Name:        "normal_tool",
		Description: "A normal tool",
		Mode:        "complete-exec",
		InputSchema: []byte(`{"type": "object"}`),
	})
	require.NoError(t, err)
	err = registry.Register(&tools.ToolDescriptor{
		Name:        "pending_tool",
		Description: "A pending tool",
		Mode:        "pending-exec",
		InputSchema: []byte(`{"type": "object"}`),
	})
	require.NoError(t, err)

	provider := mock.NewProvider("test", "model", false)
	stage := NewProviderStage(provider, registry, nil, nil)

	calls := []types.MessageToolCall{
		{ID: "c1", Name: "normal_tool", Args: json.RawMessage(`{}`)},
		{ID: "c2", Name: "pending_tool", Args: json.RawMessage(`{}`)},
		{ID: "c3", Name: "normal_tool", Args: json.RawMessage(`{}`)},
	}

	results, execErr := stage.executeToolCalls(context.Background(), calls)

	// Should get ErrToolsPending.
	require.Error(t, execErr)
	ep, ok := tools.IsErrToolsPending(execErr)
	require.True(t, ok)
	require.Len(t, ep.Pending, 1)
	assert.Equal(t, "c2", ep.Pending[0].CallID)

	// Completed results should only include non-pending entries, in order.
	require.Len(t, results, 2)
	assert.Equal(t, "c1", results[0].ToolResult.ID)
	assert.Equal(t, "c3", results[1].ToolResult.ID)
}

func TestParallelToolCalls_ContextCancellation(t *testing.T) {
	executor := &delayedExecutor{
		name:    "cancel-exec",
		delay:   5 * time.Second, // very long — will be cancelled
		status:  tools.ToolStatusComplete,
		content: []byte(`{"ok":true}`),
	}
	toolNames := []string{"slow_tool"}
	stage := setupDelayedStage(t, executor, toolNames, nil)

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)

	var results []types.Message
	var execErr error

	go func() {
		defer wg.Done()
		results, execErr = stage.executeToolCalls(ctx, []types.MessageToolCall{
			{ID: "c1", Name: "slow_tool", Args: json.RawMessage(`{}`)},
		})
	}()

	// Cancel after a short delay.
	time.Sleep(20 * time.Millisecond)
	cancel()
	wg.Wait()

	// The tool should have returned an error result (not a top-level error)
	// because executeSingleToolCall catches the ctx error from ExecuteAsync.
	// But the errgroup context is derived from the parent, so Wait() may
	// return the context error. Either outcome is acceptable.
	if execErr != nil {
		assert.ErrorIs(t, execErr, context.Canceled)
	} else {
		require.Len(t, results, 1)
		assert.NotEmpty(t, results[0].ToolResult.Error)
	}
}

func TestParallelToolCalls_SingleToolCall(t *testing.T) {
	executor := &delayedExecutor{
		name:    "single-exec",
		delay:   1 * time.Millisecond,
		status:  tools.ToolStatusComplete,
		content: []byte(`{"result":"hello"}`),
	}
	toolNames := []string{"solo_tool"}
	stage := setupDelayedStage(t, executor, toolNames, nil)

	results, err := stage.executeToolCalls(context.Background(), []types.MessageToolCall{
		{ID: "c1", Name: "solo_tool", Args: json.RawMessage(`{}`)},
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "c1", results[0].ToolResult.ID)
	assert.Contains(t, results[0].ToolResult.GetTextContent(), "hello")
}

func TestParallelToolCalls_EmptyToolCalls(t *testing.T) {
	executor := &delayedExecutor{
		name:    "empty-exec",
		delay:   1 * time.Millisecond,
		status:  tools.ToolStatusComplete,
		content: []byte(`{}`),
	}
	toolNames := []string{"tool"}
	stage := setupDelayedStage(t, executor, toolNames, nil)

	results, err := stage.executeToolCalls(context.Background(), []types.MessageToolCall{})

	require.NoError(t, err)
	assert.Empty(t, results)
}
