//go:build !voice

package voice

import (
	"errors"
	"testing"
)

func TestNewAudioIO_StubReturnsError(t *testing.T) {
	io, err := NewAudioIO()
	if io != nil {
		t.Errorf("expected nil AudioIO, got %v", io)
	}
	if !errors.Is(err, ErrVoiceNotCompiled) {
		t.Errorf("expected ErrVoiceNotCompiled, got %v", err)
	}
}
