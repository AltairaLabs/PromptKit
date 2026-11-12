package sdk

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// A tool whose calls fail twice in one turn is dropped from the model's tool
// set, and the conversation's bus says so: which tool, why, after how many
// failures, and at which round.
func TestToolExcluded_IsPublishedWhenTheLoopDropsATool(t *testing.T) {
	repo := newGateTurnRepo()
	call := mock.Turn{Type: "tool_calls", Content: "Sending.",
		ToolCalls: []mock.ToolCall{{Name: "send_message", Arguments: map[string]any{"body": "hi"}}}}
	repo.add(1, call)
	repo.add(2, call)
	repo.add(3, mock.Turn{Type: "text", Content: "I could not send it."})
	conv, err := Open(writeGatePack(t), "assist",
		WithProvider(mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)),
		WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	conv.OnTool("send_message", func(map[string]any) (any, error) { return nil, errors.New("mail server down") })

	var mu sync.Mutex
	var got []events.ToolExcludedData
	done := make(chan struct{}, 1)
	conv.EventBus().Subscribe(events.EventToolExcluded, func(e *events.Event) {
		data, ok := e.Data.(*events.ToolExcludedData)
		require.True(t, ok, "tool.excluded carries *ToolExcludedData, got %T", e.Data)
		mu.Lock()
		got = append(got, *data)
		mu.Unlock()
		done <- struct{}{}
	})

	resp, err := conv.Send(context.Background(), "message the customer")
	require.NoError(t, err)
	assert.Equal(t, "I could not send it.", resp.Text())

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("no tool.excluded event")
	}
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []events.ToolExcludedData{{
		ToolName: "send_message", Reason: "rejected twice in one tool loop", Rejections: 2, Round: 2,
	}}, got)
}
