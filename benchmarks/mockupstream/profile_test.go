package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultProfile(t *testing.T) {
	p := DefaultProfile()

	if p.OpenAI.ChunkCount == 0 {
		t.Error("OpenAI.ChunkCount should be non-zero")
	}
	if p.OpenAI.InterChunkDelay == 0 {
		t.Error("OpenAI.InterChunkDelay should be non-zero")
	}
	if p.OpenAI.FirstChunkDelay == 0 {
		t.Error("OpenAI.FirstChunkDelay should be non-zero")
	}
	if p.STT.TranscriptionDelay == 0 {
		t.Error("STT.TranscriptionDelay should be non-zero")
	}
	if p.STT.InterimInterval == 0 {
		t.Error("STT.InterimInterval should be non-zero")
	}
	if p.STT.FinalDelay == 0 {
		t.Error("STT.FinalDelay should be non-zero")
	}
	if p.TTS.FirstByteDelay == 0 {
		t.Error("TTS.FirstByteDelay should be non-zero")
	}
	if p.TTS.ChunkSize == 0 {
		t.Error("TTS.ChunkSize should be non-zero")
	}
	if p.TTS.InterChunkDelay == 0 {
		t.Error("TTS.InterChunkDelay should be non-zero")
	}
}

func TestLoadProfile_Fast(t *testing.T) {
	yaml := `
openai:
  chunk_count: 10
  inter_chunk_delay: 10ms
  first_chunk_delay: 10ms
stt:
  transcription_delay: 10ms
  interim_interval: 10ms
  final_delay: 10ms
tts:
  first_byte_delay: 10ms
  chunk_size: 4096
  inter_chunk_delay: 10ms
`
	tmp := filepath.Join(t.TempDir(), "fast.yaml")
	if err := os.WriteFile(tmp, []byte(yaml), 0600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	p, err := LoadProfile(tmp)
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}

	if p.OpenAI.ChunkCount != 10 {
		t.Errorf("OpenAI.ChunkCount: got %d, want 10", p.OpenAI.ChunkCount)
	}
	if p.OpenAI.InterChunkDelay != 10*time.Millisecond {
		t.Errorf("OpenAI.InterChunkDelay: got %v, want 10ms", p.OpenAI.InterChunkDelay)
	}
	if p.OpenAI.FirstChunkDelay != 10*time.Millisecond {
		t.Errorf("OpenAI.FirstChunkDelay: got %v, want 10ms", p.OpenAI.FirstChunkDelay)
	}
	if p.STT.TranscriptionDelay != 10*time.Millisecond {
		t.Errorf("STT.TranscriptionDelay: got %v, want 10ms", p.STT.TranscriptionDelay)
	}
	if p.TTS.ChunkSize != 4096 {
		t.Errorf("TTS.ChunkSize: got %d, want 4096", p.TTS.ChunkSize)
	}
	if p.TTS.FirstByteDelay != 10*time.Millisecond {
		t.Errorf("TTS.FirstByteDelay: got %v, want 10ms", p.TTS.FirstByteDelay)
	}
}

func TestLoadProfile_Realistic(t *testing.T) {
	yaml := `
openai:
  chunk_count: 30
  inter_chunk_delay: 30ms
  first_chunk_delay: 200ms
stt:
  transcription_delay: 150ms
  interim_interval: 100ms
  final_delay: 150ms
tts:
  first_byte_delay: 80ms
  chunk_size: 4096
  inter_chunk_delay: 30ms
`
	tmp := filepath.Join(t.TempDir(), "realistic.yaml")
	if err := os.WriteFile(tmp, []byte(yaml), 0600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	p, err := LoadProfile(tmp)
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}

	if p.OpenAI.FirstChunkDelay != 200*time.Millisecond {
		t.Errorf("OpenAI.FirstChunkDelay: got %v, want 200ms", p.OpenAI.FirstChunkDelay)
	}
	if p.OpenAI.InterChunkDelay != 30*time.Millisecond {
		t.Errorf("OpenAI.InterChunkDelay: got %v, want 30ms", p.OpenAI.InterChunkDelay)
	}
	if p.OpenAI.ChunkCount != 30 {
		t.Errorf("OpenAI.ChunkCount: got %d, want 30", p.OpenAI.ChunkCount)
	}
	if p.STT.TranscriptionDelay != 150*time.Millisecond {
		t.Errorf("STT.TranscriptionDelay: got %v, want 150ms", p.STT.TranscriptionDelay)
	}
	if p.TTS.FirstByteDelay != 80*time.Millisecond {
		t.Errorf("TTS.FirstByteDelay: got %v, want 80ms", p.TTS.FirstByteDelay)
	}
	if p.TTS.InterChunkDelay != 30*time.Millisecond {
		t.Errorf("TTS.InterChunkDelay: got %v, want 30ms", p.TTS.InterChunkDelay)
	}
}

func TestLoadProfile_NotFound(t *testing.T) {
	_, err := LoadProfile("/nonexistent/path/profile.yaml")
	if err == nil {
		t.Error("LoadProfile should return error for missing file")
	}
}
