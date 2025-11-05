package pipeline

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestPipelineWithMultimodalMessages tests that multimodal messages flow through the pipeline correctly
func TestPipelineWithMultimodalMessages(t *testing.T) {
	// Create a simple middleware that validates message handling
	validator := &multimodalValidatorMiddleware{}
	
	pipeline := NewPipeline(validator)

	// Test 1: Legacy text message
	t.Run("legacy text message", func(t *testing.T) {
		result, err := pipeline.Execute(context.Background(), "user", "Hello")
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		
		if len(result.Messages) != 1 {
			t.Errorf("Expected 1 message, got %d", len(result.Messages))
		}
		
		msg := result.Messages[0]
		if msg.Role != "user" {
			t.Errorf("Expected role 'user', got %q", msg.Role)
		}
		if msg.Content != "Hello" {
			t.Errorf("Expected content 'Hello', got %q", msg.Content)
		}
	})

	// Test 2: Pre-constructed multimodal message
	t.Run("multimodal message in context", func(t *testing.T) {
		multimodalMsg := types.Message{Role: "user"}
		multimodalMsg.AddTextPart("What's in this image?")
		multimodalMsg.AddImagePartFromURL("https://example.com/image.jpg", nil)
		
		// Create context with pre-existing message
		internalCtx := &ExecutionContext{
			Context:  context.Background(),
			Messages: []types.Message{multimodalMsg},
			Metadata: make(map[string]interface{}),
		}
		
		err := validator.Process(internalCtx, func() error { return nil })
		if err != nil {
			t.Fatalf("Process() error = %v", err)
		}
		
		// Verify the message stayed multimodal
		if !internalCtx.Messages[0].IsMultimodal() {
			t.Error("Message should still be multimodal")
		}
		if !internalCtx.Messages[0].HasMediaContent() {
			t.Error("Message should have media content")
		}
	})
}

// TestExecutionContextWithMultimodalMessages tests ExecutionContext handling of multimodal messages
func TestExecutionContextWithMultimodalMessages(t *testing.T) {
	ctx := &ExecutionContext{
		Context:  context.Background(),
		Messages: []types.Message{},
		Metadata: make(map[string]interface{}),
	}

	// Add a multimodal message
	msg := types.Message{Role: "user"}
	msg.AddTextPart("Analyze this:")
	msg.AddImagePartFromURL("https://example.com/chart.png", nil)
	
	ctx.Messages = append(ctx.Messages, msg)

	if len(ctx.Messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(ctx.Messages))
	}

	// Verify message properties
	stored := ctx.Messages[0]
	if !stored.IsMultimodal() {
		t.Error("Stored message should be multimodal")
	}
	if !stored.HasMediaContent() {
		t.Error("Stored message should have media content")
	}
	if len(stored.Parts) != 2 {
		t.Errorf("Expected 2 parts, got %d", len(stored.Parts))
	}
}

// TestPipelineMultimodalMessageCloning tests that messages are properly cloned
func TestPipelineMultimodalMessageCloning(t *testing.T) {
	original := types.Message{Role: "user"}
	original.AddTextPart("Original text")
	original.AddImagePartFromURL("https://example.com/image.jpg", nil)

	// Clone the message
	cloned := types.CloneMessage(original)

	// Modify the clone
	cloned.Parts[0].Text = stringPtr("Modified text")

	// Verify original is unchanged
	if *original.Parts[0].Text == "Modified text" {
		t.Error("Original message was modified when clone was changed")
	}
}

// TestPipelineMultimodalMessageMigration tests migration between formats in pipeline
func TestPipelineMultimodalMessageMigration(t *testing.T) {
	t.Run("migrate legacy to multimodal", func(t *testing.T) {
		msg := types.Message{
			Role:    "user",
			Content: "Legacy content",
		}

		types.MigrateToMultimodal(&msg)

		if !msg.IsMultimodal() {
			t.Error("Message should be multimodal after migration")
		}
		if msg.GetContent() != "Legacy content" {
			t.Error("Content should be preserved during migration")
		}
	})

	t.Run("migrate multimodal to legacy", func(t *testing.T) {
		msg := types.Message{Role: "user"}
		msg.AddTextPart("Multimodal text")

		err := types.MigrateToLegacy(&msg)
		if err != nil {
			t.Fatalf("MigrateToLegacy() error = %v", err)
		}

		if msg.IsMultimodal() {
			t.Error("Message should not be multimodal after migration")
		}
		if msg.Content != "Multimodal text" {
			t.Error("Content should be preserved during migration")
		}
	})
}

// TestPipelineMultimodalWithToolCalls tests multimodal messages combined with tool calls
func TestPipelineMultimodalWithToolCalls(t *testing.T) {
	ctx := &ExecutionContext{
		Context:  context.Background(),
		Messages: []types.Message{},
		Metadata: make(map[string]interface{}),
	}

	// Create a multimodal message with tool call response
	msg := types.Message{
		Role: "assistant",
		ToolCalls: []types.MessageToolCall{
			{ID: "call1", Name: "analyze_image"},
		},
	}
	msg.AddTextPart("I'll analyze that image for you.")

	ctx.Messages = append(ctx.Messages, msg)

	// Verify both multimodal and tool calls work together
	if !msg.IsMultimodal() {
		t.Error("Message should be multimodal")
	}
	if len(msg.ToolCalls) != 1 {
		t.Error("Message should have tool calls")
	}
}

