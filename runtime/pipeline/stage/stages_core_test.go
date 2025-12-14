package stage

import (
	"context"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper to create a test message element
func newTestMsgElement(role, content string) StreamElement {
	msg := &types.Message{
		Role:    role,
		Content: content,
	}
	return NewMessageElement(msg)
}

// Helper to run a stage and collect output
func runTestStage(t *testing.T, s Stage, inputs []StreamElement) []StreamElement {
	t.Helper()

	input := make(chan StreamElement, len(inputs))
	for _, elem := range inputs {
		input <- elem
	}
	close(input)

	output := make(chan StreamElement, 100)
	ctx := context.Background()

	err := s.Process(ctx, input, output)
	require.NoError(t, err)

	var results []StreamElement
	for elem := range output {
		results = append(results, elem)
	}
	return results
}

// =============================================================================
// PromptAssemblyStage Tests
// =============================================================================

func TestPromptAssemblyStage_NoRegistry(t *testing.T) {
	s := NewPromptAssemblyStage(nil, "chat", nil)

	inputs := []StreamElement{
		newTestMsgElement("user", "Hello"),
	}

	results := runTestStage(t, s, inputs)

	require.Len(t, results, 1)
	// Should use default system prompt
	assert.Equal(t, "You are a helpful AI assistant.", results[0].Metadata["system_prompt"])
}

func TestPromptAssemblyStage_WithVariables(t *testing.T) {
	s := NewPromptAssemblyStage(nil, "chat", map[string]string{
		"name": "John",
	})

	inputs := []StreamElement{
		newTestMsgElement("user", "Hello"),
	}

	results := runTestStage(t, s, inputs)

	require.Len(t, results, 1)
	baseVars := results[0].Metadata["base_variables"].(map[string]string)
	assert.Equal(t, "John", baseVars["name"])
}

func TestPromptAssemblyStage_EnrichesAllElements(t *testing.T) {
	s := NewPromptAssemblyStage(nil, "chat", nil)

	inputs := []StreamElement{
		newTestMsgElement("user", "Message 1"),
		newTestMsgElement("user", "Message 2"),
		newTestMsgElement("user", "Message 3"),
	}

	results := runTestStage(t, s, inputs)

	require.Len(t, results, 3)
	// All elements should have system_prompt
	for _, elem := range results {
		assert.Equal(t, "You are a helpful AI assistant.", elem.Metadata["system_prompt"])
	}
}

func TestPromptAssemblyStage_ContextCancellation(t *testing.T) {
	s := NewPromptAssemblyStage(nil, "chat", nil)

	input := make(chan StreamElement, 1)
	input <- newTestMsgElement("user", "Test")
	// Don't close input

	output := make(chan StreamElement, 10)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	// Should not hang
}

func TestPromptAssemblyStage_ExtractValidatorConfigs(t *testing.T) {
	s := NewPromptAssemblyStage(nil, "chat", nil)

	enabled := true
	disabled := false

	promptValidators := []prompt.ValidatorConfig{
		{
			ValidatorConfig: validators.ValidatorConfig{
				Type:   "type1",
				Params: map[string]interface{}{"key": "value"},
			},
			Enabled: &enabled,
		},
		{
			ValidatorConfig: validators.ValidatorConfig{
				Type: "type2",
			},
			Enabled: &disabled, // Disabled
		},
		{
			ValidatorConfig: validators.ValidatorConfig{
				Type: "type3",
			},
			// Enabled is nil (defaults to enabled)
		},
	}

	configs := s.extractValidatorConfigs(promptValidators)

	require.Len(t, configs, 2) // type1 and type3 (type2 is disabled)
	assert.Equal(t, "type1", configs[0].Type)
	assert.Equal(t, "type3", configs[1].Type)
}

// =============================================================================
// ValidationStage Tests
// =============================================================================

func TestValidationStage_NoValidators(t *testing.T) {
	registry := validators.NewRegistry()
	s := NewValidationStage(registry, false)

	inputs := []StreamElement{
		newTestMsgElement("user", "Hello"),
		newTestMsgElement("assistant", "Hi there!"),
	}

	results := runTestStage(t, s, inputs)

	require.Len(t, results, 2)
}

func TestValidationStage_PassingValidation(t *testing.T) {
	registry := validators.NewRegistry()
	registry.Register("always_pass", func(params map[string]interface{}) validators.Validator {
		return &passingValidator{}
	})

	s := NewValidationStage(registry, false)

	elem := newTestMsgElement("assistant", "Response content")
	elem.Metadata = map[string]interface{}{
		"validator_configs": []validators.ValidatorConfig{
			{Type: "always_pass"},
		},
	}

	results := runTestStage(t, s, []StreamElement{elem})

	require.Len(t, results, 1)
	// Check validation results attached
	validationResults, ok := results[0].Metadata["validation_results"].([]types.ValidationResult)
	require.True(t, ok)
	require.Len(t, validationResults, 1)
	assert.True(t, validationResults[0].Passed)
}

func TestValidationStage_FailingValidation(t *testing.T) {
	registry := validators.NewRegistry()
	registry.Register("always_fail", func(params map[string]interface{}) validators.Validator {
		return &failingValidator{}
	})

	s := NewValidationStage(registry, false) // Not suppressing exceptions

	elem := newTestMsgElement("assistant", "Response content")
	elem.Metadata = map[string]interface{}{
		"validator_configs": []validators.ValidatorConfig{
			{Type: "always_fail"},
		},
	}

	input := make(chan StreamElement, 1)
	input <- elem
	close(input)

	output := make(chan StreamElement, 10)
	ctx := context.Background()

	err := s.Process(ctx, input, output)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestValidationStage_SuppressedExceptions(t *testing.T) {
	registry := validators.NewRegistry()
	registry.Register("always_fail", func(params map[string]interface{}) validators.Validator {
		return &failingValidator{}
	})

	s := NewValidationStage(registry, true) // Suppressing exceptions

	elem := newTestMsgElement("assistant", "Response content")
	elem.Metadata = map[string]interface{}{
		"validator_configs": []validators.ValidatorConfig{
			{Type: "always_fail"},
		},
	}

	results := runTestStage(t, s, []StreamElement{elem})

	// Should complete without error
	require.Len(t, results, 1)
}

func TestValidationStage_EmptyContent(t *testing.T) {
	registry := validators.NewRegistry()
	registry.Register("should_not_run", func(params map[string]interface{}) validators.Validator {
		return &failingValidator{}
	})

	s := NewValidationStage(registry, false)

	elem := newTestMsgElement("assistant", "") // Empty content
	elem.Metadata = map[string]interface{}{
		"validator_configs": []validators.ValidatorConfig{
			{Type: "should_not_run"},
		},
	}

	results := runTestStage(t, s, []StreamElement{elem})

	// Should skip validation for empty content
	require.Len(t, results, 1)
}

func TestValidationStage_UnknownValidator(t *testing.T) {
	registry := validators.NewRegistry()
	// Don't register any validators

	s := NewValidationStage(registry, false)

	elem := newTestMsgElement("assistant", "Content")
	elem.Metadata = map[string]interface{}{
		"validator_configs": []validators.ValidatorConfig{
			{Type: "unknown_type"},
		},
	}

	results := runTestStage(t, s, []StreamElement{elem})

	// Should complete (unknown validators are skipped)
	require.Len(t, results, 1)
}

func TestValidationStage_NoAssistantMessage(t *testing.T) {
	registry := validators.NewRegistry()
	s := NewValidationStage(registry, false)

	inputs := []StreamElement{
		newTestMsgElement("user", "Hello"),
		newTestMsgElement("user", "World"),
	}

	results := runTestStage(t, s, inputs)

	// Should forward without validation
	require.Len(t, results, 2)
}

func TestValidationStage_AttachesToMessage(t *testing.T) {
	registry := validators.NewRegistry()
	registry.Register("pass", func(params map[string]interface{}) validators.Validator {
		return &passingValidator{}
	})

	s := NewValidationStage(registry, false)

	elem := newTestMsgElement("assistant", "Content")
	elem.Metadata = map[string]interface{}{
		"validator_configs": []validators.ValidatorConfig{
			{Type: "pass"},
		},
	}

	results := runTestStage(t, s, []StreamElement{elem})

	require.Len(t, results, 1)
	// Check validations attached to message
	require.NotNil(t, results[0].Message.Validations)
	assert.Len(t, results[0].Message.Validations, 1)
	assert.True(t, results[0].Message.Validations[0].Passed)
	assert.Equal(t, "pass", results[0].Message.Validations[0].ValidatorType)
}

// =============================================================================
// StateStoreLoadStage Tests
// =============================================================================

func TestStateStoreLoadStage_NilConfig(t *testing.T) {
	s := NewStateStoreLoadStage(nil)

	inputs := []StreamElement{
		newTestMsgElement("user", "Current message"),
	}

	results := runTestStage(t, s, inputs)

	require.Len(t, results, 1)
	assert.Equal(t, "Current message", results[0].Message.Content)
}

func TestStateStoreLoadStage_WithStore(t *testing.T) {
	store := statestore.NewMemoryStore()

	// Pre-populate state
	ctx := context.Background()
	_ = store.Save(ctx, &statestore.ConversationState{
		ID: "test-conv",
		Messages: []types.Message{
			{Role: "user", Content: "History 1"},
			{Role: "assistant", Content: "History 2"},
		},
	})

	config := &pipeline.StateStoreConfig{
		Store:          store,
		ConversationID: "test-conv",
	}

	s := NewStateStoreLoadStage(config)

	inputs := []StreamElement{
		newTestMsgElement("user", "Current message"),
	}

	results := runTestStage(t, s, inputs)

	// Should have history (2) + current (1) = 3 elements
	require.Len(t, results, 3)

	// First two should be from history
	assert.Equal(t, "History 1", results[0].Message.Content)
	assert.Equal(t, "statestore", results[0].Message.Source)
	assert.True(t, results[0].Metadata["from_history"].(bool))

	// Last should be current
	assert.Equal(t, "Current message", results[2].Message.Content)
}

func TestStateStoreLoadStage_StateNotFound(t *testing.T) {
	store := statestore.NewMemoryStore()

	config := &pipeline.StateStoreConfig{
		Store:          store,
		ConversationID: "nonexistent",
	}

	s := NewStateStoreLoadStage(config)

	inputs := []StreamElement{
		newTestMsgElement("user", "Current message"),
	}

	// Should not error on not found
	results := runTestStage(t, s, inputs)

	require.Len(t, results, 1)
	assert.Equal(t, "Current message", results[0].Message.Content)
}

func TestStateStoreLoadStage_AddsConversationMetadata(t *testing.T) {
	store := statestore.NewMemoryStore()

	config := &pipeline.StateStoreConfig{
		Store:          store,
		ConversationID: "conv-123",
		UserID:         "user-456",
	}

	s := NewStateStoreLoadStage(config)

	inputs := []StreamElement{
		newTestMsgElement("user", "Test"),
	}

	results := runTestStage(t, s, inputs)

	require.Len(t, results, 1)
	assert.Equal(t, "conv-123", results[0].Metadata["conversation_id"])
	assert.Equal(t, "user-456", results[0].Metadata["user_id"])
}

// =============================================================================
// StateStoreSaveStage Tests
// =============================================================================

func TestStateStoreSaveStage_NilConfig(t *testing.T) {
	s := NewStateStoreSaveStage(nil)

	inputs := []StreamElement{
		newTestMsgElement("user", "Message"),
	}

	results := runTestStage(t, s, inputs)

	// Should just forward
	require.Len(t, results, 1)
	assert.Equal(t, "Message", results[0].Message.Content)
}

func TestStateStoreSaveStage_SavesMessages(t *testing.T) {
	store := statestore.NewMemoryStore()

	config := &pipeline.StateStoreConfig{
		Store:          store,
		ConversationID: "save-test",
		UserID:         "user-1",
	}

	s := NewStateStoreSaveStage(config)

	inputs := []StreamElement{
		newTestMsgElement("user", "User message"),
		newTestMsgElement("assistant", "Assistant message"),
	}

	results := runTestStage(t, s, inputs)

	// Should forward all elements
	require.Len(t, results, 2)

	// Verify state was saved
	ctx := context.Background()
	state, err := store.Load(ctx, "save-test")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Len(t, state.Messages, 2)
	assert.Equal(t, "User message", state.Messages[0].Content)
	assert.Equal(t, "Assistant message", state.Messages[1].Content)
}

func TestStateStoreSaveStage_MergesMetadata(t *testing.T) {
	store := statestore.NewMemoryStore()

	config := &pipeline.StateStoreConfig{
		Store:          store,
		ConversationID: "metadata-test",
		Metadata: map[string]interface{}{
			"initial_key": "initial_value",
		},
	}

	s := NewStateStoreSaveStage(config)

	elem := newTestMsgElement("user", "Test")
	elem.Metadata = map[string]interface{}{
		"execution_key": "execution_value",
	}

	results := runTestStage(t, s, []StreamElement{elem})

	require.Len(t, results, 1)

	// Verify metadata was merged
	ctx := context.Background()
	state, err := store.Load(ctx, "metadata-test")
	require.NoError(t, err)
	assert.Equal(t, "initial_value", state.Metadata["initial_key"])
	assert.Equal(t, "execution_value", state.Metadata["execution_key"])
}

func TestStateStoreSaveStage_UpdatesExistingState(t *testing.T) {
	store := statestore.NewMemoryStore()
	ctx := context.Background()

	// Create initial state
	_ = store.Save(ctx, &statestore.ConversationState{
		ID: "update-test",
		Messages: []types.Message{
			{Role: "user", Content: "Old message"},
		},
		Metadata: map[string]interface{}{
			"old_key": "old_value",
		},
	})

	config := &pipeline.StateStoreConfig{
		Store:          store,
		ConversationID: "update-test",
	}

	s := NewStateStoreSaveStage(config)

	inputs := []StreamElement{
		newTestMsgElement("user", "New message"),
	}

	_ = runTestStage(t, s, inputs)

	// Verify state was updated
	state, err := store.Load(ctx, "update-test")
	require.NoError(t, err)
	assert.Len(t, state.Messages, 1)
	assert.Equal(t, "New message", state.Messages[0].Content)
	// Old metadata should be preserved
	assert.Equal(t, "old_value", state.Metadata["old_key"])
}

// =============================================================================
// Helper Functions Tests
// =============================================================================

func TestHasValidationFailures(t *testing.T) {
	tests := []struct {
		name     string
		results  []types.ValidationResult
		expected bool
	}{
		{
			name:     "empty results",
			results:  []types.ValidationResult{},
			expected: false,
		},
		{
			name: "all passing",
			results: []types.ValidationResult{
				{Passed: true},
				{Passed: true},
			},
			expected: false,
		},
		{
			name: "one failing",
			results: []types.ValidationResult{
				{Passed: true},
				{Passed: false},
			},
			expected: true,
		},
		{
			name: "all failing",
			results: []types.ValidationResult{
				{Passed: false},
				{Passed: false},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasValidationFailures(tt.results)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCountFailures(t *testing.T) {
	tests := []struct {
		name     string
		results  []types.ValidationResult
		expected int
	}{
		{
			name:     "empty results",
			results:  []types.ValidationResult{},
			expected: 0,
		},
		{
			name: "all passing",
			results: []types.ValidationResult{
				{Passed: true},
				{Passed: true},
			},
			expected: 0,
		},
		{
			name: "mixed",
			results: []types.ValidationResult{
				{Passed: true},
				{Passed: false},
				{Passed: false},
			},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := countFailures(tt.results)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// Test Validators
// =============================================================================

type passingValidator struct{}

func (v *passingValidator) Validate(content string, params map[string]interface{}) validators.ValidationResult {
	return validators.ValidationResult{
		Passed: true,
		Details: map[string]interface{}{
			"status": "passed",
		},
	}
}

type failingValidator struct{}

func (v *failingValidator) Validate(content string, params map[string]interface{}) validators.ValidationResult {
	return validators.ValidationResult{
		Passed: false,
		Details: map[string]interface{}{
			"status": "failed",
			"reason": "always fails",
		},
	}
}

// =============================================================================
// Invalid Store Type Tests
// =============================================================================

type invalidStore struct{}

func TestStateStoreLoadStage_InvalidStoreType(t *testing.T) {
	config := &pipeline.StateStoreConfig{
		Store:          &invalidStore{},
		ConversationID: "test",
	}

	s := NewStateStoreLoadStage(config)

	input := make(chan StreamElement)
	close(input)

	output := make(chan StreamElement, 10)
	ctx := context.Background()

	err := s.Process(ctx, input, output)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid store type")
}

func TestStateStoreSaveStage_InvalidStoreType(t *testing.T) {
	config := &pipeline.StateStoreConfig{
		Store:          &invalidStore{},
		ConversationID: "test",
	}

	s := NewStateStoreSaveStage(config)

	inputs := []StreamElement{
		newTestMsgElement("user", "Test"),
	}

	input := make(chan StreamElement, len(inputs))
	for _, elem := range inputs {
		input <- elem
	}
	close(input)

	output := make(chan StreamElement, 10)
	ctx := context.Background()

	err := s.Process(ctx, input, output)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid store type")
}

// =============================================================================
// Error Store for Testing
// =============================================================================

type errorStore struct {
	statestore.Store
	loadErr error
	saveErr error
}

func (s *errorStore) Load(ctx context.Context, id string) (*statestore.ConversationState, error) {
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	return nil, statestore.ErrNotFound
}

func (s *errorStore) Save(ctx context.Context, state *statestore.ConversationState) error {
	return s.saveErr
}

func TestStateStoreLoadStage_LoadError(t *testing.T) {
	store := &errorStore{loadErr: errors.New("load failed")}

	config := &pipeline.StateStoreConfig{
		Store:          store,
		ConversationID: "test",
	}

	s := NewStateStoreLoadStage(config)

	input := make(chan StreamElement)
	close(input)

	output := make(chan StreamElement, 10)
	ctx := context.Background()

	err := s.Process(ctx, input, output)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "load failed")
}

func TestStateStoreSaveStage_SaveError(t *testing.T) {
	store := &errorStore{saveErr: errors.New("save failed")}

	config := &pipeline.StateStoreConfig{
		Store:          store,
		ConversationID: "test",
	}

	s := NewStateStoreSaveStage(config)

	inputs := []StreamElement{
		newTestMsgElement("user", "Test"),
	}

	input := make(chan StreamElement, len(inputs))
	for _, elem := range inputs {
		input <- elem
	}
	close(input)

	output := make(chan StreamElement, 10)
	ctx := context.Background()

	err := s.Process(ctx, input, output)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "save failed")
}
