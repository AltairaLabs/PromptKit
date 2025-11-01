package middleware

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
)

// mockValidator is a test validator that fails if content contains a specific string
type mockValidator struct {
	failOn string
	called bool
}

func (m *mockValidator) Validate(content string, params map[string]interface{}) validators.ValidationResult {
	m.called = true
	if m.failOn != "" {
		// Use simple substring search
		for i := 0; i <= len(content)-len(m.failOn); i++ {
			if content[i:i+len(m.failOn)] == m.failOn {
				return validators.ValidationResult{
					OK:      false,
					Details: map[string]interface{}{"found": m.failOn},
				}
			}
		}
	}
	return validators.ValidationResult{OK: true}
}

// mockStreamingValidator supports streaming validation
type mockStreamingValidator struct {
	mockValidator
}

func (m *mockStreamingValidator) ValidateChunk(chunk providers.StreamChunk, params ...map[string]interface{}) error {
	if m.failOn != "" {
		// Use simple substring search
		for i := 0; i <= len(chunk.Content)-len(m.failOn); i++ {
			if chunk.Content[i:i+len(m.failOn)] == m.failOn {
				return &providers.ValidationAbortError{
					Reason: "validation failed in streaming",
					Chunk:  chunk,
				}
			}
		}
	}
	return nil
}

func (m *mockStreamingValidator) SupportsStreaming() bool {
	return true
}

// mockProvider simulates an LLM response
type mockProviderMiddleware struct {
	responseContent string
	streaming       bool
}

func (m *mockProviderMiddleware) Before(execCtx *pipeline.ExecutionContext) error {
	if m.streaming && execCtx.StreamOutput != nil {
		// Stream the response
		go func() {
			defer close(execCtx.StreamOutput)
			execCtx.StreamOutput <- providers.StreamChunk{
				Delta:   m.responseContent,
				Content: m.responseContent,
			}
		}()
	} else {
		// Non-streaming response
		execCtx.Response = &pipeline.Response{
			Content: m.responseContent,
		}
	}
	return nil
}

func (m *mockProviderMiddleware) After(execCtx *pipeline.ExecutionContext) error {
	return nil
}

func (m *mockProviderMiddleware) StreamChunk(execCtx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	return nil
}

// mockProviderWithTools simulates an LLM response with tool usage
type mockProviderWithToolsMiddleware struct {
	content       string
	finalResponse string
}

func (m *mockProviderWithToolsMiddleware) Before(execCtx *pipeline.ExecutionContext) error {
	execCtx.Response = &pipeline.Response{
		Content:       m.content,
		FinalResponse: m.finalResponse,
	}
	return nil
}

func (m *mockProviderWithToolsMiddleware) After(execCtx *pipeline.ExecutionContext) error {
	return nil
}

func (m *mockProviderWithToolsMiddleware) StreamChunk(execCtx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	return nil
}

func TestDynamicValidatorMiddleware_NoValidators(t *testing.T) {
	registry := validators.NewRegistry()

	middleware := DynamicValidatorMiddleware(registry)

	execCtx := &pipeline.ExecutionContext{
		Context:  context.Background(),
		Metadata: make(map[string]interface{}),
	}

	err := middleware.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
}

func TestDynamicValidatorMiddleware_ValidationPass_NoTools(t *testing.T) {
	// Setup registry with mock validator
	mockVal := &mockValidator{failOn: "badword"}
	registry := validators.NewRegistry()
	registry.Register("mock", func(params map[string]interface{}) validators.Validator { return mockVal })

	// Setup validator configs
	validatorConfigs := []validators.ValidatorConfig{
		{
			Type:   "mock",
			Params: map[string]interface{}{},
		},
	}

	// Create execution context with validator configs
	execCtx := &pipeline.ExecutionContext{
		Context: context.Background(),
		Metadata: map[string]interface{}{
			"validator_configs": validatorConfigs,
		},
		Messages: []types.Message{
			{
				Role:    "assistant",
				Content: "Hello, this is a clean response",
			},
		},
	}

	// Create validator middleware
	validatorMW := DynamicValidatorMiddleware(registry)

	// Call Process - validator validates the existing message
	err := validatorMW.Process(execCtx, func() error {
		// next() just continues to the next middleware
		return nil
	})

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if !mockVal.called {
		t.Fatal("Expected validator to be called")
	}
}

