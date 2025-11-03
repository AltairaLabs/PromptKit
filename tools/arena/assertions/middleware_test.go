package assertions

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

func TestArenaAssertionMiddleware_NoAssertions(t *testing.T) {
	registry := NewArenaAssertionRegistry()
	mw := ArenaAssertionMiddleware(registry, []AssertionConfig{})

	execCtx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "Test"},
			{Role: "assistant", Content: "Test response"},
		},
	}

	nextCalled := false
	next := func() error {
		nextCalled = true
		return nil
	}

	err := mw.Process(execCtx, next)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	// Middleware must always call next(), even when no assertions
	if !nextCalled {
		t.Fatal("Expected next() to be called even when no assertions configured")
	}
}

func TestArenaAssertionMiddleware_ToolsCalled_Pass(t *testing.T) {
	registry := NewArenaAssertionRegistry()
	assertions := []AssertionConfig{
		{
			Type: "tools_called",
			Params: map[string]interface{}{
				"tools": []string{"search", "calculate"},
			},
			Message: "test message",
		},
	}
	mw := ArenaAssertionMiddleware(registry, assertions)

	execCtx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "Search and calculate"},
			{
				Role:    "assistant",
				Content: "Searching and calculating...",
				ToolCalls: []types.MessageToolCall{
					{ID: "1", Name: "search", Args: nil},
					{ID: "2", Name: "calculate", Args: nil},
				},
			},
		},
	}

	nextCalled := false
	next := func() error {
		nextCalled = true
		return nil
	}

	err := mw.Process(execCtx, next)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !nextCalled {
		t.Fatal("Expected next() to be called")
	}

	// Check that assertion results are attached to message
	lastMsg := execCtx.Messages[len(execCtx.Messages)-1]
	if lastMsg.Meta == nil || lastMsg.Meta["assertions"] == nil {
		t.Fatal("Expected assertions to be attached to message meta")
	}
}

func TestArenaAssertionMiddleware_ToolsCalled_Fail(t *testing.T) {
	registry := NewArenaAssertionRegistry()
	assertions := []AssertionConfig{
		{
			Type: "tools_called",
			Params: map[string]interface{}{
				"tools": []string{"search", "calculate", "missing_tool"},
			},
			Message: "test message",
		},
	}
	mw := ArenaAssertionMiddleware(registry, assertions)

	execCtx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "Search and calculate"},
			{
				Role:    "assistant",
				Content: "Searching and calculating...",
				ToolCalls: []types.MessageToolCall{
					{ID: "1", Name: "search", Args: nil},
					{ID: "2", Name: "calculate", Args: nil},
				},
			},
		},
	}

	nextCalled := false
	next := func() error {
		nextCalled = true
		return nil
	}

	err := mw.Process(execCtx, next)
	if err == nil {
		t.Fatal("Expected error for missing tool, got nil")
	}
	if !nextCalled {
		t.Fatal("Expected next() to be called even when assertion fails")
	}
}

func TestArenaAssertionMiddleware_ContentIncludes_Pass(t *testing.T) {
	registry := NewArenaAssertionRegistry()
	assertions := []AssertionConfig{
		{
			Type: "content_includes",
			Params: map[string]interface{}{
				"patterns": []string{"hello", "world"},
			},
			Message: "test message",
		},
	}
	mw := ArenaAssertionMiddleware(registry, assertions)

	execCtx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "Say hello"},
			{Role: "assistant", Content: "Hello world, how are you?"},
		},
	}

	nextCalled := false
	next := func() error {
		nextCalled = true
		return nil
	}

	err := mw.Process(execCtx, next)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !nextCalled {
		t.Fatal("Expected next() to be called")
	}
}

func TestArenaAssertionMiddleware_ContentIncludes_Fail(t *testing.T) {
	registry := NewArenaAssertionRegistry()
	assertions := []AssertionConfig{
		{
			Type: "content_includes",
			Params: map[string]interface{}{
				"patterns": []string{"missing_pattern"},
			},
			Message: "test message",
		},
	}
	mw := ArenaAssertionMiddleware(registry, assertions)

	execCtx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "Say hello"},
			{Role: "assistant", Content: "Hello world"},
		},
	}

	nextCalled := false
	next := func() error {
		nextCalled = true
		return nil
	}

	err := mw.Process(execCtx, next)
	if err == nil {
		t.Fatal("Expected error for missing pattern, got nil")
	}
	if !nextCalled {
		t.Fatal("Expected next() to be called even when assertion fails")
	}
}

