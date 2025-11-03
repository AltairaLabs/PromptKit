package turnexecutors

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestScriptedExecutor_UpdateAssistantMessage(t *testing.T) {
	executor := &ScriptedExecutor{}

	tests := []struct {
		name  string
		msg   types.Message
		chunk providers.StreamChunk
		want  types.Message
	}{
		{
			name: "update content",
			msg: types.Message{
				Role:    "assistant",
				Content: "Hello",
			},
			chunk: providers.StreamChunk{
				Content: "Hello World",
				Delta:   " World",
			},
			want: types.Message{
				Role:    "assistant",
				Content: "Hello World",
			},
		},
		{
			name: "add tool calls",
			msg: types.Message{
				Role:    "assistant",
				Content: "Using tools",
			},
			chunk: providers.StreamChunk{
				Content: "Using tools",
				ToolCalls: []types.MessageToolCall{
					{ID: "call1", Name: "search", Args: []byte(`{"query": "test"}`)},
				},
			},
			want: types.Message{
				Role:    "assistant",
				Content: "Using tools",
				ToolCalls: []types.MessageToolCall{
					{ID: "call1", Name: "search", Args: []byte(`{"query": "test"}`)},
				},
			},
		},
		{
			name: "multiple tool calls",
			msg: types.Message{
				Role: "assistant",
			},
			chunk: providers.StreamChunk{
				ToolCalls: []types.MessageToolCall{
					{ID: "call1", Name: "search", Args: nil},
					{ID: "call2", Name: "calculate", Args: []byte(`{"x": 5}`)},
				},
			},
			want: types.Message{
				Role: "assistant",
				ToolCalls: []types.MessageToolCall{
					{ID: "call1", Name: "search", Args: nil},
					{ID: "call2", Name: "calculate", Args: []byte(`{"x": 5}`)},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := executor.updateAssistantMessage(tt.msg, tt.chunk)

			if got.Role != tt.want.Role {
				t.Errorf("Role = %q, want %q", got.Role, tt.want.Role)
			}
			if got.Content != tt.want.Content {
				t.Errorf("Content = %q, want %q", got.Content, tt.want.Content)
			}
			if len(got.ToolCalls) != len(tt.want.ToolCalls) {
				t.Errorf("ToolCalls length = %d, want %d", len(got.ToolCalls), len(tt.want.ToolCalls))
			}
			for i := range got.ToolCalls {
				if got.ToolCalls[i].ID != tt.want.ToolCalls[i].ID {
					t.Errorf("ToolCall[%d].ID = %q, want %q", i, got.ToolCalls[i].ID, tt.want.ToolCalls[i].ID)
				}
				if got.ToolCalls[i].Name != tt.want.ToolCalls[i].Name {
					t.Errorf("ToolCall[%d].Name = %q, want %q", i, got.ToolCalls[i].Name, tt.want.ToolCalls[i].Name)
				}
			}
		})
	}
}

func TestScriptedExecutor_UpdateMessagesList(t *testing.T) {
	executor := &ScriptedExecutor{}

	tests := []struct {
		name           string
		messages       []types.Message
		assistantMsg   types.Message
		assistantIndex int
		wantLength     int
	}{
		{
			name: "append new assistant message",
			messages: []types.Message{
				{Role: "user", Content: "Hello"},
			},
			assistantMsg: types.Message{
				Role:    "assistant",
				Content: "Hi there",
			},
			assistantIndex: 1,
			wantLength:     2,
		},
		{
			name: "update existing assistant message",
			messages: []types.Message{
				{Role: "user", Content: "Hello"},
				{Role: "assistant", Content: "Hi"},
			},
			assistantMsg: types.Message{
				Role:    "assistant",
				Content: "Hi there, updated",
			},
			assistantIndex: 1,
			wantLength:     2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := executor.updateMessagesList(tt.messages, tt.assistantMsg, tt.assistantIndex)

			if len(got) != tt.wantLength {
				t.Errorf("messages length = %d, want %d", len(got), tt.wantLength)
			}
			if got[tt.assistantIndex].Content != tt.assistantMsg.Content {
				t.Errorf("assistant message content = %q, want %q",
					got[tt.assistantIndex].Content, tt.assistantMsg.Content)
			}
		})
	}
}

func TestSelfPlayExecutor_UpdateAssistantMessage(t *testing.T) {
	executor := &SelfPlayExecutor{}

	tests := []struct {
		name  string
		msg   types.Message
		chunk providers.StreamChunk
		want  types.Message
	}{
		{
			name: "update content",
			msg: types.Message{
				Role:    "assistant",
				Content: "Test",
			},
			chunk: providers.StreamChunk{
				Content: "Test response",
				Delta:   " response",
			},
			want: types.Message{
				Role:    "assistant",
				Content: "Test response",
			},
		},
		{
			name: "add tool calls",
			msg: types.Message{
				Role: "assistant",
			},
			chunk: providers.StreamChunk{
				ToolCalls: []types.MessageToolCall{
					{ID: "call1", Name: "tool1", Args: nil},
				},
			},
			want: types.Message{
				Role: "assistant",
				ToolCalls: []types.MessageToolCall{
					{ID: "call1", Name: "tool1", Args: nil},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := executor.updateAssistantMessage(tt.msg, tt.chunk)

			if got.Role != tt.want.Role {
				t.Errorf("Role = %q, want %q", got.Role, tt.want.Role)
			}
			if got.Content != tt.want.Content {
				t.Errorf("Content = %q, want %q", got.Content, tt.want.Content)
			}
			if len(got.ToolCalls) != len(tt.want.ToolCalls) {
				t.Errorf("ToolCalls length = %d, want %d", len(got.ToolCalls), len(tt.want.ToolCalls))
			}
		})
	}
}

func TestSelfPlayExecutor_UpdateMessagesList(t *testing.T) {
	executor := &SelfPlayExecutor{}

	tests := []struct {
		name           string
		messages       []types.Message
		assistantMsg   types.Message
		assistantIndex int
		wantLength     int
	}{
		{
			name: "append new message",
			messages: []types.Message{
				{Role: "user", Content: "Question"},
			},
			assistantMsg: types.Message{
				Role:    "assistant",
				Content: "Answer",
			},
			assistantIndex: 1,
			wantLength:     2,
		},
		{
			name: "update existing message",
			messages: []types.Message{
				{Role: "user", Content: "Question"},
				{Role: "assistant", Content: "Partial"},
			},
			assistantMsg: types.Message{
				Role:    "assistant",
				Content: "Complete answer",
			},
			assistantIndex: 1,
			wantLength:     2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := executor.updateMessagesList(tt.messages, tt.assistantMsg, tt.assistantIndex)

			if len(got) != tt.wantLength {
				t.Errorf("messages length = %d, want %d", len(got), tt.wantLength)
			}
			if got[tt.assistantIndex].Content != tt.assistantMsg.Content {
				t.Errorf("assistant message content = %q, want %q",
					got[tt.assistantIndex].Content, tt.assistantMsg.Content)
			}
		})
	}
}