func TestDynamicValidatorMiddleware_ValidationFail_NoTools(t *testing.T) {
	// Setup registry with mock validator that fails on "badword"
	mockVal := &mockValidator{failOn: "badword"}
	registry := validators.NewRegistry()
	registry.Register("mock", func(params map[string]interface{}) validators.Validator { return mockVal })

	// Setup validator configs
	validatorConfigs := []validators.ValidatorConfig{
		{
			Type:   "mock",
			Params: map[string]interface{}{},
		},
	}

	// Create execution context with validator configs
	execCtx := &pipeline.ExecutionContext{
		Context: context.Background(),
		Metadata: map[string]interface{}{
			"validator_configs": validatorConfigs,
		},
		Messages: []types.Message{},
	}

	// Simulate provider adding assistant message BEFORE validator runs
	execCtx.Messages = append(execCtx.Messages, types.Message{
		Role:    "assistant",
		Content: "This contains a badword in the response",
	})

	// Create middleware stack
	validatorMW := DynamicValidatorMiddleware(registry)

	// Call Process - validator validates the existing message
	err := validatorMW.Process(execCtx, func() error {
		// next() just continues to the next middleware
		return nil
	})

	if err == nil {
		t.Fatal("Expected validation error, got nil")
	}

	valErr, ok := err.(*pipeline.ValidationError)
	if !ok {
		t.Fatalf("Expected ValidationError, got: %T", err)
	}

	if valErr.Type == "" {
		t.Fatal("Expected ValidationError to have Type set")
	}

	if !mockVal.called {
		t.Fatal("Expected validator to be called")
	}
}

func TestDynamicValidatorMiddleware_ValidationWithTools_ValidatesFinalResponse(t *testing.T) {
	// Setup registry with mock validator
	mockVal := &mockValidator{failOn: "badword"}
	registry := validators.NewRegistry()
	registry.Register("mock", func(params map[string]interface{}) validators.Validator { return mockVal })

	// Setup validator configs
	validatorConfigs := []validators.ValidatorConfig{
		{
			Type:   "mock",
			Params: map[string]interface{}{},
		},
	}

	// Create execution context
	execCtx := &pipeline.ExecutionContext{
		Context: context.Background(),
		Metadata: map[string]interface{}{
			"validator_configs": validatorConfigs,
		},
		Messages: []types.Message{},
	}

	// Simulate provider adding assistant message BEFORE validator runs
	execCtx.Messages = append(execCtx.Messages, types.Message{
		Role:    "assistant",
		Content: "This is the final response with badword",
	})

	validatorMW := DynamicValidatorMiddleware(registry)

	// Call Process - validator validates the existing message
	err := validatorMW.Process(execCtx, func() error {
		// next() just continues to the next middleware
		return nil
	})

	if err == nil {
		t.Fatal("Expected validation error on FinalResponse, got nil")
	}

	valErr, ok := err.(*pipeline.ValidationError)
	if !ok {
		t.Fatalf("Expected ValidationError, got: %T", err)
	}

	if !mockVal.called {
		t.Fatal("Expected validator to be called")
	}

	t.Logf("Validation correctly failed with: %s", valErr.Details)
}

func TestDynamicValidatorMiddleware_ValidationWithTools_PassesWhenClean(t *testing.T) {
	// Setup registry with mock validator
	mockVal := &mockValidator{failOn: "badword"}
	registry := validators.NewRegistry()
	registry.Register("mock", func(params map[string]interface{}) validators.Validator { return mockVal })

	// Setup validator configs
	validatorConfigs := []validators.ValidatorConfig{
		{
			Type:   "mock",
			Params: map[string]interface{}{},
		},
	}

	// Create execution context
	execCtx := &pipeline.ExecutionContext{
		Context: context.Background(),
		Metadata: map[string]interface{}{
			"validator_configs": validatorConfigs,
		},
		Messages: []types.Message{},
	}

	// Simulate provider adding clean assistant message BEFORE validator runs
	execCtx.Messages = append(execCtx.Messages, types.Message{
		Role:    "assistant",
		Content: "This is a clean final response",
	})

	validatorMW := DynamicValidatorMiddleware(registry)

	// Call Process - validator validates the existing message
	err := validatorMW.Process(execCtx, func() error {
		// next() just continues to the next middleware
		return nil
	})

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if !mockVal.called {
		t.Fatal("Expected validator to be called")
	}
}

