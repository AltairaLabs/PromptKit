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
	turnState := NewTurnState()
	s := NewMemoryRetrievalStageWithTurnState(ret, store, nil, turnState)

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	msg := types.Message{Role: "user", Content: "hello"}
	input <- StreamElement{Message: &msg}
	close(input)

	err := s.Process(context.Background(), input, output)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	<-output // consume forwarded element

	if turnState.Variables["memory_context"] == "" {
		t.Error("memory_context should be injected onto TurnState.Variables")
	}
}

func TestMemoryRetrievalStage_NilRetrieverPassthrough(t *testing.T) {
	s := NewMemoryRetrievalStage(nil, nil, nil)

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	msg := types.Message{Role: "user", Content: "test"}
	input <- StreamElement{Message: &msg}
	close(input)

	err := s.Process(context.Background(), input, output)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	elem := <-output
	if elem.Message.Content != "test" {
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

	input := make(chan StreamElement, 2)
	output := make(chan StreamElement, 2)

	user := types.Message{Role: "user", Content: "I love Go"}
	assistant := types.Message{Role: "assistant", Content: "Great choice!"}
	input <- StreamElement{Message: &user}
	input <- StreamElement{Message: &assistant}
	close(input)

	err := s.Process(context.Background(), input, output)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	<-output
	<-output

	saved, _ := store.List(context.Background(), scope, memory.ListOptions{})
	if len(saved) != 1 {
		t.Fatalf("expected 1 saved memory, got %d", len(saved))
	}
	if saved[0].GetProvenance() != memory.ProvenanceAgentExtracted {
		t.Errorf("provenance = %q, want %q", saved[0].GetProvenance(), memory.ProvenanceAgentExtracted)
	}
}

func TestMemoryExtractionStage_PreservesExtractorProvenance(t *testing.T) {
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

	msg := types.Message{Role: "user", Content: "test"}
	input <- StreamElement{Message: &msg}
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

	// Element with no message — extraction should skip
	input <- StreamElement{}
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

	msg := types.Message{Role: "user", Content: "test"}
	input <- StreamElement{Message: &msg}
	close(input)

	err := s.Process(context.Background(), input, output)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	elem := <-output
	if elem.Message.Content != "test" {
		t.Error("element should pass through unchanged")
	}
}
