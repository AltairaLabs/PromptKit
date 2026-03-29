package stage

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/memory"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// MemoryRetrievalStage injects relevant memories into the conversation context.
// Runs early in the pipeline (before the provider stage). Calls
// Retriever.RetrieveContext() with the current messages and injects returned
// memories as a system-level context message.
//
// No-op passthrough when retriever is nil.
type MemoryRetrievalStage struct {
	BaseStage
	retriever memory.Retriever
	store     memory.Store
	scope     map[string]string
}

// NewMemoryRetrievalStage creates a retrieval stage.
func NewMemoryRetrievalStage(
	retriever memory.Retriever, store memory.Store, scope map[string]string,
) *MemoryRetrievalStage {
	return &MemoryRetrievalStage{
		BaseStage: NewBaseStage("memory_retrieval", StageTypeTransform),
		retriever: retriever,
		store:     store,
		scope:     scope,
	}
}

// Process implements Stage.
func (s *MemoryRetrievalStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	for elem := range input {
		if s.retriever == nil {
			output <- elem
			continue
		}

		// Extract messages from the element for context
		messages := extractMessages(&elem)
		if len(messages) == 0 {
			output <- elem
			continue
		}

		memories, err := s.retriever.RetrieveContext(ctx, s.scope, messages)
		if err != nil {
			logger.Error("Memory retrieval failed", "error", err)
			output <- elem
			continue
		}

		if len(memories) > 0 {
			injectMemories(&elem, memories)
			logger.Debug("Memories injected into context",
				"count", len(memories))
		}

		output <- elem
	}
	return nil
}

// MemoryExtractionStage extracts memories from the conversation after the
// provider responds. Runs late in the pipeline (before state persist). Calls
// Extractor.Extract() with the conversation messages and saves results via
// Store.Save().
//
// No-op passthrough when extractor is nil.
type MemoryExtractionStage struct {
	BaseStage
	extractor memory.Extractor
	store     memory.Store
	scope     map[string]string
}

// NewMemoryExtractionStage creates an extraction stage.
func NewMemoryExtractionStage(
	extractor memory.Extractor, store memory.Store, scope map[string]string,
) *MemoryExtractionStage {
	return &MemoryExtractionStage{
		BaseStage: NewBaseStage("memory_extraction", StageTypeTransform),
		extractor: extractor,
		store:     store,
		scope:     scope,
	}
}

// Process implements Stage.
func (s *MemoryExtractionStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	for elem := range input {
		if s.extractor == nil {
			output <- elem
			continue
		}

		messages := extractMessages(&elem)
		if len(messages) == 0 {
			output <- elem
			continue
		}

		s.extractAndSave(ctx, messages)
		output <- elem
	}
	return nil
}

// extractAndSave runs the extractor and saves resulting memories.
func (s *MemoryExtractionStage) extractAndSave(ctx context.Context, messages []types.Message) {
	memories, err := s.extractor.Extract(ctx, s.scope, messages)
	if err != nil {
		logger.Error("Memory extraction failed", "error", err)
		return
	}
	for _, m := range memories {
		if m.Scope == nil {
			m.Scope = s.scope
		}
		if saveErr := s.store.Save(ctx, m); saveErr != nil {
			logger.Error("Memory save failed", "id", m.ID, "error", saveErr)
		}
	}
	if len(memories) > 0 {
		logger.Debug("Memories extracted and saved", "count", len(memories))
	}
}

// extractMessages pulls the message slice from a StreamElement's metadata.
func extractMessages(elem *StreamElement) []types.Message {
	if elem.Metadata == nil {
		return nil
	}
	msgs, ok := elem.Metadata["messages"]
	if !ok {
		return nil
	}
	typedMsgs, ok := msgs.([]types.Message)
	if !ok {
		return nil
	}
	return typedMsgs
}

// injectMemories adds retrieved memories to the element metadata as context.
func injectMemories(elem *StreamElement, memories []*memory.Memory) {
	if elem.Metadata == nil {
		return
	}
	// Format memories as a context string for the system prompt
	var memoryContext string
	for _, m := range memories {
		memoryContext += fmt.Sprintf("[%s] %s (confidence: %.1f)\n", m.Type, m.Content, m.Confidence)
	}

	// Store formatted memories in metadata for the template stage to use
	if existing, ok := elem.Metadata["variables"].(map[string]string); ok {
		existing["memory_context"] = memoryContext
	} else {
		elem.Metadata["memory_context"] = memoryContext
	}
}
