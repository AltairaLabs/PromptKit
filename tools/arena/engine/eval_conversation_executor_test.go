package engine

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
	"github.com/AltairaLabs/PromptKit/tools/arena/adapters"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)

func TestNewEvalConversationExecutor(t *testing.T) {
	adapterReg := adapters.NewRegistry()
	assertionReg := assertions.NewArenaAssertionRegistry()
	convAssertionReg := assertions.NewConversationAssertionRegistry()
	promptReg := prompt.NewRegistryWithRepository(memory.NewPromptRepository())
	providerReg := providers.NewRegistry()

	executor := NewEvalConversationExecutor(
		adapterReg,
		assertionReg,
		convAssertionReg,
		promptReg,
		providerReg,
		nil,
	)

	if executor == nil {
		t.Fatal("Expected non-nil executor")
	}
	if executor.adapterRegistry != adapterReg {
		t.Error("Adapter registry not set correctly")
	}
	if executor.assertionRegistry != assertionReg {
		t.Error("Assertion registry not set correctly")
	}
	if executor.convAssertionReg != convAssertionReg {
		t.Error("Conversation assertion registry not set correctly")
	}
	if executor.promptRegistry != promptReg {
		t.Error("Prompt registry not set correctly")
	}
	if executor.providerRegistry != providerReg {
		t.Error("Provider registry not set correctly")
	}
}

func TestEvalConversationExecutor_ValidatesEvalConfig(t *testing.T) {
	executor := NewEvalConversationExecutor(
		adapters.NewRegistry(),
		assertions.NewArenaAssertionRegistry(),
		assertions.NewConversationAssertionRegistry(),
		prompt.NewRegistryWithRepository(memory.NewPromptRepository()),
		providers.NewRegistry(),
		nil,
	)

	tests := []struct {
		name        string
		eval        *config.Eval
		shouldFail  bool
		errorSubstr string
	}{
		{
			name:        "nil eval config",
			eval:        nil,
			shouldFail:  true,
			errorSubstr: "eval configuration is required",
		},
		{
			name: "missing recording path",
			eval: &config.Eval{
				ID: "test",
				Recording: config.RecordingSource{
					Path: "",
				},
			},
			shouldFail:  true,
			errorSubstr: "recording path is required",
		},
		{
			name: "valid eval config",
			eval: &config.Eval{
				ID: "test",
				Recording: config.RecordingSource{
					Path: "test.json",
					Type: "session",
				},
			},
			shouldFail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := ConversationRequest{
				Eval: tt.eval,
			}

			result := executor.ExecuteConversation(context.Background(), req)

			if tt.shouldFail {
				if !result.Failed {
					t.Errorf("Expected failure for %s", tt.name)
				}
				if result.Error == "" {
					t.Error("Expected error message")
				}
				if tt.errorSubstr != "" && result.Error != "" {
					// Check that error contains expected substring
					found := false
					if len(result.Error) >= len(tt.errorSubstr) {
						for i := 0; i <= len(result.Error)-len(tt.errorSubstr); i++ {
							if result.Error[i:i+len(tt.errorSubstr)] == tt.errorSubstr {
								found = true
								break
							}
						}
					}
					if !found {
						t.Errorf("Expected error to contain %q, got %q", tt.errorSubstr, result.Error)
					}
				}
			} else if result.Failed && result.Error != "" {
				// For valid config, we expect failure from missing file/adapter
				// Accept either "no adapter" or "failed to load recording"
				hasExpectedError := false
				for _, expectedMsg := range []string{"no adapter", "failed to load recording"} {
					if len(result.Error) >= len(expectedMsg) {
						for i := 0; i <= len(result.Error)-len(expectedMsg); i++ {
							if result.Error[i:i+len(expectedMsg)] == expectedMsg {
								hasExpectedError = true
								break
							}
						}
					}
					if hasExpectedError {
						break
					}
				}
				if !hasExpectedError {
					t.Errorf("Expected adapter/load error for valid config, got: %s", result.Error)
				}
			}
		})
	}
}