func TestArenaAssertionMiddleware_MultipleAssertions(t *testing.T) {
	registry := NewArenaAssertionRegistry()
	assertions := []AssertionConfig{
		{
			Type: "tools_called",
			Params: map[string]interface{}{
				"tools": []string{"search"},
			},
			Message: "test message",
		},
		{
			Type: "content_includes",
			Params: map[string]interface{}{
				"patterns": []string{"result"},
			},
		},
	}
	mw := ArenaAssertionMiddleware(registry, assertions)

	execCtx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "Search something"},
			{
				Role:    "assistant",
				Content: "Here is the result from search",
				ToolCalls: []types.MessageToolCall{
					{ID: "1", Name: "search", Args: nil},
				},
			},
		},
	}

	nextCalled := false
	next := func() error {
		nextCalled = true
		return nil
	}

	err := mw.Process(execCtx, next)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !nextCalled {
		t.Fatal("Expected next() to be called")
	}

	// Check that both assertion results are attached
	lastMsg := execCtx.Messages[len(execCtx.Messages)-1]
	assertionResults, ok := lastMsg.Meta["assertions"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected assertions map in meta")
	}

	if len(assertionResults) != 2 {
		t.Fatalf("Expected 2 assertion results, got %d", len(assertionResults))
	}
}

func TestArenaAssertionMiddleware_InvalidValidatorType(t *testing.T) {
	registry := NewArenaAssertionRegistry()
	assertions := []AssertionConfig{
		{
			Type:    "invalid_validator_type",
			Params:  map[string]interface{}{},
			Message: "test message",
		},
	}
	mw := ArenaAssertionMiddleware(registry, assertions)

	execCtx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "Test"},
			{Role: "assistant", Content: "Test response"},
		},
	}

	nextCalled := false
	next := func() error {
		nextCalled = true
		return nil
	}

	err := mw.Process(execCtx, next)
	// Invalid validator type now logs warning and continues (no error)
	if err != nil {
		t.Fatalf("Expected no error for invalid validator type, got %v", err)
	}
	if !nextCalled {
		t.Fatal("Expected next() to be called")
	}
}

func TestArenaAssertionMiddleware_NoAssistantMessage(t *testing.T) {
	registry := NewArenaAssertionRegistry()
	assertions := []AssertionConfig{
		{
			Type: "content_includes",
			Params: map[string]interface{}{
				"patterns": []string{"test"},
			},
			Message: "test message",
		},
	}
	mw := ArenaAssertionMiddleware(registry, assertions)

	execCtx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "Test"},
		},
	}

	nextCalled := false
	next := func() error {
		nextCalled = true
		return nil
	}

	err := mw.Process(execCtx, next)
	// No assistant message now logs debug and continues (no error)
	if err != nil {
		t.Fatalf("Expected no error when no assistant message found, got %v", err)
	}
	if !nextCalled {
		t.Fatal("Expected next() to be called")
	}
}

func TestArenaAssertionMiddleware_StreamChunk_NoOp(t *testing.T) {
	registry := NewArenaAssertionRegistry()
	assertions := []AssertionConfig{
		{
			Type: "content_includes",
			Params: map[string]interface{}{
				"patterns": []string{"test"},
			},
			Message: "test message",
		},
	}
	mw := ArenaAssertionMiddleware(registry, assertions)

	execCtx := &pipeline.ExecutionContext{
		Messages: []types.Message{},
	}

	// StreamChunk should be a no-op
	err := mw.StreamChunk(execCtx, nil)
	if err != nil {
		t.Fatalf("Expected no error from StreamChunk, got %v", err)
	}
}

