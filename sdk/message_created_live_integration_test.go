//go:build integration

package sdk

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

const liveBusPackJSON = `{
  "$schema": "https://promptpack.org/schema/latest/promptpack.schema.json",
  "id": "live-bus-probe",
  "name": "Live Bus Probe",
  "version": "1.0.0",
  "description": "Pack for the live message.created bus test",
  "template_engine": {"version": "v1", "syntax": "{{variable}}"},
  "prompts": {
    "chat": {
      "id": "chat",
      "name": "Chat",
      "version": "1.0.0",
      "system_template": "Use the get_weather tool when asked about weather.",
      "tools": ["get_weather"]
    }
  },
  "tools": {
    "get_weather": {
      "name": "get_weather",
      "description": "Get the weather for a city",
      "parameters": {
        "type": "object",
        "properties": {"city": {"type": "string"}},
        "required": ["city"]
      }
    }
  }
}`

// liveWeatherExecutor answers the tool call deterministically, so the turn
// exercises a full tool round without depending on an external service.
type liveWeatherExecutor struct{}

func (*liveWeatherExecutor) Name() string { return "live-weather" }
func (*liveWeatherExecutor) Execute(
	_ context.Context, _ *tools.ToolDescriptor, _ json.RawMessage,
) (json.RawMessage, error) {
	return json.RawMessage(`{"tempC":11,"sky":"rain"}`), nil
}

// TestMessageCreated_ReachesTheBusLive drives a REAL provider turn and asserts
// message.created arrives on the bus with no EventStore, no WithRecording and
// no state store.
//
// Every other test for this route uses a mock provider. A mock cannot show that
// the assistant message the model actually produced reaches a subscriber, in
// order, with a transcript-absolute index — which is the whole claim.
func TestMessageCreated_ReachesTheBusLive(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	bus := events.NewEventBus()
	t.Cleanup(bus.Close)

	var mu sync.Mutex
	var got []*events.MessageCreatedData
	bus.Subscribe(events.EventMessageCreated, func(e *events.Event) {
		if d, ok := e.Data.(*events.MessageCreatedData); ok {
			mu.Lock()
			got = append(got, d)
			mu.Unlock()
		}
	})

	dir := t.TempDir()
	packPath := filepath.Join(dir, "live-bus.pack.json")
	require.NoError(t, os.WriteFile(packPath, []byte(liveBusPackJSON), 0o600))

	reg := tools.NewRegistry()
	exec := &liveWeatherExecutor{}
	reg.RegisterExecutor(exec)
	require.NoError(t, reg.Register(&tools.ToolDescriptor{
		Name:        "get_weather",
		Description: "Get the weather for a city",
		Mode:        exec.Name(),
		InputSchema: []byte(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
	}))

	conv, err := Open(packPath, "chat",
		WithModel("claude-sonnet-4-5-20250929"),
		WithEventBus(bus),
		WithToolRegistry(reg),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	// A TOOL-CALLING turn deliberately: it produces all four message shapes,
	// and the assistant's tool-call round is the one with no text at all —
	// the shape a text-only turn cannot exercise.
	resp, err := conv.Send(context.Background(), "What is the weather in Leeds?")
	require.NoError(t, err)
	require.NotEmpty(t, resp.Text())

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, got, "message.created never reached the bus on a real turn")

	sort.Slice(got, func(i, j int) bool { return got[i].Index < got[j].Index })
	for i, d := range got {
		assert.Equalf(t, i, d.Index, "index %d out of order: %+v", i, d)
		parts := ""
		for _, pt := range d.Parts {
			if pt.Text != nil {
				parts += *pt.Text
			}
		}
		t.Logf("message.created index=%d role=%s content=%q parts=%q nparts=%d",
			d.Index, d.Role, d.Content, parts, len(d.Parts))
	}

	// Observed shapes on a real tool-calling turn — every one differs, which is
	// why fixtures must come from here rather than from what the struct allows:
	//
	//   user      Content=""    Parts=[text]   -> text is in Parts
	//   assistant Content=""    ToolCalls=[1]  -> NO text at all, legitimately
	//   tool      Content=json  ToolResult set
	//   assistant Content=text                 -> text is in Content
	//
	// So the rule is not "everything has text". It is: a message either yields
	// text, or it is a tool-call round. Reading .Content directly fails the
	// first row — a blank user turn, every conversation.
	for _, d := range got {
		if d.GetContent() != "" {
			continue
		}
		assert.NotEmptyf(t, d.ToolCalls,
			"message at index %d (role %s) yielded no text and made no tool call, "+
				"so a consumer has nothing to render", d.Index, d.Role)
	}

	var sawUserText, sawToolCall, sawToolResult, sawAssistantText bool
	for _, d := range got {
		switch {
		case d.Role == "user" && d.GetContent() != "":
			sawUserText = true
		case d.Role == "assistant" && len(d.ToolCalls) > 0:
			sawToolCall = true
		case d.Role == "tool" && d.ToolResult != nil:
			sawToolResult = true
		case d.Role == "assistant" && d.GetContent() != "":
			sawAssistantText = true
		}
	}
	assert.True(t, sawUserText, "the user turn must reach the bus WITH readable text")
	assert.True(t, sawToolCall, "the tool-calling round must reach the bus")
	assert.True(t, sawToolResult, "the tool result must reach the bus")
	assert.True(t, sawAssistantText, "the model's final reply must reach the bus")
}
