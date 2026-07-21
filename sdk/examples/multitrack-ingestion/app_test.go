package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
)

func TestKeyless_LabelsTranscriptAndFiresAgentPerTurn(t *testing.T) {
	provider := mock.NewProviderWithRepository("mock", "mock-model", false,
		mock.NewInMemoryMockRepository("Noted."))

	var mu sync.Mutex
	var transcripts []string
	onTranscript := func(speaker, text string) {
		mu.Lock()
		transcripts = append(transcripts, speaker+": "+text)
		mu.Unlock()
	}

	conv, err := newConversation(providers.Provider(provider), onTranscript)
	if err != nil {
		t.Fatalf("newConversation: %v", err)
	}
	defer func() { _ = conv.Close() }()

	respCh, err := conv.Response()
	if err != nil {
		t.Fatalf("Response: %v", err)
	}

	var replyMu sync.Mutex
	replies := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		for chunk := range respCh {
			// On this path each turn's response arrives as exactly one non-empty
			// chunk, so a non-empty chunk == one agent firing.
			if chunk.Content != "" {
				replyMu.Lock()
				replies++
				replyMu.Unlock()
			}
		}
	}()

	if err := feed(context.Background(), conv); err != nil {
		t.Fatalf("feed: %v", err)
	}

	// Close drains the session; wait for the response goroutine to finish so
	// both the transcript and the reply count are final before asserting (the
	// pipeline processes turns asynchronously after feed returns).
	_ = conv.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("response channel did not drain within 3s")
	}

	// Each track's lines must appear, prefixed with the right speaker label.
	mu.Lock()
	joined := strings.Join(transcripts, "\n")
	mu.Unlock()
	for _, tn := range script {
		want := tn.Speaker + ": " + tn.Line
		if !strings.Contains(joined, want) {
			t.Errorf("missing labeled transcript %q in:\n%s", want, joined)
		}
	}

	// The agent must fire once per turn.
	replyMu.Lock()
	defer replyMu.Unlock()
	if replies != len(script) {
		t.Errorf("expected %d per-turn replies, got %d", len(script), replies)
	}
}
