//go:build integration

package sdk

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/events"
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
      "system_template": "Answer in one short word."
    }
  }
}`

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

	conv, err := Open(packPath, "chat",
		WithModel("claude-sonnet-4-5-20250929"),
		WithEventBus(bus),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	resp, err := conv.Send(context.Background(), "Reply with the single word OK")
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

	// Every published message must yield text through GetContent, whichever
	// field carries it. Reading .Content alone shows a BLANK user turn: the SDK
	// builds the user message with Parts and the provider returns the reply in
	// Content. Only a live turn exposes that split.
	for _, d := range got {
		assert.NotEmptyf(t, d.GetContent(),
			"message at index %d (role %s) published no readable text", d.Index, d.Role)
	}

	var sawUser, sawAssistant bool
	for _, d := range got {
		switch d.Role {
		case "user":
			sawUser = true
		case "assistant":
			sawAssistant = true
		}
	}
	assert.True(t, sawUser, "the user turn must reach the bus")
	assert.True(t, sawAssistant, "the model's own reply must reach the bus")
}
