package stage

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/memory"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

type mockRetriever struct {
	memories []*memory.Memory
}

func (r *mockRetriever) RetrieveContext(_ context.Context, _ map[string]string, _ []types.Message) ([]*memory.Memory, error) {
	return r.memories, nil
}

type mockExtractor struct {
	extracted []*memory.Memory
}

func (e *mockExtractor) Extract(_ context.Context, _ map[string]string, _ []types.Message) ([]*memory.Memory, error) {
	return e.extracted, nil
}

func TestMemoryRetrievalStage_InjectsMemories(t *testing.T) {
	ret := &mockRetriever{
		memories: []*memory.Memory{
			{Type: "fact", Content: "User likes Go", Confidence: 0.9},
		},
	}
	store := memory.NewInMemoryStore()
	s := NewMemoryRetrievalStage(ret, store, nil)

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	msgs := []types.Message{{Role: "user", Content: "hello"}}
	input <- StreamElement{Metadata: map[string]any{"messages": msgs}}
	close(input)

	err := s.Process(context.Background(), input, output)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	elem := <-output
	if elem.Metadata["memory_context"] == nil {
		t.Error("memory_context should be injected")
	}
}

func TestMemoryRetrievalStage_NilRetrieverPassthrough(t *testing.T) {
	s := NewMemoryRetrievalStage(nil, nil, nil)

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	input <- StreamElement{Metadata: map[string]any{"test": "value"}}
	close(input)

	err := s.Process(context.Background(), input, output)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	elem := <-output
	if elem.Metadata["test"] != "value" {
		t.Error("element should pass through unchanged")
	}
}

func TestMemoryExtractionStage_ExtractsAndSaves(t *testing.T) {
	ext := &mockExtractor{
		extracted: []*memory.Memory{
			{Type: "fact", Content: "User mentioned Go preference"},
		},
	}
	store := memory.NewInMemoryStore()
	scope := map[string]string{"user_id": "test"}
	s := NewMemoryExtractionStage(ext, store, scope)

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	msgs := []types.Message{
		{Role: "user", Content: "I love Go"},
		{Role: "assistant", Content: "Great choice!"},
	}
	input <- StreamElement{Metadata: map[string]any{"messages": msgs}}
	close(input)

	err := s.Process(context.Background(), input, output)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	<-output // consume

	// Verify memory was saved with provenance
	saved, _ := store.List(context.Background(), scope, memory.ListOptions{})
	if len(saved) != 1 {
		t.Fatalf("expected 1 saved memory, got %d", len(saved))
	}
	if saved[0].GetProvenance() != memory.ProvenanceAgentExtracted {
		t.Errorf("provenance = %q, want %q", saved[0].GetProvenance(), memory.ProvenanceAgentExtracted)
	}
}

func TestMemoryExtractionStage_PreservesExtractorProvenance(t *testing.T) {
	// If the extractor already sets provenance, extractAndSave should not overwrite it
	ext := &mockExtractor{
		extracted: []*memory.Memory{
			{Type: "fact", Content: "Operator-curated knowledge",
				Metadata: map[string]any{memory.MetaKeyProvenance: string(memory.ProvenanceOperatorCurated)}},
		},
	}
	store := memory.NewInMemoryStore()
	scope := map[string]string{"user_id": "test"}
	s := NewMemoryExtractionStage(ext, store, scope)

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	msgs := []types.Message{{Role: "user", Content: "test"}}
	input <- StreamElement{Metadata: map[string]any{"messages": msgs}}
	close(input)

	err := s.Process(context.Background(), input, output)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	<-output

	saved, _ := store.List(context.Background(), scope, memory.ListOptions{})
	if len(saved) != 1 {
		t.Fatalf("expected 1 saved memory, got %d", len(saved))
	}
	// Extractor's provenance should be preserved
	if saved[0].GetProvenance() != memory.ProvenanceOperatorCurated {
		t.Errorf("provenance = %q, want %q (should not be overwritten)",
			saved[0].GetProvenance(), memory.ProvenanceOperatorCurated)
	}
}

func TestMemoryExtractionStage_NoMessagesSkipsExtraction(t *testing.T) {
	ext := &mockExtractor{
		extracted: []*memory.Memory{
			{Type: "fact", Content: "should not be saved"},
		},
	}
	store := memory.NewInMemoryStore()
	scope := map[string]string{"user_id": "test"}
	s := NewMemoryExtractionStage(ext, store, scope)

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// No messages in metadata
	input <- StreamElement{Metadata: map[string]any{}}
	close(input)

	err := s.Process(context.Background(), input, output)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	<-output

	saved, _ := store.List(context.Background(), scope, memory.ListOptions{})
	if len(saved) != 0 {
		t.Errorf("expected 0 memories without messages, got %d", len(saved))
	}
}

func TestMemoryExtractionStage_NilExtractorPassthrough(t *testing.T) {
	s := NewMemoryExtractionStage(nil, nil, nil)

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	input <- StreamElement{Metadata: map[string]any{"test": "value"}}
	close(input)

	err := s.Process(context.Background(), input, output)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	elem := <-output
	if elem.Metadata["test"] != "value" {
		t.Error("element should pass through unchanged")
	}
}