// TestPipelineStreamingWithMultimodal tests streaming with multimodal messages
func TestPipelineStreamingWithMultimodal(t *testing.T) {
	ctx := &ExecutionContext{
		Context:      context.Background(),
		Messages:     []types.Message{},
		Metadata:     make(map[string]interface{}),
		StreamMode:   true,
		StreamOutput: make(chan providers.StreamChunk, 10),
	}

	// Add multimodal message
	msg := types.Message{Role: "user"}
	msg.AddTextPart("Stream response about this:")
	msg.AddImagePartFromURL("https://example.com/img.jpg", nil)
	ctx.Messages = append(ctx.Messages, msg)

	// Emit a test chunk
	chunk := providers.StreamChunk{
		Content: "Streaming response...",
	}

	success := ctx.EmitStreamChunk(chunk)
	if !success {
		t.Error("Failed to emit chunk")
	}

	// Read the chunk
	select {
	case received := <-ctx.StreamOutput:
		if received.Content != chunk.Content {
			t.Errorf("Received chunk content = %q, want %q", received.Content, chunk.Content)
		}
	default:
		t.Error("No chunk received")
	}
}

// TestMultimodalMessageInExecutionResult tests that ExecutionResult preserves multimodal messages
func TestMultimodalMessageInExecutionResult(t *testing.T) {
	msg := types.Message{Role: "user"}
	msg.AddTextPart("Test message")
	msg.AddImagePartFromURL("https://example.com/img.jpg", nil)

	result := &ExecutionResult{
		Messages: []types.Message{msg},
		CostInfo: types.CostInfo{
			InputTokens:  100,
			OutputTokens: 50,
		},
		Metadata: make(map[string]interface{}),
	}

	// Verify multimodal message is preserved in result
	if len(result.Messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(result.Messages))
	}

	resultMsg := result.Messages[0]
	if !resultMsg.IsMultimodal() {
		t.Error("Message in result should be multimodal")
	}
	if !resultMsg.HasMediaContent() {
		t.Error("Message in result should have media content")
	}
}

// TestPipelineMultimodalMessageBackwardCompatibility tests backward compatibility
func TestPipelineMultimodalMessageBackwardCompatibility(t *testing.T) {
	middleware := &legacyMessageHandlerMiddleware{}
	pipeline := NewPipeline(middleware)

	// Execute with legacy format
	result, err := pipeline.Execute(context.Background(), "user", "Legacy message")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	// Verify legacy format still works
	if len(result.Messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(result.Messages))
	}

	msg := result.Messages[0]
	if msg.Content != "Legacy message" {
		t.Errorf("Expected content 'Legacy message', got %q", msg.Content)
	}
	
	// Legacy messages should not be multimodal by default
	if msg.IsMultimodal() {
		t.Error("Legacy message should not be multimodal")
	}
}

// TestMultimodalMessageValidation tests validation of multimodal messages
func TestMultimodalMessageValidation(t *testing.T) {
	tests := []struct {
		name    string
		msg     types.Message
		wantErr bool
	}{
		{
			name: "valid text part",
			msg: types.Message{
				Role:  "user",
				Parts: []types.ContentPart{types.NewTextPart("Valid text")},
			},
			wantErr: false,
		},
		{
			name: "valid image part",
			msg: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					types.NewImagePartFromURL("https://example.com/img.jpg", nil),
				},
			},
			wantErr: false,
		},
		{
			name: "invalid empty text",
			msg: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{Type: types.ContentTypeText, Text: stringPtr("")},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			for _, part := range tt.msg.Parts {
				if validateErr := part.Validate(); validateErr != nil {
					err = validateErr
					break
				}
			}

			if (err != nil) != tt.wantErr {
				t.Errorf("Validation error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// Mock middleware implementations for testing

type multimodalValidatorMiddleware struct{}

func (m *multimodalValidatorMiddleware) Process(ctx *ExecutionContext, next func() error) error {
	// Validate all messages in context
	for i, msg := range ctx.Messages {
		if msg.IsMultimodal() {
			for j, part := range msg.Parts {
				if err := part.Validate(); err != nil {
					ctx.Error = err
					return err
				}
				
				// Track validation in metadata
				if ctx.Metadata["validated_parts"] == nil {
					ctx.Metadata["validated_parts"] = []string{}
				}
				ctx.Metadata["validated_parts"] = append(
					ctx.Metadata["validated_parts"].([]string),
					part.Type,
				)
				
				_ = j // Use variable
			}
		}
		_ = i // Use variable
	}
	return next()
}

func (m *multimodalValidatorMiddleware) StreamChunk(ctx *ExecutionContext, chunk *providers.StreamChunk) error {
	return nil // No-op for streaming
}

type legacyMessageHandlerMiddleware struct{}

func (m *legacyMessageHandlerMiddleware) Process(ctx *ExecutionContext, next func() error) error {
	// This middleware treats all messages as they are (backward compatible)
	for _, msg := range ctx.Messages {
		// Track message format in metadata
		if msg.IsMultimodal() {
			ctx.Metadata["has_multimodal"] = true
		} else {
			ctx.Metadata["has_legacy"] = true
		}
	}
	return next()
}

func (m *legacyMessageHandlerMiddleware) StreamChunk(ctx *ExecutionContext, chunk *providers.StreamChunk) error {
	return nil
}

// Helper function
func stringPtr(s string) *string {
	return &s
}
