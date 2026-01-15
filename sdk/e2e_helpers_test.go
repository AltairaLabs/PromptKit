//go:build e2e

package sdk

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pipeline"
	"github.com/AltairaLabs/PromptKit/sdk/session"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// E2E Test Helpers
//
// Shared utilities for creating test conversations with mock providers.
// These helpers allow e2e tests to verify integration without making real
// API calls.
// =============================================================================

// E2ETestConfig holds configuration for creating e2e test conversations.
type E2ETestConfig struct {
	// EventBus for event emission (nil to disable events)
	EventBus *events.EventBus

	// ConversationID for the test conversation
	ConversationID string

	// Streaming enables streaming pipeline mode
	Streaming bool
}

// DefaultE2ETestConfig returns default configuration for e2e tests.
func DefaultE2ETestConfig() E2ETestConfig {
	return E2ETestConfig{
		ConversationID: "e2e-test",
	}
}

// NewE2ETestConversation creates a conversation with a mock provider for e2e tests.
// This is the primary helper for creating test conversations.
func NewE2ETestConversation(t *testing.T, cfg E2ETestConfig) *Conversation {
	t.Helper()

	if cfg.ConversationID == "" {
		cfg.ConversationID = "e2e-test"
	}

	// Create prompt registry
	repo := memory.NewPromptRepository()
	repo.RegisterPrompt("chat", &prompt.Config{
		APIVersion: "promptkit.io/v1alpha1",
		Kind:       "Prompt",
		Spec: prompt.Spec{
			TaskType:       "chat",
			SystemTemplate: "You are a helpful assistant.",
		},
	})
	promptRegistry := prompt.NewRegistryWithRepository(repo)

	// Create mock provider
	provider := mock.NewProvider("mock-test", "mock-model", false)

	// Create event emitter if event bus is provided
	var emitter *events.Emitter
	if cfg.EventBus != nil {
		emitter = events.NewEmitter(cfg.EventBus, "", cfg.ConversationID, cfg.ConversationID)
	}

	// Build pipeline with event emitter
	pipelineCfg := &pipeline.Config{
		PromptRegistry: promptRegistry,
		TaskType:       "chat",
		Provider:       provider,
		EventEmitter:   emitter,
	}

	var pipe *stage.StreamPipeline
	var err error
	if cfg.Streaming {
		pipe, err = pipeline.BuildStreamPipeline(pipelineCfg)
	} else {
		pipe, err = pipeline.Build(pipelineCfg)
	}
	require.NoError(t, err)

	// Create session
	sess, err := session.NewUnarySession(session.UnarySessionConfig{
		Pipeline:       pipe,
		StateStore:     statestore.NewMemoryStore(),
		ConversationID: cfg.ConversationID,
	})
	require.NoError(t, err)

	// Build conversation struct
	conv := &Conversation{
		pack:           nil, // Not needed for e2e tests
		prompt:         &pack.Prompt{ID: "chat", Name: "chat"},
		promptName:     "chat",
		promptRegistry: promptRegistry,
		config:         &config{eventBus: cfg.EventBus, provider: provider},
		mode:           UnaryMode,
		unarySession:   sess,
	}

	return conv
}

// MustNewE2ETestConversation creates a conversation and fails the test on error.
func MustNewE2ETestConversation(t *testing.T, eventBus *events.EventBus) *Conversation {
	t.Helper()
	return NewE2ETestConversation(t, E2ETestConfig{
		EventBus:       eventBus,
		ConversationID: "e2e-test",
	})
}

// MustNewE2EStreamingConversation creates a streaming conversation.
func MustNewE2EStreamingConversation(t *testing.T, eventBus *events.EventBus) *Conversation {
	t.Helper()
	return NewE2ETestConversation(t, E2ETestConfig{
		EventBus:       eventBus,
		ConversationID: "e2e-test-streaming",
		Streaming:      true,
	})
}