func TestArenaAssertionMiddleware_ContextInjection(t *testing.T) {
	registry := NewArenaAssertionRegistry()

	// Create a custom validator to capture params
	var capturedParams map[string]interface{}
	registry.Register("test_context_validator", func(params map[string]interface{}) runtimeValidators.Validator {
		return &testContextValidator{capturedParams: &capturedParams}
	})

	assertions := []AssertionConfig{
		{
			Type:    "test_context_validator",
			Params:  map[string]interface{}{"custom": "value"},
			Message: "test message",
		},
	}
	mw := ArenaAssertionMiddleware(registry, assertions)

	execCtx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "Test"},
			{
				Role:    "assistant",
				Content: "Response",
				ToolCalls: []types.MessageToolCall{
					{ID: "1", Name: "test_tool", Args: nil},
				},
			},
		},
	}

	nextCalled := false
	next := func() error {
		nextCalled = true
		return nil
	}

	err := mw.Process(execCtx, next)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !nextCalled {
		t.Fatal("Expected next() to be called")
	}

	// Verify that context was injected
	if capturedParams["_turn_messages"] == nil {
		t.Error("Expected _turn_messages to be injected")
	}
	if capturedParams["_execution_context_messages"] == nil {
		t.Error("Expected _execution_context_messages to be injected")
	}
	if capturedParams["custom"] != "value" {
		t.Error("Expected custom param to be preserved")
	}

	// Verify _turn_messages is a deep clone (not the same slice)
	turnMessages, ok := capturedParams["_turn_messages"].([]types.Message)
	if !ok {
		t.Fatal("Expected _turn_messages to be []types.Message")
	}
	if len(turnMessages) != 2 {
		t.Errorf("Expected 2 messages in turn, got %d", len(turnMessages))
	}
}

func TestArenaAssertionMiddleware_SourceFieldFiltering(t *testing.T) {
	registry := NewArenaAssertionRegistry()

	// Create a custom validator to capture turn messages
	var capturedTurnMessages []types.Message
	registry.Register("test_capture_validator", func(params map[string]interface{}) runtimeValidators.Validator {
		return &testCaptureValidator{capturedMessages: &capturedTurnMessages}
	})

	assertions := []AssertionConfig{
		{
			Type:    "test_capture_validator",
			Params:  map[string]interface{}{},
			Message: "test message",
		},
	}
	mw := ArenaAssertionMiddleware(registry, assertions)

	execCtx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			// Historical messages (loaded from StateStore)
			{Role: "user", Content: "Previous question", Source: "statestore"},
			{Role: "assistant", Content: "Previous answer", Source: "statestore"},
			// Current turn messages (new)
			{Role: "user", Content: "Current question", Source: ""}, // User input
			{Role: "assistant", Content: "Current answer", Source: "pipeline"},
		},
	}

	nextCalled := false
	next := func() error {
		nextCalled = true
		return nil
	}

	err := mw.Process(execCtx, next)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !nextCalled {
		t.Fatal("Expected next() to be called")
	}

	// Verify that only current turn messages were passed to validator
	if len(capturedTurnMessages) != 2 {
		t.Fatalf("Expected 2 current turn messages, got %d", len(capturedTurnMessages))
	}

	// Verify the messages are from current turn (not from statestore)
	for i, msg := range capturedTurnMessages {
		if msg.Source == "statestore" {
			t.Errorf("Message %d should not have Source='statestore', got %q", i, msg.Source)
		}
	}

	// Verify correct content
	if capturedTurnMessages[0].Content != "Current question" {
		t.Errorf("Expected first turn message to be 'Current question', got %q", capturedTurnMessages[0].Content)
	}
	if capturedTurnMessages[1].Content != "Current answer" {
		t.Errorf("Expected second turn message to be 'Current answer', got %q", capturedTurnMessages[1].Content)
	}
}

// testCaptureValidator captures turn messages for testing
type testCaptureValidator struct {
	capturedMessages *[]types.Message
}

func (v *testCaptureValidator) Validate(content string, params map[string]interface{}) runtimeValidators.ValidationResult {
	if turnMessages, ok := params["_turn_messages"].([]types.Message); ok {
		*v.capturedMessages = turnMessages
	}
	return runtimeValidators.ValidationResult{Passed: true}
}

// testContextValidator is a test helper
type testContextValidator struct {
	capturedParams *map[string]interface{}
}

func (v *testContextValidator) Validate(content string, params map[string]interface{}) runtimeValidators.ValidationResult {
	*v.capturedParams = params
	return runtimeValidators.ValidationResult{Passed: true}
}
