package tts

import (
	"context"
	"testing"
)

func TestCartesiaService_SynthesizeStream_EmptyText(t *testing.T) {
	service := NewCartesia("test-key")
	_, err := service.SynthesizeStream(context.Background(), "", SynthesisConfig{})
	if err != ErrEmptyText {
		t.Errorf("SynthesizeStream() error = %v, want ErrEmptyText", err)
	}
}

func TestCartesiaService_SynthesizeStream_InvalidWSURL(t *testing.T) {
	service := NewCartesia("test-key", WithCartesiaWSURL("wss://invalid-host-that-does-not-exist:9999"))

	_, err := service.SynthesizeStream(context.Background(), "test", SynthesisConfig{})
	if err == nil {
		t.Fatal("expected error for invalid WebSocket URL")
	}

	var synthErr *SynthesisError
	if !isError(err, &synthErr) {
		t.Fatalf("expected SynthesisError, got %T", err)
	}

	if !synthErr.Retryable {
		t.Error("expected retryable error for connection failure")
	}
}
