package main

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
)

func TestScriptedSTT_ReplaysSourceLinesInOrder(t *testing.T) {
	stt := newScriptedSTT("speaker-a")
	var got []string
	for range script {
		resp, err := stt.Transcribe(context.Background(), base.STTRequest{})
		if err != nil {
			t.Fatalf("Transcribe: %v", err)
		}
		if resp.Text != "" {
			got = append(got, resp.Text)
		}
	}
	var want []string
	for _, tn := range script {
		if tn.Source == "speaker-a" {
			want = append(want, tn.Line)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d: %v vs %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestSynthTurnPCM_IsLoudThenSilent(t *testing.T) {
	pcm := synthTurnPCM()
	if len(pcm) < 8000 {
		t.Fatalf("expected a multi-frame turn, got %d bytes", len(pcm))
	}
	if len(pcm)%2 != 0 {
		t.Fatalf("PCM16 must be an even byte count, got %d", len(pcm))
	}
}
