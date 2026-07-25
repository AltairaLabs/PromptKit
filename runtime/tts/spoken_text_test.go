package tts

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
)

// These tests cover SpokenTextReporter: the text each provider actually speaks
// after markup lowering. See #1657. Every provider degrades identically on
// plain text (no tags ⇒ input returned verbatim) and each honors config.Model,
// falling back to the service's configured model when config.Model is empty.

func TestOpenAIService_SpokenText(t *testing.T) {
	t.Run("tts-1 strips tags", func(t *testing.T) {
		s := NewOpenAI("test-key")
		got := s.SpokenText("[whispers]Come here[/]Did you hear that?", SynthesisConfig{Model: ModelTTS1})
		if want := "Come hereDid you hear that?"; got != want {
			t.Errorf("SpokenText = %q, want %q", got, want)
		}
	})

	t.Run("gpt-4o-mini-tts moves tags to instructions, speaks the remainder", func(t *testing.T) {
		s := NewOpenAI("test-key")
		got := s.SpokenText("[whispers]Come here", SynthesisConfig{Model: ModelGPT4oMiniTTS})
		if want := "Come here"; got != want {
			t.Errorf("SpokenText = %q, want %q", got, want)
		}
	})

	t.Run("plain text unchanged", func(t *testing.T) {
		s := NewOpenAI("test-key")
		if got := s.SpokenText("plain text", SynthesisConfig{}); got != "plain text" {
			t.Errorf("SpokenText = %q, want %q", got, "plain text")
		}
	})

	t.Run("empty config.Model falls back to service model", func(t *testing.T) {
		s := NewOpenAI("test-key", base.WithModel(ModelGPT4oMiniTTS))
		// config.Model empty ⇒ uses the expressive service model ⇒ tags become
		// instructions and the spoken remainder is "Come here".
		if got := s.SpokenText("[whispers]Come here", SynthesisConfig{}); got != "Come here" {
			t.Errorf("SpokenText = %q, want %q", got, "Come here")
		}
	})
}

func TestCartesiaService_SpokenText(t *testing.T) {
	s := NewCartesia("test-key")

	t.Run("strips emotion markup from the transcript", func(t *testing.T) {
		if got := s.SpokenText("[excited]a[sad]b[/]c", SynthesisConfig{}); got != "abc" {
			t.Errorf("SpokenText = %q, want %q", got, "abc")
		}
	})

	t.Run("plain text unchanged", func(t *testing.T) {
		if got := s.SpokenText("plain text", SynthesisConfig{}); got != "plain text" {
			t.Errorf("SpokenText = %q, want %q", got, "plain text")
		}
	})
}

func TestElevenLabsService_SpokenText(t *testing.T) {
	t.Run("v3 keeps inline tags verbatim", func(t *testing.T) {
		s := NewElevenLabs("test-key")
		in := "[whispers]Come here[/]Did you hear that?"
		if got := s.SpokenText(in, SynthesisConfig{Model: ElevenLabsModelV3}); got != in {
			t.Errorf("SpokenText = %q, want %q (verbatim)", got, in)
		}
	})

	t.Run("non-v3 strips tags", func(t *testing.T) {
		s := NewElevenLabs("test-key")
		got := s.SpokenText("[whispers]Come here[/]Did you hear that?", SynthesisConfig{Model: ElevenLabsModelMultilingual})
		if want := "Come hereDid you hear that?"; got != want {
			t.Errorf("SpokenText = %q, want %q", got, want)
		}
	})

	t.Run("empty config.Model falls back to service model", func(t *testing.T) {
		// Service defaults to a non-v3 model, so tags are stripped.
		s := NewElevenLabs("test-key")
		got := s.SpokenText("[whispers]Come here", SynthesisConfig{})
		if want := "Come here"; got != want {
			t.Errorf("SpokenText = %q, want %q", got, want)
		}
	})

	t.Run("plain text unchanged", func(t *testing.T) {
		s := NewElevenLabs("test-key")
		if got := s.SpokenText("plain text", SynthesisConfig{}); got != "plain text" {
			t.Errorf("SpokenText = %q, want %q", got, "plain text")
		}
	})
}