func TestDynamicValidatorMiddleware_MultipleValidators(t *testing.T) {
	// Setup registry with multiple mock validators
	mockVal1 := &mockValidator{failOn: "badword1"}
	mockVal2 := &mockValidator{failOn: "badword2"}
	registry := validators.NewRegistry()
	registry.Register("mock1", func(params map[string]interface{}) validators.Validator { return mockVal1 })
	registry.Register("mock2", func(params map[string]interface{}) validators.Validator { return mockVal2 })

	// Setup validator configs
	validatorConfigs := []validators.ValidatorConfig{
		{Type: "mock1", Params: map[string]interface{}{}},
		{Type: "mock2", Params: map[string]interface{}{}},
	}

	execCtx := &pipeline.ExecutionContext{
		Context: context.Background(),
		Metadata: map[string]interface{}{
			"validator_configs": validatorConfigs,
		},
		Messages: []types.Message{},
	}

	// Simulate provider adding clean assistant message BEFORE validator runs
	execCtx.Messages = append(execCtx.Messages, types.Message{
		Role:    "assistant",
		Content: "Clean response",
	})

	validatorMW := DynamicValidatorMiddleware(registry)

	// Test 1: Clean content passes all validators
	err := validatorMW.Process(execCtx, func() error {
		// next() just continues to the next middleware
		return nil
	})

	if err != nil {
		t.Fatalf("Expected no error with clean content, got: %v", err)
	}

	if !mockVal1.called || !mockVal2.called {
		t.Fatal("Expected both validators to be called")
	}

	// Reset called flags
	mockVal1.called = false
	mockVal2.called = false

	// Test 2: Content with badword1 fails first validator
	// Create new context for second test
	execCtx2 := &pipeline.ExecutionContext{
		Context: context.Background(),
		Metadata: map[string]interface{}{
			"validator_configs": validatorConfigs,
		},
		Messages: []types.Message{},
	}

	// Simulate provider adding assistant message with bad content BEFORE validator runs
	execCtx2.Messages = append(execCtx2.Messages, types.Message{
		Role:    "assistant",
		Content: "This has badword1",
	})

	// Call Process for second test
	err = validatorMW.Process(execCtx2, func() error {
		// next() just continues to the next middleware
		return nil
	})

	if err == nil {
		t.Fatal("Expected validation error, got nil")
	}

	if !mockVal1.called {
		t.Fatal("Expected first validator to be called")
	}
	// Note: mockVal2 might not be called if validation short-circuits on first failure
}

func TestDynamicValidatorMiddleware_UnknownValidatorType(t *testing.T) {
	registry := validators.NewRegistry()

	// Setup validator config with unknown type
	validatorConfigs := []validators.ValidatorConfig{
		{
			Type:   "unknown_validator",
			Params: map[string]interface{}{},
		},
	}

	execCtx := &pipeline.ExecutionContext{
		Context: context.Background(),
		Metadata: map[string]interface{}{
			"validator_configs": validatorConfigs,
		},
		Messages: []types.Message{},
	}

	// Simulate provider adding assistant message BEFORE validator runs
	execCtx.Messages = append(execCtx.Messages, types.Message{
		Role:    "assistant",
		Content: "Some response",
	})

	validatorMW := DynamicValidatorMiddleware(registry)

	// Call Process - validator validates the existing message
	err := validatorMW.Process(execCtx, func() error {
		// next() just continues to the next middleware
		return nil
	})

	// Should not error, just skip unknown validators
	if err != nil {
		t.Fatalf("Expected no error with unknown validator, got: %v", err)
	}
}

func TestDynamicValidatorMiddleware_EmptyResponse(t *testing.T) {
	mockVal := &mockValidator{failOn: "badword"}
	registry := validators.NewRegistry()
	registry.Register("mock", func(params map[string]interface{}) validators.Validator { return mockVal })

	validatorConfigs := []validators.ValidatorConfig{
		{Type: "mock", Params: map[string]interface{}{}},
	}

	execCtx := &pipeline.ExecutionContext{
		Context: context.Background(),
		Metadata: map[string]interface{}{
			"validator_configs": validatorConfigs,
		},
		Messages: []types.Message{},
	}

	// Simulate provider adding empty assistant message BEFORE validator runs
	execCtx.Messages = append(execCtx.Messages, types.Message{
		Role:    "assistant",
		Content: "",
	})

	validatorMW := DynamicValidatorMiddleware(registry)

	// Call Process - validator validates the existing message
	err := validatorMW.Process(execCtx, func() error {
		// next() just continues to the next middleware
		return nil
	})

	// Should skip validation on empty content
	if err != nil {
		t.Fatalf("Expected no error with empty content, got: %v", err)
	}

	// Validator should not be called for empty content
	if mockVal.called {
		t.Fatal("Expected validator to be skipped for empty content")
	}
}

