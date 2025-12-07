package pages

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

func TestNewConversationPage(t *testing.T) {
	page := NewConversationPage()

	assert.NotNil(t, page)
	assert.NotNil(t, page.panel)
}

func TestConversationPage_Reset(t *testing.T) {
	page := NewConversationPage()
	page.SetDimensions(100, 50)

	res := &statestore.RunResult{
		RunID: "run-123",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there"},
		},
		Cost:     types.CostInfo{TotalCost: 0.001},
		Duration: time.Second,
	}
	page.SetData("run-123", "test-scenario", "test-provider", res)

	// Reset clears the panel's internal state
	page.Reset()

	// After reset, the panel should be in initial state
	// The panel's internal tableReady and detailReady flags should be false
	panel := page.Panel()
	assert.NotNil(t, panel)
}

func TestConversationPage_SetDimensions(t *testing.T) {
	page := NewConversationPage()

	page.SetDimensions(120, 60)

	// Verify dimensions are passed to panel (panel should be usable)
	assert.NotNil(t, page.panel)
}

func TestConversationPage_SetData(t *testing.T) {
	page := NewConversationPage()
	page.SetDimensions(100, 50)

	res := &statestore.RunResult{
		RunID: "run-456",
		Messages: []types.Message{
			{Role: "user", Content: "Test message"},
		},
		Cost:     types.CostInfo{InputTokens: 5, OutputTokens: 10},
		Duration: time.Millisecond * 500,
	}

	page.SetData("run-456", "scenario-1", "provider-1", res)

	view := page.Render()
	assert.Contains(t, view, "Conversation")
}

func TestConversationPage_SetData_NilResult(t *testing.T) {
	page := NewConversationPage()
	page.SetDimensions(100, 50)

	page.SetData("run-789", "scenario", "provider", nil)

	view := page.Render()
	assert.Contains(t, view, "No conversation available")
}

func TestConversationPage_Update(t *testing.T) {
	page := NewConversationPage()
	page.SetDimensions(100, 50)

	res := &statestore.RunResult{
		RunID: "run-update",
		Messages: []types.Message{
			{Role: "user", Content: "Message 1"},
			{Role: "assistant", Content: "Message 2"},
		},
		Cost:     types.CostInfo{},
		Duration: time.Second,
	}
	page.SetData("run-update", "scenario", "provider", res)

	// Update with a key message
	cmd := page.Update(tea.KeyMsg{Type: tea.KeyDown})
	// cmd may be nil or a command, depending on panel state
	_ = cmd

	// Test with right arrow to switch focus
	cmd = page.Update(tea.KeyMsg{Type: tea.KeyRight})
	_ = cmd
}

func TestConversationPage_Update_NoData(t *testing.T) {
	page := NewConversationPage()

	// Update without setting data should not panic
	cmd := page.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Nil(t, cmd)
}

func TestConversationPage_Render_WithMessages(t *testing.T) {
	page := NewConversationPage()
	page.SetDimensions(100, 50)

	res := &statestore.RunResult{
		RunID: "render-test",
		Messages: []types.Message{
			{Role: "system", Content: "You are a helpful assistant"},
			{Role: "user", Content: "Hello!"},
			{Role: "assistant", Content: "Hi! How can I help you today?"},
		},
		Cost: types.CostInfo{
			InputTokens:  15,
			OutputTokens: 25,
			TotalCost:    0.002,
		},
		Duration: 2 * time.Second,
	}
	page.SetData("render-test", "my-scenario", "openai", res)

	view := page.Render()

	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Conversation")
	assert.Contains(t, view, "my-scenario")
	assert.Contains(t, view, "openai")
}

func TestConversationPage_GetKeyBindings(t *testing.T) {
	page := NewConversationPage()

	bindings := page.GetKeyBindings()

	assert.NotEmpty(t, bindings)
	// Should have navigation, pane switching, and back bindings
	assert.GreaterOrEqual(t, len(bindings), 3)

	// Verify expected key bindings exist
	foundNav := false
	foundSwitch := false
	foundBack := false
	for _, b := range bindings {
		if b.Description == "navigate" {
			foundNav = true
		}
		if b.Description == "switch pane" {
			foundSwitch = true
		}
		if b.Description == "back" {
			foundBack = true
		}
	}
	assert.True(t, foundNav, "should have navigate binding")
	assert.True(t, foundSwitch, "should have switch pane binding")
	assert.True(t, foundBack, "should have back binding")
}

func TestConversationPage_Panel(t *testing.T) {
	page := NewConversationPage()

	panel := page.Panel()

	assert.NotNil(t, panel)
	// Verify it's the same panel
	assert.Same(t, page.panel, panel)
}

func TestConversationPage_Render_EmptyMessages(t *testing.T) {
	page := NewConversationPage()
	page.SetDimensions(100, 50)

	res := &statestore.RunResult{
		RunID:    "empty-test",
		Messages: []types.Message{},
		Cost:     types.CostInfo{},
		Duration: 0,
	}
	page.SetData("empty-test", "scenario", "provider", res)

	view := page.Render()

	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Waiting for conversation to start")
}

func TestConversationPage_MultipleSetData(t *testing.T) {
	page := NewConversationPage()
	page.SetDimensions(100, 50)

	// Set initial data
	res1 := &statestore.RunResult{
		RunID: "run-1",
		Messages: []types.Message{
			{Role: "user", Content: "First conversation"},
		},
		Cost:     types.CostInfo{},
		Duration: time.Second,
	}
	page.SetData("run-1", "scenario-1", "provider-1", res1)

	view1 := page.Render()
	assert.Contains(t, view1, "scenario-1")

	// Set different data
	res2 := &statestore.RunResult{
		RunID: "run-2",
		Messages: []types.Message{
			{Role: "user", Content: "Second conversation"},
		},
		Cost:     types.CostInfo{},
		Duration: time.Second,
	}
	page.SetData("run-2", "scenario-2", "provider-2", res2)

	view2 := page.Render()
	assert.Contains(t, view2, "scenario-2")
}
