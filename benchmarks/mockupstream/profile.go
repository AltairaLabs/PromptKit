package main

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Profile holds latency configuration for all mock upstream servers.
type Profile struct {
	OpenAI OpenAIProfile `yaml:"openai"`
	STT    STTProfile    `yaml:"stt"`
	TTS    TTSProfile    `yaml:"tts"`
}

// OpenAIProfile configures the mock OpenAI SSE chat completion server.
type OpenAIProfile struct {
	ChunkCount      int           `yaml:"chunk_count"`
	InterChunkDelay time.Duration `yaml:"inter_chunk_delay"`
	FirstChunkDelay time.Duration `yaml:"first_chunk_delay"`
}

// STTProfile configures the mock speech-to-text WebSocket server.
type STTProfile struct {
	TranscriptionDelay time.Duration `yaml:"transcription_delay"`
	InterimInterval    time.Duration `yaml:"interim_interval"`
	FinalDelay         time.Duration `yaml:"final_delay"`
}

// TTSProfile configures the mock text-to-speech WebSocket server.
type TTSProfile struct {
	FirstByteDelay  time.Duration `yaml:"first_byte_delay"`
	ChunkSize       int           `yaml:"chunk_size"`
	InterChunkDelay time.Duration `yaml:"inter_chunk_delay"`
}

// DefaultProfile returns sensible default latencies suitable for local testing.
func DefaultProfile() Profile {
	return Profile{
		OpenAI: OpenAIProfile{
			ChunkCount:      20,
			InterChunkDelay: 30 * time.Millisecond,
			FirstChunkDelay: 100 * time.Millisecond,
		},
		STT: STTProfile{
			TranscriptionDelay: 100 * time.Millisecond,
			InterimInterval:    80 * time.Millisecond,
			FinalDelay:         100 * time.Millisecond,
		},
		TTS: TTSProfile{
			FirstByteDelay:  60 * time.Millisecond,
			ChunkSize:       4096,
			InterChunkDelay: 30 * time.Millisecond,
		},
	}
}

// LoadProfile reads a YAML file at path and unmarshals it into a Profile.
func LoadProfile(path string) (Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, fmt.Errorf("read profile %q: %w", path, err)
	}

	var p Profile
	if err := yaml.Unmarshal(data, &p); err != nil {
		return Profile{}, fmt.Errorf("parse profile %q: %w", path, err)
	}

	return p, nil
}
