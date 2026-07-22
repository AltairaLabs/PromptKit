package sdk

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gateTurnRepo scripts mock LLM turns for the gate integration test.
type gateTurnRepo struct {
	mu    sync.RWMutex
	turns map[string]*mock.Turn
}

func newGateTurnRepo() *gateTurnRepo { return &gateTurnRepo{turns: map[string]*mock.Turn{}} }

func (r *gateTurnRepo) add(n int, turn mock.Turn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.turns[fmt.Sprintf("default:%d", n)] = &turn
}

func (r *gateTurnRepo) GetResponse(ctx context.Context, p mock.ResponseParams) (string, error) {
	t, err := r.GetTurn(ctx, p)
	if err != nil {
		return "", err
	}
	return t.Content, nil
}

func (r *gateTurnRepo) GetTurn(_ context.Context, p mock.ResponseParams) (*mock.Turn, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if t, ok := r.turns[fmt.Sprintf("default:%d", p.TurnNumber)]; ok {
		return t, nil
	}
	return &mock.Turn{Type: "text", Content: ""}, nil
}

// A pack with a single tool the model can call.
func writeGatePack(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "gate.pack.json")
	body := `{
      "$schema": "https://promptpack.org/schema/latest/promptpack.schema.json",
      "id": "gate", "name": "gate", "version": "1.0.0",
      "template_engine": {"version": "v1", "syntax": "{{variable}}"},
      "prompts": {"assist": {"id": "assist", "name": "a", "version": "1.0.0",
        "system_template": "You help.",
        "tools": ["send_message"]}},
      "tools": {"send_message": {"name": "send_message", "description": "send",
        "parameters": {"type": "object", "properties": {"body": {"type": "string"}}}}}
    }`
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

// The standard (unary) pipeline must now enforce the OnToolAsync approval gate:
// a held tool is surfaced pending and NOT executed until ResolveTool. This is
// the path that previously bypassed HITL entirely (CheckPending was dead code).
func TestOnToolAsync_GatesOnStandardPipeline(t *testing.T) {
	repo := newGateTurnRepo()
	repo.add(1, mock.Turn{
		Type:      "tool_calls",
		Content:   "Sending.",
		ToolCalls: []mock.ToolCall{{Name: "send_message", Arguments: map[string]any{"body": "hi"}}},
	})
	repo.add(2, mock.Turn{Type: "text", Content: "Done."})
	provider := mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)

	conv, err := Open(writeGatePack(t), "assist", WithProvider(provider), WithSkipSchemaValidation())
	require.NoError(t, err)
	defer func() { _ = conv.Close() }()

	var executedWith string
	conv.OnToolAsync(
		"send_message",
		func(args map[string]any) sdktools.PendingResult {
			return sdktools.PendingResult{Reason: "requires_approval", Message: "Approve send?"}
		},
		func(args map[string]any) (any, error) {
			executedWith, _ = args["body"].(string)
			return map[string]any{"sent": args["body"]}, nil
		},
	)

	resp, err := conv.Send(context.Background(), "please send a message")
	require.NoError(t, err)

	// HELD: surfaced pending, and the handler did NOT run.
	pending := resp.PendingTools()
	require.Len(t, pending, 1, "tool must be held pending on the standard pipeline")
	assert.Equal(t, "send_message", pending[0].Name)
	assert.Equal(t, "requires_approval", pending[0].Reason)
	assert.Empty(t, executedWith, "held tool must not execute before approval")

	// Also resolvable via the conversation store.
	stored, err := conv.PendingTools(context.Background())
	require.NoError(t, err)
	require.Len(t, stored, 1)

	// APPROVE → the handler runs.
	_, err = conv.ResolveTool(context.Background(), pending[0].ID)
	require.NoError(t, err)
	assert.Equal(t, "hi", executedWith, "approval must execute the held tool")
}