func TestDynamicValidatorMiddleware_NilResponse(t *testing.T) {
	mockVal := &mockValidator{failOn: "badword"}
	registry := validators.NewRegistry()
	registry.Register("mock", func(params map[string]interface{}) validators.Validator { return mockVal })

	validatorConfigs := []validators.ValidatorConfig{
		{Type: "mock", Params: map[string]interface{}{}},
	}

	execCtx := &pipeline.ExecutionContext{
		Context: context.Background(),
		Metadata: map[string]interface{}{
			"validator_configs": validatorConfigs,
		},
		Messages: []types.Message{},
	}

	validatorMW := DynamicValidatorMiddleware(registry)

	// Call Process - provider doesn't add any message (nil response case)
	err := validatorMW.Process(execCtx, func() error {
		// Provider doesn't add any message (nil response case)
		return nil
	})

	// Should skip validation on nil response
	if err != nil {
		t.Fatalf("Expected no error with nil response, got: %v", err)
	}

	if mockVal.called {
		t.Fatal("Expected validator to be skipped for nil response")
	}
}

func TestDynamicValidatorMiddleware_StreamingValidation(t *testing.T) {
	// Setup registry with streaming validator
	registry := validators.NewRegistry()
	mockVal := &mockStreamingValidator{mockValidator{failOn: "forbidden"}}
	registry.Register("test_streaming", func(params map[string]interface{}) validators.Validator {
		return mockVal
	})

	// Setup validator configs
	validatorConfigs := []validators.ValidatorConfig{
		{Type: "test_streaming", Params: map[string]interface{}{}},
	}

	// Create execution context in streaming mode
	execCtx := &pipeline.ExecutionContext{
		Context:      context.Background(),
		StreamMode:   true,
		StreamOutput: make(chan providers.StreamChunk, 10),
		Metadata: map[string]interface{}{
			"validator_configs": validatorConfigs,
		},
		Messages: []types.Message{},
	}

	validatorMW := DynamicValidatorMiddleware(registry)

	// Test chunk validation - passing chunks (StreamChunk is called first during streaming)
	chunk1 := &providers.StreamChunk{
		Content: "Hello ",
		Delta:   "Hello ",
	}
	err := validatorMW.StreamChunk(execCtx, chunk1)
	if err != nil {
		t.Fatalf("StreamChunk() failed on valid chunk: %v", err)
	}

	// Verify validators were built and stored in metadata on first chunk
	storedValidators, ok := execCtx.Metadata["_validators"].([]validators.Validator)
	if !ok || len(storedValidators) == 0 {
		t.Fatal("Expected validators to be stored in metadata after first chunk")
	}

	chunk2 := &providers.StreamChunk{
		Content: "Hello world",
		Delta:   "world",
	}
	err = validatorMW.StreamChunk(execCtx, chunk2)
	if err != nil {
		t.Fatalf("StreamChunk() failed on valid chunk: %v", err)
	}

	// Test chunk validation - failing chunk
	chunk3 := &providers.StreamChunk{
		Content: "Hello world forbidden text",
		Delta:   " forbidden text",
	}
	err = validatorMW.StreamChunk(execCtx, chunk3)
	if err == nil {
		t.Fatal("Expected StreamChunk() to fail on forbidden content")
	}

	// Verify stream was interrupted
	if !execCtx.StreamInterrupted {
		t.Fatal("Expected stream to be interrupted after validation failure")
	}

	if execCtx.InterruptReason == "" {
		t.Fatal("Expected InterruptReason to be set")
	}

	// Verify validation results were stored
	results, ok := execCtx.Metadata["_streaming_validation_results"].([]types.ValidationResult)
	if !ok || len(results) == 0 {
		t.Fatal("Expected validation results to be stored in metadata")
	}

	// After streaming ends, Process() would be called to finalize validation
	// Add assistant message that would have been added by provider
	execCtx.Messages = append(execCtx.Messages, types.Message{
		Role:    "assistant",
		Content: "Hello world forbidden text",
	})

	// Call Process to finalize - should already have error from streaming
	// In a real scenario, next() would be empty since provider already executed during streaming
	err = validatorMW.Process(execCtx, func() error { return nil })

	// The validation error should be returned, and the message should have validations attached
	if err == nil {
		t.Fatal("Expected Process() to return validation error from streaming")
	}

	// Verify validations were attached to message
	if len(execCtx.Messages[0].Validations) == 0 {
		t.Error("Expected validations to be attached to message")
	}
}

