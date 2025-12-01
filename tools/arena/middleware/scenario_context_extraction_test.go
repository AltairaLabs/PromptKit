package middleware

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestScenarioContextExtractionMiddleware_Process(t *testing.T) {
	tests := []struct {
		name               string
		scenario           *config.Scenario
		messages           []types.Message
		existingVars       map[string]string
		wantDomain         string
		wantUserRole       string
		wantContextSlot    string
		shouldNotOverwrite bool
	}{
		{
			name: "extracts from scenario with metadata",
			scenario: &config.Scenario{
				ID:          "test-scenario",
				TaskType:    "customer-support",
				Description: "Help a customer with billing",
				ContextMetadata: &config.ContextMetadata{
					Domain:   "billing",
					UserRole: "customer",
				},
			},
			messages: []types.Message{
				{Role: "user", Content: "I have a question about my bill"},
			},
			wantDomain:      "billing",
			wantUserRole:    "customer",
			wantContextSlot: "Help a customer with billing. User wants to: I have a question about my bill",
		},
		{
			name: "scenario without metadata",
			scenario: &config.Scenario{
				ID:          "test-scenario",
				TaskType:    "general",
				Description: "General conversation",
			},
			messages:        []types.Message{},
			wantDomain:      "",
			wantUserRole:    "",
			wantContextSlot: "General conversation",
		},
		{
			name: "long user message truncated",
			scenario: &config.Scenario{
				ID:          "test-scenario",
				TaskType:    "support",
				Description: "Customer support",
			},
			messages: []types.Message{
				{Role: "user", Content: "This is a very long message that should be truncated because it exceeds the maximum length allowed for context extraction and we need to make sure it gets cut off properly"},
			},
			wantDomain:      "",
			wantUserRole:    "",
			wantContextSlot: "Customer support. User wants to: This is a very long message that should be truncated because it exceeds the maximum length allowed for context extraction and we need to make sure it ...",
		},
		{
			name: "no description falls back to task type",
			scenario: &config.Scenario{
				ID:       "test-scenario",
				TaskType: "brainstorming",
			},
			messages:        []types.Message{},
			wantDomain:      "",
			wantUserRole:    "",
			wantContextSlot: "brainstorming conversation",
		},
		{
			name: "preserves existing variables",
			scenario: &config.Scenario{
				ID:       "test-scenario",
				TaskType: "support",
				ContextMetadata: &config.ContextMetadata{
					Domain:   "technical",
					UserRole: "admin",
				},
			},
			messages: []types.Message{},
			existingVars: map[string]string{
				"domain": "existing-domain",
			},
			wantDomain:         "existing-domain", // Should NOT be overwritten
			wantUserRole:       "admin",
			wantContextSlot:    "support conversation",
			shouldNotOverwrite: true,
		},
		{
			name: "non-user first message ignored",
			scenario: &config.Scenario{
				ID:          "test-scenario",
				TaskType:    "support",
				Description: "Help user",
			},
			messages: []types.Message{
				{Role: "assistant", Content: "Hello, how can I help?"},
			},
			wantDomain:      "",
			wantUserRole:    "",
			wantContextSlot: "Help user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware := ScenarioContextExtractionMiddleware(tt.scenario)

			execCtx := &pipeline.ExecutionContext{
				Messages:  tt.messages,
				Variables: tt.existingVars,
			}

			nextCalled := false
			err := middleware.Process(execCtx, func() error {
				nextCalled = true
				return nil
			})

			if err != nil {
				t.Errorf("Process() error = %v, want nil", err)
			}

			if !nextCalled {
				t.Error("Process() did not call next()")
			}

			if execCtx.Variables == nil {
				t.Fatal("Process() did not initialize Variables map")
			}

			if execCtx.Variables["domain"] != tt.wantDomain {
				t.Errorf("Variables[domain] = %v, want %v", execCtx.Variables["domain"], tt.wantDomain)
			}

			if execCtx.Variables["user_role"] != tt.wantUserRole {
				t.Errorf("Variables[user_role] = %v, want %v", execCtx.Variables["user_role"], tt.wantUserRole)
			}

			if execCtx.Variables["user_context"] != tt.wantUserRole {
				t.Errorf("Variables[user_context] = %v, want %v", execCtx.Variables["user_context"], tt.wantUserRole)
			}

			if execCtx.Variables["context_slot"] != tt.wantContextSlot {
				t.Errorf("Variables[context_slot] = %v, want %v", execCtx.Variables["context_slot"], tt.wantContextSlot)
			}
		})
	}
}

