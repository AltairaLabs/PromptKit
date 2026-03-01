package engine

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/adapters"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)

func TestNewEvalConversationExecutor(t *testing.T) {
	adapterReg := adapters.NewRegistry()
	promptReg := prompt.NewRegistryWithRepository(memory.NewPromptRepository())
	providerReg := providers.NewRegistry()

	executor := NewEvalConversationExecutor(
		adapterReg,
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
	// Without a packEvalHook, applyTurnAssertions is a no-op because
	// assertions now run exclusively through the eval pipeline.
	executor := NewEvalConversationExecutor(
		adapters.NewRegistry(),
		prompt.NewRegistryWithRepository(memory.NewPromptRepository()),
		providers.NewRegistry(),
		nil,
	)

	tests := []struct {
		name          string
		assertionCfgs []assertions.AssertionConfig
		msg           *types.Message
	}{
		{
			name:          "no assertions",
			assertionCfgs: []assertions.AssertionConfig{},
			msg:           &types.Message{Role: "assistant", Content: "hello"},
		},
		{
			name: "single passing assertion skipped without packEvalHook",
			assertionCfgs: []assertions.AssertionConfig{
				{Type: "always_pass", Params: map[string]interface{}{}},
			},
			msg: &types.Message{Role: "assistant", Content: "hello"},
		},
		{
			name: "single failing assertion skipped without packEvalHook",
			assertionCfgs: []assertions.AssertionConfig{
				{Type: "always_fail", Params: map[string]interface{}{}},
			},
			msg: &types.Message{Role: "assistant", Content: "hello"},
		},
		{
			name: "mixed assertions skipped without packEvalHook",
			assertionCfgs: []assertions.AssertionConfig{
				{Type: "always_pass", Params: map[string]interface{}{}},
				{Type: "always_fail", Params: map[string]interface{}{}},
				{Type: "always_pass", Params: map[string]interface{}{}},
			},
			msg: &types.Message{Role: "assistant", Content: "hello"},
		},
		{
			name: "unknown assertion type skipped without packEvalHook",
			assertionCfgs: []assertions.AssertionConfig{
				{Type: "unknown", Params: map[string]interface{}{}},
			},
			msg: &types.Message{Role: "assistant", Content: "hello"},
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

			// Without packEvalHook, no assertions should be stored in metadata
			if tt.msg.Meta != nil {
				if _, ok := tt.msg.Meta["assertions"]; ok {
					t.Fatal("Expected no assertions in message metadata without packEvalHook")
				}
			}
		})
	}
}

func TestEvalConversationExecutor_ExecuteConversationStream(t *testing.T) {
	executor := NewEvalConversationExecutor(
		adapters.NewRegistry(),
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

func TestEvalConversationExecutor_ConversationAssertionsWithEvalOnlyHook(t *testing.T) {
	// Build a PackEvalHook with empty defs (eval-only mode, no pack).
	// This simulates the fix: eval mode creates a hook so assertions execute.
	registry := evals.NewEvalTypeRegistry()
	hook := NewPackEvalHook(registry, nil, false, nil, "")

	executor := NewEvalConversationExecutor(
		adapters.NewRegistry(),
		prompt.NewRegistryWithRepository(memory.NewPromptRepository()),
		providers.NewRegistry(),
		hook,
	)

	convCtx := &assertions.ConversationContext{
		AllTurns: []types.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "I can help you with billing and account issues."},
		},
		Metadata: assertions.ConversationMetadata{
			Extras: map[string]interface{}{},
		},
	}

	assertionCfgs := []assertions.AssertionConfig{
		{
			Type:    "contains_any",
			Params:  map[string]interface{}{"patterns": []interface{}{"billing", "support"}},
			Message: "should mention billing or support",
		},
		{
			Type:    "contains_any",
			Params:  map[string]interface{}{"patterns": []interface{}{"nonexistent-keyword"}},
			Message: "should fail - keyword not present",
		},
	}

	results := executor.evaluateConversationAssertions(context.Background(), assertionCfgs, convCtx)

	if len(results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(results))
	}
	if !results[0].Passed {
		t.Errorf("Expected first assertion (contains_any 'billing') to pass, got failed: %s", results[0].Details)
	}
	if results[1].Passed {
		t.Errorf("Expected second assertion (contains_any 'nonexistent-keyword') to fail, got passed")
	}
}

func TestEvalConversationExecutor_TurnAssertionsWithEvalOnlyHook(t *testing.T) {
	registry := evals.NewEvalTypeRegistry()
	hook := NewPackEvalHook(registry, nil, false, nil, "")

	executor := NewEvalConversationExecutor(
		adapters.NewRegistry(),
		prompt.NewRegistryWithRepository(memory.NewPromptRepository()),
		providers.NewRegistry(),
		hook,
	)

	msg := &types.Message{Role: "assistant", Content: "Here is your account summary."}
	convCtx := &assertions.ConversationContext{
		AllTurns: []types.Message{*msg},
		Metadata: assertions.ConversationMetadata{
			Extras: map[string]interface{}{},
		},
	}

	assertionCfgs := []assertions.AssertionConfig{
		{
			Type:   "contains_any",
			Params: map[string]interface{}{"patterns": []interface{}{"account"}},
		},
	}

	executor.applyTurnAssertions(assertionCfgs, msg, convCtx)

	if msg.Meta == nil {
		t.Fatal("Expected message metadata to be set")
	}
	results, ok := msg.Meta["assertions"].([]assertions.AssertionResult)
	if !ok {
		t.Fatal("Expected assertions in message metadata")
	}
	if len(results) != 1 {
		t.Fatalf("Expected 1 assertion result, got %d", len(results))
	}
	if !results[0].Passed {
		t.Errorf("Expected assertion to pass, got failed")
	}
}

func TestEvalConversationExecutor_ApplyConversationAssertions(t *testing.T) {
	// Without a packEvalHook, evaluateConversationAssertions returns nil
	// because assertions now run exclusively through the eval pipeline.
	executor := NewEvalConversationExecutor(
		adapters.NewRegistry(),
		prompt.NewRegistryWithRepository(memory.NewPromptRepository()),
		providers.NewRegistry(),
		nil,
	)

	tests := []struct {
		name          string
		assertionCfgs []assertions.AssertionConfig
	}{
		{
			name:          "no assertions",
			assertionCfgs: []assertions.AssertionConfig{},
		},
		{
			name: "single passing assertion skipped without packEvalHook",
			assertionCfgs: []assertions.AssertionConfig{
				{Type: "always_pass", Params: map[string]interface{}{}},
			},
		},
		{
			name: "single failing assertion skipped without packEvalHook",
			assertionCfgs: []assertions.AssertionConfig{
				{Type: "always_fail", Params: map[string]interface{}{}},
			},
		},
		{
			name: "mixed assertions skipped without packEvalHook",
			assertionCfgs: []assertions.AssertionConfig{
				{Type: "always_pass", Params: map[string]interface{}{}},
				{Type: "always_fail", Params: map[string]interface{}{}},
				{Type: "always_pass", Params: map[string]interface{}{}},
			},
		},
		{
			name: "unknown assertion type skipped without packEvalHook",
			assertionCfgs: []assertions.AssertionConfig{
				{Type: "unknown", Params: map[string]interface{}{}},
			},
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

			results := executor.evaluateConversationAssertions(context.Background(), tt.assertionCfgs, convCtx)

			// Without packEvalHook, all assertions should be skipped (nil result)
			if len(results) != 0 {
				t.Errorf("Expected 0 results without packEvalHook, got %d", len(results))
			}
		})
	}
}

