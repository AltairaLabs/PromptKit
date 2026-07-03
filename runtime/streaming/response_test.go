package streaming

import (
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func strPtr(s string) *string { return &s }

func TestResponseAction_String(t *testing.T) {
	tests := []struct {
		name   string
		action ResponseAction
		want   string
	}{
		{"continue", ResponseActionContinue, "continue"},
		{"complete", ResponseActionComplete, "complete"},
		{"error", ResponseActionError, "error"},
		{"tool_calls", ResponseActionToolCalls, "tool_calls"},
		{"unknown", ResponseAction(99), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.action.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProcessResponseElement(t *testing.T) {
	sentinelErr := errors.New("boom")

	tests := []struct {
		name       string
		elem       *stage.StreamElement
		wantAction ResponseAction
		wantErr    error // exact error, or nil
	}{
		{
			name:       "error element returns error action with error",
			elem:       &stage.StreamElement{Error: sentinelErr},
			wantAction: ResponseActionError,
			wantErr:    sentinelErr,
		},
		{
			name:       "interrupted signal continues",
			elem:       &stage.StreamElement{Meta: stage.ElementMetadata{Interrupted: true}},
			wantAction: ResponseActionContinue,
			wantErr:    nil,
		},
		{
			name:       "interrupted turn complete continues",
			elem:       &stage.StreamElement{Meta: stage.ElementMetadata{InterruptedTurnComplete: true}},
			wantAction: ResponseActionContinue,
			wantErr:    nil,
		},
		{
			name:       "end of stream with nil message is empty response error",
			elem:       &stage.StreamElement{EndOfStream: true},
			wantAction: ResponseActionError,
			wantErr:    ErrEmptyResponse,
		},
		{
			name: "end of stream with empty message is empty response error",
			elem: &stage.StreamElement{
				EndOfStream: true,
				Message:     &types.Message{},
			},
			wantAction: ResponseActionError,
			wantErr:    ErrEmptyResponse,
		},
		{
			name: "end of stream with content completes",
			elem: &stage.StreamElement{
				EndOfStream: true,
				Message:     &types.Message{Content: "hello"},
			},
			wantAction: ResponseActionComplete,
			wantErr:    nil,
		},
		{
			name: "end of stream with parts completes",
			elem: &stage.StreamElement{
				EndOfStream: true,
				Message: &types.Message{
					Parts: []types.ContentPart{{Type: types.ContentTypeText, Text: strPtr("hi")}},
				},
			},
			wantAction: ResponseActionComplete,
			wantErr:    nil,
		},
		{
			name: "end of stream with tool calls signals tool execution",
			elem: &stage.StreamElement{
				EndOfStream: true,
				Message: &types.Message{
					ToolCalls: []types.MessageToolCall{{ID: "1", Name: "search"}},
				},
			},
			wantAction: ResponseActionToolCalls,
			wantErr:    nil,
		},
		{
			name: "end of stream with tool calls and no content still signals tools",
			elem: &stage.StreamElement{
				EndOfStream: true,
				Message: &types.Message{
					ToolCalls: []types.MessageToolCall{{ID: "1", Name: "search"}},
				},
			},
			wantAction: ResponseActionToolCalls,
			wantErr:    nil,
		},
		{
			name:       "non-end-of-stream streaming chunk continues",
			elem:       &stage.StreamElement{Text: strPtr("partial")},
			wantAction: ResponseActionContinue,
			wantErr:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, err := ProcessResponseElement(tt.elem, "test")
			if action != tt.wantAction {
				t.Errorf("action = %v, want %v", action, tt.wantAction)
			}
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("err = %v, want nil", err)
				}
			} else if !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
