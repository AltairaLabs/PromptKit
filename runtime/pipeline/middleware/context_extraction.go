package middleware

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ContextExtractionMiddleware extracts domain hints and user roles from conversation history only.
// This is designed for Runtime SDK use where no scenario metadata is available.
//
// This middleware analyzes the message history to detect:
// - Domain/topic (e.g., "mobile app development", "finance", "healthcare")
// - User role (e.g., "entrepreneur", "developer", "student")
// - Other context variables
//
// Extracted variables are merged into execCtx.Variables, allowing templates to use them.

type contextExtractionMiddleware struct{}

// ContextExtractionMiddleware creates a middleware that extracts context variables from conversation history.
func ContextExtractionMiddleware() pipeline.Middleware {
	return &contextExtractionMiddleware{}
}

// Process extracts context variables from the conversation history and merges them into ExecutionContext.Variables.
// It analyzes messages to detect domain hints, user roles, and other context variables.
// Results are cached in metadata to avoid repeated extraction.
func (m *contextExtractionMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
	// Extract context from conversation messages
	conversationID := ""
	if id, ok := execCtx.Metadata["conversation_id"].(string); ok {
		conversationID = id
	}

	// Check cache first
	cacheKey := "context_extraction_cache"
	if cached, ok := execCtx.Metadata[cacheKey].(map[string]string); ok {
		// Use cached extraction if it exists
		if execCtx.Variables == nil {
			execCtx.Variables = make(map[string]string)
		}
		for k, v := range cached {
			if _, exists := execCtx.Variables[k]; !exists {
				execCtx.Variables[k] = v
			}
		}
		return next()
	}

	extracted := extractFromMessages(execCtx.Messages, conversationID)

	// Initialize Variables map if needed
	if execCtx.Variables == nil {
		execCtx.Variables = make(map[string]string)
	}

	// Merge extracted variables (don't overwrite existing ones)
	for k, v := range extracted {
		if _, exists := execCtx.Variables[k]; !exists {
			execCtx.Variables[k] = v
		}
	}

	// Cache the extraction
	execCtx.Metadata[cacheKey] = extracted

	// Continue to next middleware
	return next()
}

// StreamChunk is a no-op for context extraction middleware as it doesn't process stream chunks.
func (m *contextExtractionMiddleware) StreamChunk(execCtx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	// Context extraction middleware doesn't process chunks
	return nil
}

// extractFromMessages extracts context from conversation messages only
func extractFromMessages(messages []types.Message, conversationID string) map[string]string {
	variables := make(map[string]string)

	// Combine all message content
	text := ""
	userText := ""
	for i := range messages {
		text += " " + messages[i].Content
		if messages[i].Role == "user" {
			userText += " " + messages[i].Content
		}
	}

	// Extract domain and role (uses helper functions from context_extraction_utils.go)
	domain := extractDomainFromText(text)
	userRole := extractRoleFromText(userText)
	contextSlot := summarizeMessages(messages)

	variables["domain"] = domain
	variables["user_context"] = userRole
	variables["user_role"] = userRole
	variables["context_slot"] = contextSlot

	if len(messages) > 0 {
		variables["message_count"] = fmt.Sprintf("%d", len(messages))
		variables["conversation_id"] = conversationID
	}

	return variables
}