// Approve-with-edits (#1651) now works on the standard pipeline too, because
// the gate surfaces the call and ResolveToolWithArgs re-runs the handler with
// the reviewer's overrides.
func TestOnToolAsync_ApproveWithEditsOnStandardPipeline(t *testing.T) {
	repo := newGateTurnRepo()
	repo.add(1, mock.Turn{
		Type:      "tool_calls",
		Content:   "Sending.",
		ToolCalls: []mock.ToolCall{{Name: "send_message", Arguments: map[string]any{"body": "original"}}},
	})
	repo.add(2, mock.Turn{Type: "text", Content: "Done."})
	provider := mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)

	conv, err := Open(writeGatePack(t), "assist", WithProvider(provider), WithSkipSchemaValidation())
	require.NoError(t, err)
	defer func() { _ = conv.Close() }()

	var executedWith string
	conv.OnToolAsync(
		"send_message",
		func(map[string]any) sdktools.PendingResult { return sdktools.PendingResult{Reason: "approval"} },
		func(args map[string]any) (any, error) {
			executedWith, _ = args["body"].(string)
			return "ok", nil
		},
	)

	resp, err := conv.Send(context.Background(), "send it")
	require.NoError(t, err)
	pending := resp.PendingTools()
	require.Len(t, pending, 1)

	// Reviewer edits the body before approving.
	res, err := conv.ResolveToolWithArgs(context.Background(), pending[0].ID, map[string]any{"body": "edited"})
	require.NoError(t, err)
	assert.True(t, res.Edited)
	assert.Equal(t, "edited", executedWith, "approve-with-edits must run the handler with the override")
}

// The WithIngestion streaming-duplex path (the Guardian per-turn shape) must
// enforce the same gate: the streaming ProviderStage built for a duplex agent
// consults the approval checker, holds the tool pending, and does not execute it
// until ResolveTool. This is the second of the two paths #1653 wires (the first
// being the unary Send path above); the ASM DuplexProviderStage already gated.
func TestOnToolAsync_GatesOnWithIngestionDuplexPipeline(t *testing.T) {
	repo := newGateTurnRepo()
	repo.add(1, mock.Turn{
		Type:      "tool_calls",
		Content:   "Sending.",
		ToolCalls: []mock.ToolCall{{Name: "send_message", Arguments: map[string]any{"body": "hi"}}},
	})
	provider := mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)

	ingest := IngestionFunc(func(b *stage.PipelineBuilder) (string, error) {
		b.AddStage(newTurnEmitterStage("turn_emitter"))
		return "turn_emitter", nil
	})

	conv, err := OpenDuplex(writeGatePack(t), "assist",
		WithProvider(provider),
		WithSkipSchemaValidation(),
		WithIngestion(ingest),
	)
	require.NoError(t, err)
	defer func() { _ = conv.Close() }()

	var mu sync.Mutex
	var executedWith string
	conv.OnToolAsync(
		"send_message",
		func(map[string]any) sdktools.PendingResult {
			return sdktools.PendingResult{Reason: "requires_approval", Message: "Approve send?"}
		},
		func(args map[string]any) (any, error) {
			mu.Lock()
			executedWith, _ = args["body"].(string)
			mu.Unlock()
			return map[string]any{"sent": args["body"]}, nil
		},
	)

	responseCh, err := conv.Response()
	require.NoError(t, err)
	go func() {
		for range responseCh { //nolint:revive // drain so the pipeline output never blocks
		}
	}()

	ctx := context.Background()
	require.NoError(t, conv.SendChunk(ctx, &providers.StreamChunk{Content: "please send", Source: "caller"}))

	// HELD: the streaming ProviderStage surfaces the call in the pending store and
	// the handler does not run while the session stays open.
	require.Eventually(t, func() bool {
		held, _ := conv.PendingTools(ctx)
		return len(held) == 1
	}, 5*time.Second, 10*time.Millisecond,
		"WithIngestion streaming ProviderStage must hold the tool pending")
	mu.Lock()
	assert.Empty(t, executedWith, "held tool must not execute before approval")
	mu.Unlock()

	pending, err := conv.PendingTools(ctx)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, "send_message", pending[0].Name)
	assert.Equal(t, "requires_approval", pending[0].Reason)

	// APPROVE → the handler runs with the model's proposed args.
	_, err = conv.ResolveTool(ctx, pending[0].ID)
	require.NoError(t, err)
	mu.Lock()
	assert.Equal(t, "hi", executedWith, "approval must execute the held tool")
	mu.Unlock()
}
