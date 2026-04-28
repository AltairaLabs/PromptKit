package stage

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/memory"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// MemoryRetrievalStage injects relevant memories into the conversation context.
// Runs early in the pipeline (before the provider stage). Accumulates
// messages from input elements, then calls Retriever.RetrieveContext() once
// the input channel closes and writes the formatted memory context onto
// TurnState.Variables["memory_context"] for the template stage to consume.
//
// No-op passthrough when retriever or turnState is nil.
type MemoryRetrievalStage struct {
	BaseStage
	retriever memory.Retriever
	store     memory.Store
	scope     map[string]string
	turnState *TurnState
}

// NewMemoryRetrievalStage creates a retrieval stage.
func NewMemoryRetrievalStage(
	retriever memory.Retriever, store memory.Store, scope map[string]string,
) *MemoryRetrievalStage {
	return NewMemoryRetrievalStageWithTurnState(retriever, store, scope, nil)
}

// NewMemoryRetrievalStageWithTurnState creates a retrieval stage that
// publishes its formatted memory context onto the supplied TurnState.
func NewMemoryRetrievalStageWithTurnState(
	retriever memory.Retriever, store memory.Store, scope map[string]string, turnState *TurnState,
) *MemoryRetrievalStage {
	return &MemoryRetrievalStage{
		BaseStage: NewBaseStage("memory_retrieval", StageTypeTransform),
		retriever: retriever,
		store:     store,
		scope:     scope,
		turnState: turnState,
	}
}

// Process implements Stage.
func (s *MemoryRetrievalStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	var messages []types.Message
	var pending []StreamElement

	for elem := range input {
		if elem.Message != nil {
			messages = append(messages, *elem.Message)
		}
		pending = append(pending, elem)
	}

	if s.retriever != nil && s.turnState != nil && len(messages) > 0 {
		memories, err := s.retriever.RetrieveContext(ctx, s.scope, messages)
		if err != nil {
			logger.Error("Memory retrieval failed", "error", err)
		} else if len(memories) > 0 {
			s.injectMemoryContext(memories)
			logger.Debug("Memories injected into context", "count", len(memories))
		}
	}

	for i := range pending {
		select {
		case output <- pending[i]:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// injectMemoryContext formats the retrieved memories and writes them onto
// TurnState.Variables["memory_context"].
func (s *MemoryRetrievalStage) injectMemoryContext(memories []*memory.Memory) {
	var ctx string
	for _, m := range memories {
		ctx += fmt.Sprintf("[%s] %s (confidence: %.1f)\n", m.Type, m.Content, m.Confidence)
	}
	if s.turnState.Variables == nil {
		s.turnState.Variables = make(map[string]string)
	}
	s.turnState.Variables["memory_context"] = ctx
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

	var messages []types.Message
	var pending []StreamElement

	for elem := range input {
		if elem.Message != nil {
			messages = append(messages, *elem.Message)
		}
		pending = append(pending, elem)
	}

	if s.extractor != nil && len(messages) > 0 {
		s.extractAndSave(ctx, messages)
	}

	for i := range pending {
		select {
		case output <- pending[i]:
		case <-ctx.Done():
			return ctx.Err()
		}
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
		// Set provenance if the extractor didn't already set it.
		if m.GetProvenance() == "" {
			m.SetProvenance(memory.ProvenanceAgentExtracted)
		}
		if saveErr := s.store.Save(ctx, m); saveErr != nil {
			logger.Error("Memory save failed", "id", m.ID, "error", saveErr)
		}
	}
	if len(memories) > 0 {
		logger.Debug("Memories extracted and saved", "count", len(memories))
	}
}
