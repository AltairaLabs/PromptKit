package stage

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockRepository implements prompt.Repository for testing.
type mockRepository struct {
	prompts map[string]*prompt.Config
}

func newMockRepo() *mockRepository {
	return &mockRepository{prompts: make(map[string]*prompt.Config)}
}

func (m *mockRepository) LoadPrompt(taskType string) (*prompt.Config, error) {
	if cfg, ok := m.prompts[taskType]; ok {
		return cfg, nil
	}
	return nil, fmt.Errorf("prompt not found: %s", taskType)
}

func (m *mockRepository) LoadFragment(string, string, string) (*prompt.Fragment, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockRepository) ListPrompts() ([]string, error) {
	keys := make([]string, 0, len(m.prompts))
	for k := range m.prompts {
		keys = append(keys, k)
	}
	return keys, nil
}

func (m *mockRepository) SavePrompt(config *prompt.Config) error {
	m.prompts[config.Spec.TaskType] = config
	return nil
}

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

func TestPromptAssemblyStage_WithRegistry(t *testing.T) {
	// Create a registry with a mock repository and a prompt that has validators
	repo := newMockRepo()
	registry := prompt.NewRegistryWithRepository(repo)
	enabled := true
	cfg := &prompt.Config{
		Spec: prompt.Spec{
			TaskType:       "chat",
			SystemTemplate: "You are a test bot.",
			Validators: []prompt.ValidatorConfig{
				{
					Type:    "banned_words",
					Params:  map[string]interface{}{"words": []string{"bad"}},
					Enabled: &enabled,
				},
			},
		},
	}
	err := registry.RegisterConfig("chat", cfg)
	require.NoError(t, err)

	s := NewPromptAssemblyStage(registry, "chat", nil)

	inputs := []StreamElement{
		newTestMsgElement("user", "Hello"),
	}
	results := runTestStage(t, s, inputs)

	require.Len(t, results, 1)
	assert.Equal(t, "You are a test bot.", results[0].Metadata["system_prompt"])
	// Validator configs should be passed through
	assert.NotNil(t, results[0].Metadata["validator_configs"])
}

func TestPromptAssemblyStage_RegistryMissing(t *testing.T) {
	// Test the path where registry has no prompt for the task type
	repo := newMockRepo()
	registry := prompt.NewRegistryWithRepository(repo)

	s := NewPromptAssemblyStage(registry, "nonexistent", nil)

	inputs := []StreamElement{
		newTestMsgElement("user", "Hello"),
	}
	results := runTestStage(t, s, inputs)

	require.Len(t, results, 1)
	// Should fall back to default system prompt
	assert.Equal(t, "You are a helpful AI assistant.", results[0].Metadata["system_prompt"])
}

func TestPromptAssemblyStage_ExtractValidatorConfigs(t *testing.T) {
	s := NewPromptAssemblyStage(nil, "chat", nil)

	enabled := true
	disabled := false

	promptValidators := []prompt.ValidatorConfig{
		{
			Type:    "type1",
			Params:  map[string]interface{}{"key": "value"},
			Enabled: &enabled,
		},
		{
			Type:    "type2",
			Enabled: &disabled, // Disabled
		},
		{
			Type: "type3",
			// Enabled is nil (defaults to enabled)
		},
	}

	configs := s.extractValidatorConfigs(promptValidators)

	require.Len(t, configs, 2) // type1 and type3 (type2 is disabled)
	assert.Equal(t, "type1", configs[0].Type)
	assert.Equal(t, "type3", configs[1].Type)
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