func TestEvalConversationExecutor_RequiresAdapterRegistry(t *testing.T) {
	executor := NewEvalConversationExecutor(
		nil, // nil adapter registry
		assertions.NewArenaAssertionRegistry(),
		assertions.NewConversationAssertionRegistry(),
		prompt.NewRegistryWithRepository(memory.NewPromptRepository()),
		providers.NewRegistry(),
		nil,
	)

	req := ConversationRequest{
		Eval: &config.Eval{
			ID: "test",
			Recording: config.RecordingSource{
				Path: "test.json",
			},
		},
	}

	result := executor.ExecuteConversation(context.Background(), req)

	if !result.Failed {
		t.Error("Expected failure when adapter registry is nil")
	}
	if result.Error == "" {
		t.Error("Expected error message")
	}
}

func TestEvalConversationExecutor_NoAdapterFound(t *testing.T) {
	// Create empty registry (no adapters)
	emptyRegistry := &adapters.Registry{}

	executor := NewEvalConversationExecutor(
		emptyRegistry,
		assertions.NewArenaAssertionRegistry(),
		assertions.NewConversationAssertionRegistry(),
		prompt.NewRegistryWithRepository(memory.NewPromptRepository()),
		providers.NewRegistry(),
		nil,
	)

	req := ConversationRequest{
		Eval: &config.Eval{
			ID: "test",
			Recording: config.RecordingSource{
				Path: "nonexistent.json",
			},
		},
	}

	result := executor.ExecuteConversation(context.Background(), req)

	if !result.Failed {
		t.Error("Expected failure when no adapter can handle the recording")
	}
	if result.Error == "" {
		t.Error("Expected error message")
	}
}

func TestEvalConversationExecutor_CalculateCost(t *testing.T) {
	executor := NewEvalConversationExecutor(
		adapters.NewRegistry(),
		assertions.NewArenaAssertionRegistry(),
		assertions.NewConversationAssertionRegistry(),
		prompt.NewRegistryWithRepository(memory.NewPromptRepository()),
		providers.NewRegistry(),
		nil,
	)

	tests := []struct {
		name     string
		messages []types.Message
		expected types.CostInfo
	}{
		{
			name:     "no messages",
			messages: []types.Message{},
			expected: types.CostInfo{},
		},
		{
			name: "messages without cost metadata",
			messages: []types.Message{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi"},
			},
			expected: types.CostInfo{},
		},
		{
			name: "messages with cost metadata",
			messages: []types.Message{
				{Role: "user", Content: "hello"},
				{
					Role:    "assistant",
					Content: "hi",
					Meta: map[string]interface{}{
						"cost": types.CostInfo{
							TotalCost:    0.01,
							InputTokens:  10,
							OutputTokens: 20,
						},
					},
				},
				{
					Role:    "assistant",
					Content: "there",
					Meta: map[string]interface{}{
						"cost": types.CostInfo{
							TotalCost:    0.02,
							InputTokens:  15,
							OutputTokens: 25,
							CachedTokens: 5,
						},
					},
				},
			},
			expected: types.CostInfo{
				TotalCost:    0.03,
				InputTokens:  25,
				OutputTokens: 45,
				CachedTokens: 5,
			},
		},
		{
			name: "ignores non-assistant messages",
			messages: []types.Message{
				{
					Role:    "user",
					Content: "hello",
					Meta: map[string]interface{}{
						"cost": types.CostInfo{
							TotalCost: 999.99, // Should be ignored
						},
					},
				},
				{
					Role:    "assistant",
					Content: "hi",
					Meta: map[string]interface{}{
						"cost": types.CostInfo{
							TotalCost:    0.01,
							InputTokens:  10,
							OutputTokens: 20,
						},
					},
				},
			},
			expected: types.CostInfo{
				TotalCost:    0.01,
				InputTokens:  10,
				OutputTokens: 20,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := executor.calculateCost(tt.messages)

			if result.TotalCost != tt.expected.TotalCost {
				t.Errorf("TotalCost: expected %v, got %v", tt.expected.TotalCost, result.TotalCost)
			}
			if result.InputTokens != tt.expected.InputTokens {
				t.Errorf("InputTokens: expected %v, got %v", tt.expected.InputTokens, result.InputTokens)
			}
			if result.OutputTokens != tt.expected.OutputTokens {
				t.Errorf("OutputTokens: expected %v, got %v", tt.expected.OutputTokens, result.OutputTokens)
			}
			if result.CachedTokens != tt.expected.CachedTokens {
				t.Errorf("CachedTokens: expected %v, got %v", tt.expected.CachedTokens, result.CachedTokens)
			}
		})
	}
}

