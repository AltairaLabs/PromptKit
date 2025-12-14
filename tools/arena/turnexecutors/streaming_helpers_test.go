package turnexecutors

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

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