func TestScenarioContextExtractionMiddleware_StreamChunk(t *testing.T) {
	scenario := &config.Scenario{
		ID:       "test",
		TaskType: "support",
	}
	middleware := ScenarioContextExtractionMiddleware(scenario)

	execCtx := &pipeline.ExecutionContext{}
	err := middleware.StreamChunk(execCtx, nil)

	if err != nil {
		t.Errorf("StreamChunk() error = %v, want nil", err)
	}
}

func TestBuildContextSlot(t *testing.T) {
	tests := []struct {
		name     string
		scenario *config.Scenario
		messages []types.Message
		want     string
	}{
		{
			name: "description and user message",
			scenario: &config.Scenario{
				Description: "Help with billing",
				TaskType:    "support",
			},
			messages: []types.Message{
				{Role: "user", Content: "I need help"},
			},
			want: "Help with billing. User wants to: I need help",
		},
		{
			name: "description only",
			scenario: &config.Scenario{
				Description: "Billing support",
				TaskType:    "support",
			},
			messages: []types.Message{},
			want:     "Billing support",
		},
		{
			name: "no description, fallback to task type",
			scenario: &config.Scenario{
				TaskType: "customer-service",
			},
			messages: []types.Message{},
			want:     "customer-service conversation",
		},
		{
			name: "long user message truncated",
			scenario: &config.Scenario{
				Description: "Support",
				TaskType:    "support",
			},
			messages: []types.Message{
				{Role: "user", Content: "This is a very long message that definitely exceeds one hundred and fifty characters and should be truncated with ellipsis at the end to keep the context manageable and readable"},
			},
			want: "Support. User wants to: This is a very long message that definitely exceeds one hundred and fifty characters and should be truncated with ellipsis at the end to keep the cont...",
		},
		{
			name: "empty user message ignored",
			scenario: &config.Scenario{
				Description: "Support ticket",
				TaskType:    "support",
			},
			messages: []types.Message{
				{Role: "user", Content: ""},
			},
			want: "Support ticket",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildContextSlot(tt.scenario, tt.messages)
			if got != tt.want {
				t.Errorf("buildContextSlot() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractFromScenario(t *testing.T) {
	tests := []struct {
		name     string
		scenario *config.Scenario
		messages []types.Message
		wantVars map[string]string
	}{
		{
			name: "full metadata",
			scenario: &config.Scenario{
				ID:          "test",
				TaskType:    "support",
				Description: "Help customer",
				ContextMetadata: &config.ContextMetadata{
					Domain:   "billing",
					UserRole: "customer",
				},
			},
			messages: []types.Message{},
			wantVars: map[string]string{
				"domain":       "billing",
				"user_role":    "customer",
				"user_context": "customer",
				"context_slot": "Help customer",
			},
		},
		{
			name: "no metadata",
			scenario: &config.Scenario{
				ID:          "test",
				TaskType:    "general",
				Description: "Chat",
			},
			messages: []types.Message{},
			wantVars: map[string]string{
				"domain":       "",
				"user_role":    "",
				"user_context": "",
				"context_slot": "Chat",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFromScenario(tt.scenario, tt.messages)

			for key, want := range tt.wantVars {
				if got[key] != want {
					t.Errorf("extractFromScenario()[%s] = %v, want %v", key, got[key], want)
				}
			}
		})
	}
}