func TestEvalConversationExecutor_HasFailedAssertions(t *testing.T) {
	executor := NewEvalConversationExecutor(
		adapters.NewRegistry(),
		assertions.NewArenaAssertionRegistry(),
		assertions.NewConversationAssertionRegistry(),
		prompt.NewRegistryWithRepository(memory.NewPromptRepository()),
		providers.NewRegistry(),
		nil,
	)

	tests := []struct {
		name        string
		messages    []types.Message
		convResults []assertions.ConversationValidationResult
		expected    bool
	}{
		{
			name:        "no assertions",
			messages:    []types.Message{},
			convResults: nil,
			expected:    false,
		},
		{
			name: "all turn assertions pass",
			messages: []types.Message{
				{
					Role:    "assistant",
					Content: "hi",
					Meta: map[string]interface{}{
						"assertions": []assertions.AssertionResult{
							{Passed: true},
							{Passed: true},
						},
					},
				},
			},
			convResults: nil,
			expected:    false,
		},
		{
			name: "one turn assertion fails",
			messages: []types.Message{
				{
					Role:    "assistant",
					Content: "hi",
					Meta: map[string]interface{}{
						"assertions": []assertions.AssertionResult{
							{Passed: true},
							{Passed: false},
						},
					},
				},
			},
			convResults: nil,
			expected:    true,
		},
		{
			name:     "all conversation assertions pass",
			messages: []types.Message{},
			convResults: []assertions.ConversationValidationResult{
				{Passed: true},
				{Passed: true},
			},
			expected: false,
		},
		{
			name:     "one conversation assertion fails",
			messages: []types.Message{},
			convResults: []assertions.ConversationValidationResult{
				{Passed: true},
				{Passed: false},
			},
			expected: true,
		},
		{
			name: "mixed - all pass",
			messages: []types.Message{
				{
					Role:    "assistant",
					Content: "hi",
					Meta: map[string]interface{}{
						"assertions": []assertions.AssertionResult{
							{Passed: true},
						},
					},
				},
			},
			convResults: []assertions.ConversationValidationResult{
				{Passed: true},
			},
			expected: false,
		},
		{
			name: "mixed - turn fails",
			messages: []types.Message{
				{
					Role:    "assistant",
					Content: "hi",
					Meta: map[string]interface{}{
						"assertions": []assertions.AssertionResult{
							{Passed: false},
						},
					},
				},
			},
			convResults: []assertions.ConversationValidationResult{
				{Passed: true},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := executor.hasFailedAssertions(tt.messages, tt.convResults)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestEvalConversationExecutor_MergeTags(t *testing.T) {
	executor := NewEvalConversationExecutor(
		adapters.NewRegistry(),
		assertions.NewArenaAssertionRegistry(),
		assertions.NewConversationAssertionRegistry(),
		prompt.NewRegistryWithRepository(memory.NewPromptRepository()),
		providers.NewRegistry(),
		nil,
	)

	tests := []struct {
		name         string
		evalTags     []string
		metadata     *adapters.RecordingMetadata
		expectedTags []string
	}{
		{
			name:         "no tags",
			evalTags:     nil,
			metadata:     nil,
			expectedTags: []string{},
		},
		{
			name:         "only eval tags",
			evalTags:     []string{"eval", "test"},
			metadata:     nil,
			expectedTags: []string{"eval", "test"},
		},
		{
			name:     "only metadata tags",
			evalTags: nil,
			metadata: &adapters.RecordingMetadata{
				Tags: []string{"recording", "prod"},
			},
			expectedTags: []string{"recording", "prod"},
		},
		{
			name:     "merged tags no overlap",
			evalTags: []string{"eval", "test"},
			metadata: &adapters.RecordingMetadata{
				Tags: []string{"recording", "prod"},
			},
			expectedTags: []string{"eval", "test", "recording", "prod"},
		},
		{
			name:     "merged tags with duplicates",
			evalTags: []string{"test", "eval"},
			metadata: &adapters.RecordingMetadata{
				Tags: []string{"test", "prod"},
			},
			expectedTags: []string{"test", "eval", "prod"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := executor.mergeTags(tt.evalTags, tt.metadata)

			if len(result) != len(tt.expectedTags) {
				t.Errorf("Expected %d tags, got %d", len(tt.expectedTags), len(result))
			}

			// Check all expected tags are present
			for _, expected := range tt.expectedTags {
				found := false
				for _, tag := range result {
					if tag == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected tag %q not found in result %v", expected, result)
				}
			}
		})
	}
}

func TestEvalConversationExecutor_BuildConversationContext(t *testing.T) {
	executor := NewEvalConversationExecutor(
		adapters.NewRegistry(),
		assertions.NewArenaAssertionRegistry(),
		assertions.NewConversationAssertionRegistry(),
		prompt.NewRegistryWithRepository(memory.NewPromptRepository()),
		providers.NewRegistry(),
		nil,
	)

	tests := []struct {
		name     string
		eval     *config.Eval
		messages []types.Message
		metadata *adapters.RecordingMetadata
	}{
		{
			name: "basic context",
			eval: &config.Eval{
				ID:   "test-eval",
				Tags: []string{"eval-tag"},
			},
			messages: []types.Message{
				{Role: "user", Content: "hello"},
			},
			metadata: &adapters.RecordingMetadata{
				SessionID: "session-123",
				Tags:      []string{"meta-tag"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &ConversationRequest{
				Eval:   tt.eval,
				Config: &config.Config{}, // Empty config is fine for this test
			}
			ctx := executor.buildConversationContext(req, tt.messages, tt.metadata)

			if ctx == nil {
				t.Fatal("Expected non-nil context")
			}

			if len(ctx.AllTurns) != len(tt.messages) {
				t.Errorf("Expected %d messages, got %d", len(tt.messages), len(ctx.AllTurns))
			}

			// Check eval_id in extras
			if evalID, ok := ctx.Metadata.Extras["eval_id"].(string); !ok || evalID != tt.eval.ID {
				t.Errorf("Expected eval_id %q, got %v", tt.eval.ID, ctx.Metadata.Extras["eval_id"])
			}

			// Check session_id if metadata present
			if tt.metadata != nil && tt.metadata.SessionID != "" {
				if sessionID, ok := ctx.Metadata.Extras["session_id"].(string); !ok || sessionID != tt.metadata.SessionID {
					t.Errorf("Expected session_id %q, got %v", tt.metadata.SessionID, ctx.Metadata.Extras["session_id"])
				}
			}
		})
	}
}

func TestEvalConversationExecutor_ApplyTurnAssertions(t *testing.T) {
	// Create a mock validator that always passes
	passValidator := func(params map[string]interface{}) runtimeValidators.Validator {
		return &mockValidator{shouldPass: true}
	}

	// Create a mock validator that always fails
	failValidator := func(params map[string]interface{}) runtimeValidators.Validator {
		return &mockValidator{shouldPass: false}
	}

	assertionReg := runtimeValidators.NewRegistry()
	assertionReg.Register("always_pass", passValidator)
	assertionReg.Register("always_fail", failValidator)

	executor := NewEvalConversationExecutor(
		adapters.NewRegistry(),
		assertionReg,
		assertions.NewConversationAssertionRegistry(),
		prompt.NewRegistryWithRepository(memory.NewPromptRepository()),
		providers.NewRegistry(),
		nil,
	)

	tests := []struct {
		name           string
		assertionCfgs  []assertions.AssertionConfig
		msg            *types.Message
		expectedPassed int
		expectedFailed int
	}{
		{
			name:           "no assertions",
			assertionCfgs:  []assertions.AssertionConfig{},
			msg:            &types.Message{Role: "assistant", Content: "hello"},
			expectedPassed: 0,
			expectedFailed: 0,
		},
		{
			name: "single passing assertion",
			assertionCfgs: []assertions.AssertionConfig{
				{Type: "always_pass", Params: map[string]interface{}{}},
			},
			msg:            &types.Message{Role: "assistant", Content: "hello"},
			expectedPassed: 1,
			expectedFailed: 0,
		},
		{
			name: "single failing assertion",
			assertionCfgs: []assertions.AssertionConfig{
				{Type: "always_fail", Params: map[string]interface{}{}},
			},
			msg:            &types.Message{Role: "assistant", Content: "hello"},
			expectedPassed: 0,
			expectedFailed: 1,
		},
		{
			name: "mixed assertions",
			assertionCfgs: []assertions.AssertionConfig{
				{Type: "always_pass", Params: map[string]interface{}{}},
				{Type: "always_fail", Params: map[string]interface{}{}},
				{Type: "always_pass", Params: map[string]interface{}{}},
			},
			msg:            &types.Message{Role: "assistant", Content: "hello"},
			expectedPassed: 2,
			expectedFailed: 1,
		},
		{
			name: "unknown assertion type",
			assertionCfgs: []assertions.AssertionConfig{
				{Type: "unknown", Params: map[string]interface{}{}},
			},
			msg:            &types.Message{Role: "assistant", Content: "hello"},
			expectedPassed: 0,
			expectedFailed: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			convCtx := &assertions.ConversationContext{
				AllTurns: []types.Message{*tt.msg},
				Metadata: assertions.ConversationMetadata{
					Extras: map[string]interface{}{
						"judge_targets": map[string]interface{}{},
					},
				},
			}

			executor.applyTurnAssertions(tt.assertionCfgs, tt.msg, convCtx)

			// Check that assertions were stored in metadata
			if tt.msg.Meta == nil {
				if tt.expectedPassed+tt.expectedFailed > 0 {
					t.Fatal("Expected assertions in message metadata")
				}
				return
			}

			resultsRaw, ok := tt.msg.Meta["assertions"]
			if !ok {
				if tt.expectedPassed+tt.expectedFailed > 0 {
					t.Fatal("Expected assertions key in message metadata")
				}
				return
			}

			results, ok := resultsRaw.([]assertions.AssertionResult)
			if !ok {
				t.Fatal("Expected assertions to be []AssertionResult")
			}

			passed := 0
			failed := 0
			for _, result := range results {
				if result.Passed {
					passed++
				} else {
					failed++
				}
			}

			if passed != tt.expectedPassed {
				t.Errorf("Expected %d passing assertions, got %d", tt.expectedPassed, passed)
			}
			if failed != tt.expectedFailed {
				t.Errorf("Expected %d failing assertions, got %d", tt.expectedFailed, failed)
			}
		})
	}
}

func TestEvalConversationExecutor_ExecuteConversationStream(t *testing.T) {
	executor := NewEvalConversationExecutor(
		adapters.NewRegistry(),
		assertions.NewArenaAssertionRegistry(),
		assertions.NewConversationAssertionRegistry(),
		prompt.NewRegistryWithRepository(memory.NewPromptRepository()),
		providers.NewRegistry(),
		nil,
	)

	req := ConversationRequest{
		Eval: &config.Eval{
			ID: "test",
			Recording: config.RecordingSource{
				Path: "test.json",
			},
		},
	}

	ch, err := executor.ExecuteConversationStream(context.Background(), req)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	chunks := []ConversationStreamChunk{}
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	if len(chunks) != 1 {
		t.Errorf("Expected 1 chunk, got %d", len(chunks))
	}

	if chunks[0].Result == nil {
		t.Error("Expected non-nil result in chunk")
	}

	// Should have same failure as non-streaming (no adapter found)
	if !chunks[0].Result.Failed {
		t.Error("Expected failed result when adapter not found")
	}
}

func TestEvalConversationExecutor_ApplyConversationAssertions(t *testing.T) {
	// Create mock conversation validators
	passValidator := &mockConversationValidator{shouldPass: true}
	failValidator := &mockConversationValidator{shouldPass: false}

	convAssertionReg := assertions.NewConversationAssertionRegistry()
	convAssertionReg.Register("always_pass", func() assertions.ConversationValidator { return passValidator })
	convAssertionReg.Register("always_fail", func() assertions.ConversationValidator { return failValidator })

	executor := NewEvalConversationExecutor(
		adapters.NewRegistry(),
		assertions.NewArenaAssertionRegistry(),
		convAssertionReg,
		prompt.NewRegistryWithRepository(memory.NewPromptRepository()),
		providers.NewRegistry(),
		nil,
	)

	tests := []struct {
		name           string
		assertionCfgs  []assertions.AssertionConfig
		expectedPassed int
		expectedFailed int
	}{
		{
			name:           "no assertions",
			assertionCfgs:  []assertions.AssertionConfig{},
			expectedPassed: 0,
			expectedFailed: 0,
		},
		{
			name: "single passing assertion",
			assertionCfgs: []assertions.AssertionConfig{
				{Type: "always_pass", Params: map[string]interface{}{}},
			},
			expectedPassed: 1,
			expectedFailed: 0,
		},
		{
			name: "single failing assertion",
			assertionCfgs: []assertions.AssertionConfig{
				{Type: "always_fail", Params: map[string]interface{}{}},
			},
			expectedPassed: 0,
			expectedFailed: 1,
		},
		{
			name: "mixed assertions",
			assertionCfgs: []assertions.AssertionConfig{
				{Type: "always_pass", Params: map[string]interface{}{}},
				{Type: "always_fail", Params: map[string]interface{}{}},
				{Type: "always_pass", Params: map[string]interface{}{}},
			},
			expectedPassed: 2,
			expectedFailed: 1,
		},
		{
			name: "unknown assertion type skipped",
			assertionCfgs: []assertions.AssertionConfig{
				{Type: "unknown", Params: map[string]interface{}{}},
			},
			expectedPassed: 0,
			expectedFailed: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			convCtx := &assertions.ConversationContext{
				AllTurns: []types.Message{
					{Role: "user", Content: "hello"},
				},
				Metadata: assertions.ConversationMetadata{
					Extras: map[string]interface{}{},
				},
			}

			results := executor.applyConversationAssertions(context.Background(), tt.assertionCfgs, convCtx)

			passed := 0
			failed := 0
			for _, result := range results {
				if result.Passed {
					passed++
				} else {
					failed++
				}
			}

			if passed != tt.expectedPassed {
				t.Errorf("Expected %d passing assertions, got %d", tt.expectedPassed, passed)
			}
			if failed != tt.expectedFailed {
				t.Errorf("Expected %d failing assertions, got %d", tt.expectedFailed, failed)
			}
		})
	}
}

// mockValidator is a simple mock for testing
type mockValidator struct {
	shouldPass bool
}

func (m *mockValidator) Validate(response string, params map[string]interface{}) runtimeValidators.ValidationResult {
	return runtimeValidators.ValidationResult{
		Passed:  m.shouldPass,
		Details: map[string]interface{}{"mock": true},
	}
}

// mockConversationValidator is a simple mock for conversation-level validators
type mockConversationValidator struct {
	shouldPass bool
}

func (m *mockConversationValidator) Type() string {
	return "mock"
}

func (m *mockConversationValidator) ValidateConversation(
	ctx context.Context,
	convCtx *assertions.ConversationContext,
	params map[string]interface{},
) assertions.ConversationValidationResult {
	return assertions.ConversationValidationResult{
		Passed:  m.shouldPass,
		Message: "mock validation",
		Details: map[string]interface{}{"mock": true},
	}
}
