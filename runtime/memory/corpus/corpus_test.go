package corpus_test

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/memory"
	"github.com/AltairaLabs/PromptKit/runtime/memory/corpus"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

var docs = []corpus.Document{
	{ID: "refunds", Title: "Refund policy", Text: "Refunds are issued within 14 days of purchase."},
	{ID: "shipping", Title: "Shipping", Text: "Orders ship next business day by courier."},
	{ID: "returns", Title: "Returns", Text: "Return shipping is free for faulty goods."},
}

func userTurn(text string) []types.Message {
	return []types.Message{{Role: "user", Content: text}}
}

// compile-time proof the corpus satisfies the ambient-injection interface.
var _ memory.Retriever = (*corpus.Retriever)(nil)

func TestRetriever_ReturnsDocumentMatchingTheUserTurn(t *testing.T) {
	r := corpus.New(docs)

	got, err := r.RetrieveContext(context.Background(), nil, userTurn("how long do refunds take?"))
	if err != nil {
		t.Fatalf("RetrieveContext: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("want 1 match, got %d", len(got))
	}
	if got[0].Content != docs[0].Text {
		t.Errorf("want the refund document, got %q", got[0].Content)
	}
}

func TestRetriever_RanksMoreOverlapFirst(t *testing.T) {
	r := corpus.New(docs)

	got, err := r.RetrieveContext(context.Background(), nil,
		userTurn("is return shipping free?"))
	if err != nil {
		t.Fatalf("RetrieveContext: %v", err)
	}

	if len(got) < 2 {
		t.Fatalf("want both shipping documents, got %d", len(got))
	}
	if got[0].Content != docs[2].Text {
		t.Errorf("want the returns document ranked first, got %q", got[0].Content)
	}
}

func TestRetriever_NoOverlapReturnsNothing(t *testing.T) {
	r := corpus.New(docs)

	got, err := r.RetrieveContext(context.Background(), nil, userTurn("what is the weather"))
	if err != nil {
		t.Fatalf("RetrieveContext: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want no matches for an unrelated turn, got %d", len(got))
	}
}

func TestRetriever_TopKCapsResults(t *testing.T) {
	r := corpus.New(docs, corpus.WithTopK(1))

	got, err := r.RetrieveContext(context.Background(), nil,
		userTurn("shipping refunds returns"))
	if err != nil {
		t.Fatalf("RetrieveContext: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("want TopK to cap results at 1, got %d", len(got))
	}
}

func TestRetriever_NoUserTurnReturnsNothing(t *testing.T) {
	r := corpus.New(docs)

	got, err := r.RetrieveContext(context.Background(), nil, []types.Message{
		{Role: "assistant", Content: "refunds shipping returns"},
	})
	if err != nil {
		t.Fatalf("RetrieveContext: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want no matches without a user turn, got %d", len(got))
	}
}

func TestRetriever_CarriesDocumentIdentityForCitation(t *testing.T) {
	r := corpus.New(docs)

	got, err := r.RetrieveContext(context.Background(), nil, userTurn("refunds"))
	if err != nil {
		t.Fatalf("RetrieveContext: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("want a match")
	}

	if got[0].Type != corpus.MemoryType {
		t.Errorf("want type %q, got %q", corpus.MemoryType, got[0].Type)
	}
	if got[0].ID != "refunds" {
		t.Errorf("want the document ID preserved, got %q", got[0].ID)
	}
	if got[0].Metadata["title"] != "Refund policy" {
		t.Errorf("want the title in metadata, got %v", got[0].Metadata["title"])
	}
}

func TestRetriever_ReadsUserTextFromParts(t *testing.T) {
	r := corpus.New(docs)

	text := "refunds please"
	msg := types.Message{Role: "user", Parts: []types.ContentPart{{Type: "text", Text: &text}}}

	got, err := r.RetrieveContext(context.Background(), nil, []types.Message{msg})
	if err != nil {
		t.Fatalf("RetrieveContext: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want the refund document from message parts, got %d", len(got))
	}
}