func TestDynamicValidatorMiddleware_StreamingValidation_FinalChunk(t *testing.T) {
	// Setup registry with streaming validator that fails on "badword"
	registry := validators.NewRegistry()
	mockVal := &mockStreamingValidator{mockValidator{failOn: "badword"}}
	registry.Register("test_buffer", func(params map[string]interface{}) validators.Validator {
		return mockVal
	})

	// Setup validator configs
	validatorConfigs := []validators.ValidatorConfig{
		{Type: "test_buffer", Params: map[string]interface{}{}},
	}

	// Test 1: Successful validation - no forbidden content
	// Create execution context in streaming mode
	execCtx := &pipeline.ExecutionContext{
		Context:      context.Background(),
		StreamMode:   true,
		StreamOutput: make(chan providers.StreamChunk, 10),
		Metadata: map[string]interface{}{
			"validator_configs": validatorConfigs,
		},
		Messages: []types.Message{},
	}

	validatorMW := DynamicValidatorMiddleware(registry)

	// Process chunks (StreamChunk is called during streaming, before Process)
	chunk1 := &providers.StreamChunk{
		Content: "Hello ",
		Delta:   "Hello ",
	}
	err := validatorMW.StreamChunk(execCtx, chunk1)
	if err != nil {
		t.Fatalf("StreamChunk() failed: %v", err)
	}

	chunk2 := &providers.StreamChunk{
		Content: "Hello world",
		Delta:   "world",
	}
	err = validatorMW.StreamChunk(execCtx, chunk2)
	if err != nil {
		t.Fatalf("StreamChunk() failed: %v", err)
	}

	// Send final chunk with FinishReason set
	finishReason := "stop"
	finalChunk := &providers.StreamChunk{
		Content:      "Hello world",
		Delta:        "",
		FinishReason: &finishReason,
	}
	err = validatorMW.StreamChunk(execCtx, finalChunk)
	// Should pass - no "badword" in content
	if err != nil {
		t.Fatalf("StreamChunk() failed on final chunk: %v", err)
	}

	// After streaming completes, Process() is called to finalize
	// Provider adds the assistant message
	execCtx.Messages = append(execCtx.Messages, types.Message{
		Role:    "assistant",
		Content: "Hello world",
	})

	err = validatorMW.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("Process() failed: %v", err)
	}

	// Test 2: Failed validation with forbidden content
	execCtx2 := &pipeline.ExecutionContext{
		Context:      context.Background(),
		StreamMode:   true,
		StreamOutput: make(chan providers.StreamChunk, 10),
		Metadata: map[string]interface{}{
			"validator_configs": validatorConfigs,
		},
		Messages: []types.Message{},
	}

	// Process chunks with forbidden content (StreamChunk called during streaming)
	badChunk1 := &providers.StreamChunk{
		Content: "This contains ",
		Delta:   "This contains ",
	}
	err = validatorMW.StreamChunk(execCtx2, badChunk1)
	if err != nil {
		t.Fatalf("StreamChunk() failed: %v", err)
	}

	// Send chunk with forbidden word - should fail immediately
	badChunk2 := &providers.StreamChunk{
		Content: "This contains badword",
		Delta:   "badword",
	}
	err = validatorMW.StreamChunk(execCtx2, badChunk2)
	if err == nil {
		t.Fatal("Expected StreamChunk() to fail on chunk with forbidden content")
	}

	// Verify validation failure was marked
	if failed, ok := execCtx2.Metadata["_streaming_validation_failed"].(bool); !ok || !failed {
		t.Fatal("Expected validation failure to be marked in metadata")
	}

	// After streaming fails, Process() would still be called to finalize
	execCtx2.Messages = append(execCtx2.Messages, types.Message{
		Role:    "assistant",
		Content: "This contains badword",
	})

	err = validatorMW.Process(execCtx2, func() error { return nil })
	if err == nil {
		t.Fatal("Expected Process() to return validation error from streaming")
	}
}
