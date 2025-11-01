package sdk

import (
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestConversation_HasPendingTools(t *testing.T) {
	tests := []struct {
		name     string
		messages []types.Message
		want     bool
	}{
		{
			name:     "empty conversation",
			messages: []types.Message{},
			want:     false,
		},
		{
			name: "no tool calls",
			messages: []types.Message{
				{Role: "user", Content: "Hello"},
				{Role: "assistant", Content: "Hi there"},
			},
			want: false,
		},
		{
			name: "tool call with result",
			messages: []types.Message{
				{Role: "user", Content: "Send email"},
				{
					Role: "assistant",
					ToolCalls: []types.MessageToolCall{
						{ID: "call_1", Name: "send_email"},
					},
				},
				{
					Role: "tool",
					ToolResult: &types.MessageToolResult{
						ID:      "call_1",
						Content: `{"status":"sent"}`,
					},
				},
			},
			want: false,
		},
		{
			name: "tool call without result - pending",
			messages: []types.Message{
				{Role: "user", Content: "Send email to CEO"},
				{
					Role: "assistant",
					ToolCalls: []types.MessageToolCall{
						{ID: "call_2", Name: "send_email"},
					},
				},
			},
			want: true,
		},
		{
			name: "multiple tool calls, one pending",
			messages: []types.Message{
				{Role: "user", Content: "Check weather and send email"},
				{
					Role: "assistant",
					ToolCalls: []types.MessageToolCall{
						{ID: "call_3", Name: "get_weather"},
						{ID: "call_4", Name: "send_email"},
					},
				},
				{
					Role: "tool",
					ToolResult: &types.MessageToolResult{
						ID:      "call_3",
						Content: `{"temp":72}`,
					},
				},
				// call_4 has no result - pending
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conv := &Conversation{
				state: &statestore.ConversationState{
					Messages: tt.messages,
				},
			}

			got := conv.HasPendingTools()
			if got != tt.want {
				t.Errorf("HasPendingTools() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConversation_GetPendingTools(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]interface{}
		want     int // number of pending tools
	}{
		{
			name:     "no metadata",
			metadata: nil,
			want:     0,
		},
		{
			name:     "no pending_tools key",
			metadata: map[string]interface{}{"other": "data"},
			want:     0,
		},
		{
			name: "pending_tools as slice of PendingToolInfo",
			metadata: map[string]interface{}{
				"pending_tools": []tools.PendingToolInfo{
					{
						ToolName: "send_email",
						Reason:   "requires_approval",
						Message:  "Email to CEO requires approval",
					},
				},
			},
			want: 1,
		},
		{
			name: "pending_tools as slice of pointers",
			metadata: map[string]interface{}{
				"pending_tools": []*tools.PendingToolInfo{
					{
						ToolName: "send_email",
						Reason:   "requires_approval",
					},
					{
						ToolName: "delete_account",
						Reason:   "high_risk",
					},
				},
			},
			want: 2,
		},
		{
			name: "pending_tools as []interface{}",
			metadata: map[string]interface{}{
				"pending_tools": []interface{}{
					&tools.PendingToolInfo{
						ToolName: "send_email",
						Reason:   "requires_approval",
					},
				},
			},
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conv := &Conversation{
				state: &statestore.ConversationState{
					Metadata: tt.metadata,
				},
			}

			got := conv.GetPendingTools()
			if len(got) != tt.want {
				t.Errorf("GetPendingTools() returned %d tools, want %d", len(got), tt.want)
			}

			// Verify structure
			for _, info := range got {
				if info.ToolName == "" {
					t.Error("PendingToolInfo has empty ToolName")
				}
				if info.Reason == "" {
					t.Error("PendingToolInfo has empty Reason")
				}
			}
		})
	}
}

func TestConversation_AddToolResult(t *testing.T) {
	tests := []struct {
		name         string
		messages     []types.Message
		toolCallID   string
		result       string
		wantErr      bool
		wantMessages int
	}{
		{
			name: "add result for existing tool call",
			messages: []types.Message{
				{Role: "user", Content: "Send email"},
				{
					Role: "assistant",
					ToolCalls: []types.MessageToolCall{
						{ID: "call_1", Name: "send_email"},
					},
				},
			},
			toolCallID:   "call_1",
			result:       `{"status":"sent"}`,
			wantErr:      false,
			wantMessages: 3, // user + assistant + tool
		},
		{
			name: "tool call ID not found",
			messages: []types.Message{
				{Role: "user", Content: "Hello"},
				{Role: "assistant", Content: "Hi"},
			},
			toolCallID:   "call_999",
			result:       `{"status":"sent"}`,
			wantErr:      true,
			wantMessages: 2,
		},
		{
			name:         "empty conversation",
			messages:     []types.Message{},
			toolCallID:   "call_1",
			result:       `{"status":"sent"}`,
			wantErr:      true,
			wantMessages: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conv := &Conversation{
				state: &statestore.ConversationState{
					ID:             "test_conv",
					Messages:       tt.messages,
					LastAccessedAt: time.Now(),
				},
			}

			err := conv.AddToolResult(tt.toolCallID, tt.result)
			if (err != nil) != tt.wantErr {
				t.Errorf("AddToolResult() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(conv.state.Messages) != tt.wantMessages {
				t.Errorf("AddToolResult() resulted in %d messages, want %d", len(conv.state.Messages), tt.wantMessages)
			}

			// If no error, verify the tool result message was added correctly
			if !tt.wantErr && len(conv.state.Messages) > 0 {
				lastMsg := conv.state.Messages[len(conv.state.Messages)-1]
				if lastMsg.Role != "tool" {
					t.Errorf("Last message role = %s, want 'tool'", lastMsg.Role)
				}
				if lastMsg.ToolResult == nil {
					t.Error("ToolResult is nil")
				} else {
					if lastMsg.ToolResult.ID != tt.toolCallID {
						t.Errorf("ToolResult.ID = %s, want %s", lastMsg.ToolResult.ID, tt.toolCallID)
					}
					if lastMsg.Content != tt.result {
						t.Errorf("Message content = %s, want %s", lastMsg.Content, tt.result)
					}
				}
			}
		})
	}
}

func TestConversation_AddToolResult_Multiple(t *testing.T) {
	// Test adding multiple tool results
	conv := &Conversation{
		state: &statestore.ConversationState{
			ID: "test_conv",
			Messages: []types.Message{
				{Role: "user", Content: "Do two things"},
				{
					Role: "assistant",
					ToolCalls: []types.MessageToolCall{
						{ID: "call_1", Name: "tool_1"},
						{ID: "call_2", Name: "tool_2"},
					},
				},
			},
			LastAccessedAt: time.Now(),
		},
	}

	// Add first result
	err := conv.AddToolResult("call_1", `{"result":"one"}`)
	if err != nil {
		t.Fatalf("AddToolResult(call_1) failed: %v", err)
	}

	// Add second result
	err = conv.AddToolResult("call_2", `{"result":"two"}`)
	if err != nil {
		t.Fatalf("AddToolResult(call_2) failed: %v", err)
	}

	// Should have 4 messages: user, assistant, tool1, tool2
	if len(conv.state.Messages) != 4 {
		t.Errorf("Expected 4 messages, got %d", len(conv.state.Messages))
	}

	// Verify both tool results
	toolResults := 0
	for _, msg := range conv.state.Messages {
		if msg.Role == "tool" {
			toolResults++
		}
	}
	if toolResults != 2 {
		t.Errorf("Expected 2 tool result messages, got %d", toolResults)
	}
}
