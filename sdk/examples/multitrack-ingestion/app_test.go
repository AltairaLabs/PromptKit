package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
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

	// feed returns as soon as the last chunk is queued; the pipeline is still
	// processing turns. Close cancels the session context rather than draining
	// it, so closing here races the in-flight turns and intermittently kills
	// the last one mid-STT ("stage=speaker-b_stt error=context canceled",
	// ~20% of runs). Wait for every turn's reply first, then close.
	deadline := time.After(5 * time.Second)
	for {
		replyMu.Lock()
		got := replies
		replyMu.Unlock()
		if got >= len(script) {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("only %d of %d per-turn replies arrived within 5s", got, len(script))
		case <-time.After(10 * time.Millisecond):
		}
	}

	// Now that every turn has been answered, close and let the response
	// goroutine finish so the transcript is final before asserting.
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
