// Package replay provides a provider that replays recorded sessions deterministically.
package replay

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Constants for streaming configuration.
const (
	roleAssistant           = "assistant"
	finishReasonStop        = "stop"
	finishReasonComplete    = "complete"
	streamChannelBufferSize = 100
	preferredSampleRate     = 24000
	defaultChunkDelayMs     = 100
)

// ErrSessionClosed is returned when attempting to use a closed session.
var ErrSessionClosed = errors.New("session is closed")

// Ensure Provider implements StreamInputSupport.
var _ providers.StreamInputSupport = (*StreamingProvider)(nil)

// StreamingProvider extends Provider with streaming support for duplex replay.
type StreamingProvider struct {
	*Provider
	baseDir  string // Base directory for resolving relative paths
	messages []arenaMessage
}

// arenaMessage represents a message from arena output JSON.
type arenaMessage struct {
	Role     string             `json:"role"`
	Content  string             `json:"content"`
	Parts    []arenaContentPart `json:"parts,omitempty"`
	CostInfo *types.CostInfo    `json:"cost_info,omitempty"`
	Meta     map[string]any     `json:"meta,omitempty"`
}

// arenaContentPart represents a content part from arena output.
type arenaContentPart struct {
	Type  string      `json:"type"`
	Text  string      `json:"text,omitempty"`
	Media *arenaMedia `json:"media,omitempty"`
}

// arenaMedia represents media content from arena output.
type arenaMedia struct {
	StorageReference string `json:"storage_reference,omitempty"`
	MIMEType         string `json:"mime_type,omitempty"`
	Data             string `json:"data,omitempty"`
}

// arenaOutput represents the arena run output JSON structure.
type arenaOutput struct {
	RunID      string         `json:"RunID"`
	ScenarioID string         `json:"ScenarioID"`
	ProviderID string         `json:"ProviderID"`
	Messages   []arenaMessage `json:"Messages"`
	Params     struct {
		SystemPrompt string `json:"system_prompt"`
	} `json:"Params"`
	Cost      *types.CostInfo `json:"Cost,omitempty"`
	StartTime time.Time       `json:"StartTime"`
	EndTime   time.Time       `json:"EndTime"`
}

// NewStreamingProviderFromArenaOutput creates a streaming replay provider from an arena output file.
func NewStreamingProviderFromArenaOutput(path string, cfg *Config) (*StreamingProvider, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is user-provided
	if err != nil {
		return nil, fmt.Errorf("read arena output: %w", err)
	}

	var output arenaOutput
	if err := json.Unmarshal(data, &output); err != nil {
		return nil, fmt.Errorf("parse arena output: %w", err)
	}

	if len(output.Messages) == 0 {
		return nil, fmt.Errorf("arena output contains no messages")
	}

	// Create a base provider (it won't be used for streaming but provides the interface)
	config := DefaultConfig()
	if cfg != nil {
		config = *cfg
	}

	p := &Provider{
		id:         "replay-streaming",
		config:     config,
		contentMap: make(map[string]int),
	}

	return &StreamingProvider{
		Provider: p,
		baseDir:  filepath.Dir(path),
		messages: output.Messages,
	}, nil
}

// CreateStreamSession creates a new bidirectional streaming session for replay.
func (p *StreamingProvider) CreateStreamSession(
	ctx context.Context,
	req *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	// Extract assistant messages for replay
	var assistantMsgs []arenaMessage
	for _, msg := range p.messages {
		if msg.Role == roleAssistant {
			assistantMsgs = append(assistantMsgs, msg)
		}
	}

	if len(assistantMsgs) == 0 {
		return nil, fmt.Errorf("no assistant messages found for replay")
	}

	session := &StreamSession{
		provider:     p,
		config:       p.config,
		messages:     assistantMsgs,
		baseDir:      p.baseDir,
		responseChan: make(chan providers.StreamChunk, streamChannelBufferSize),
		doneChan:     make(chan struct{}),
	}

	return session, nil
}

// SupportsStreamInput returns the media types supported for streaming input.
func (p *StreamingProvider) SupportsStreamInput() []string {
	return []string{types.ContentTypeAudio}
}

// GetStreamingCapabilities returns detailed information about streaming support.
func (p *StreamingProvider) GetStreamingCapabilities() providers.StreamingCapabilities {
	return providers.StreamingCapabilities{
		SupportedMediaTypes: []string{types.ContentTypeAudio},
		Audio: &providers.AudioStreamingCapabilities{
			SupportedEncodings:   []string{"pcm", "pcm_linear16", "wav"},
			SupportedSampleRates: []int{16000, preferredSampleRate},
			SupportedChannels:    []int{1},
			PreferredEncoding:    "pcm",
			PreferredSampleRate:  preferredSampleRate,
		},
		BidirectionalSupport: true,
	}
}

// StreamSession implements StreamInputSession for replaying recorded sessions.
type StreamSession struct {
	provider     *StreamingProvider
	config       Config
	messages     []arenaMessage
	baseDir      string
	responseChan chan providers.StreamChunk
	doneChan     chan struct{}

	mu           sync.Mutex
	turnIndex    int
	closed       bool
	inputCount   int
	sessionError error

	sendWg sync.WaitGroup // Tracks active sends to safely close channels
}

// SendChunk receives input chunks and triggers replay of the next response.
func (s *StreamSession) SendChunk(ctx context.Context, chunk *types.MediaChunk) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrSessionClosed
	}
	s.inputCount++
	s.mu.Unlock()

	return nil
}

// SendText receives text input and triggers replay of the next response.
func (s *StreamSession) SendText(ctx context.Context, text string) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrSessionClosed
	}

	// Find the next response to send
	if s.turnIndex >= len(s.messages) {
		s.mu.Unlock()
		// No more responses - send end signal
		finishReason := finishReasonStop
		s.safeSend(&providers.StreamChunk{
			FinishReason: &finishReason,
		})
		return nil
	}

	msg := s.messages[s.turnIndex]
	s.turnIndex++
	s.mu.Unlock()

	// Send the response
	return s.sendMessage(ctx, msg)
}

// SendSystemContext sends system context (ignored for replay).
func (s *StreamSession) SendSystemContext(ctx context.Context, text string) error {
	// Ignored for replay
	return nil
}

// sendMessage sends a recorded message as stream chunks.
func (s *StreamSession) sendMessage(ctx context.Context, msg arenaMessage) error {
	if err := s.applyTimingDelay(ctx); err != nil {
		return err
	}

	s.sendTextContent(msg)
	s.sendAudioParts(msg)
	s.sendCompletionChunk(msg)

	return nil
}

// applyTimingDelay applies the configured timing delay between responses.
func (s *StreamSession) applyTimingDelay(ctx context.Context) error {
	if s.config.Timing == TimingInstant {
		return nil
	}

	delay := defaultChunkDelayMs * time.Millisecond
	if s.config.Timing == TimingAccelerated && s.config.Speed > 0 {
		delay = time.Duration(float64(delay) / s.config.Speed)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

// safeSend safely sends a chunk to the response channel, returning false if session is closed.
func (s *StreamSession) safeSend(chunk *providers.StreamChunk) bool {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return false
	}
	// Register as an active send while holding the lock
	s.sendWg.Add(1)
	s.mu.Unlock()

	defer s.sendWg.Done()

	// Use a select with doneChan to avoid blocking on a closed channel
	select {
	case <-s.doneChan:
		return false
	case s.responseChan <- *chunk:
		return true
	}
}

// sendTextContent sends the text content of a message.
func (s *StreamSession) sendTextContent(msg arenaMessage) {
	if msg.Content != "" {
		s.safeSend(&providers.StreamChunk{
			Content: msg.Content,
			Delta:   msg.Content,
		})
	}
}

// sendAudioParts sends any audio parts from the message.
func (s *StreamSession) sendAudioParts(msg arenaMessage) {
	for _, part := range msg.Parts {
		if part.Type != types.ContentTypeAudio || part.Media == nil {
			continue
		}

		audioData, err := s.loadAudioData(part.Media)
		if err != nil {
			// Log but don't fail - audio might be optional
			continue
		}

		audioStr := string(audioData)
		if !s.safeSend(&providers.StreamChunk{
			MediaDelta: &types.MediaContent{
				MIMEType: part.Media.MIMEType,
				Data:     &audioStr,
			},
		}) {
			return // Session closed, stop sending
		}
	}
}

// sendCompletionChunk sends the completion signal for a turn.
func (s *StreamSession) sendCompletionChunk(msg arenaMessage) {
	finishReason := finishReasonComplete
	if fr, ok := msg.Meta["finish_reason"].(string); ok {
		finishReason = fr
	}

	chunk := &providers.StreamChunk{
		FinishReason: &finishReason,
	}

	if msg.CostInfo != nil {
		chunk.CostInfo = msg.CostInfo
	}

	s.safeSend(chunk)
}

// loadAudioData loads audio data from a storage reference or inline data.
func (s *StreamSession) loadAudioData(media *arenaMedia) ([]byte, error) {
	// Try inline data first
	if media.Data != "" {
		return base64.StdEncoding.DecodeString(media.Data)
	}

	// Load from storage reference
	if media.StorageReference != "" {
		path := media.StorageReference
		if !filepath.IsAbs(path) {
			path = filepath.Join(s.baseDir, path)
		}

		file, err := os.Open(path) //nolint:gosec // path is user-provided
		if err != nil {
			return nil, fmt.Errorf("open audio file: %w", err)
		}
		defer file.Close()

		data, err := io.ReadAll(file)
		if err != nil {
			return nil, fmt.Errorf("read audio file: %w", err)
		}

		return data, nil
	}

	return nil, fmt.Errorf("no audio data source")
}

// Response returns the response channel.
func (s *StreamSession) Response() <-chan providers.StreamChunk {
	return s.responseChan
}

// Close ends the streaming session.
func (s *StreamSession) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}

	s.closed = true
	// Close doneChan first so safeSend operations will abort
	close(s.doneChan)
	s.mu.Unlock()

	// Wait for any in-flight sends to complete before closing responseChan
	s.sendWg.Wait()

	close(s.responseChan)

	return nil
}

// Error returns any session error.
func (s *StreamSession) Error() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionError
}

// Done returns a channel that closes when the session ends.
func (s *StreamSession) Done() <-chan struct{} {
	return s.doneChan
}

// TriggerNextResponse manually triggers the next response (for testing).
func (s *StreamSession) TriggerNextResponse(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrSessionClosed
	}

	if s.turnIndex >= len(s.messages) {
		s.mu.Unlock()
		return fmt.Errorf("no more responses")
	}

	msg := s.messages[s.turnIndex]
	s.turnIndex++
	s.mu.Unlock()

	return s.sendMessage(ctx, msg)
}

// RemainingTurns returns the number of responses left to replay.
func (s *StreamSession) RemainingTurns() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages) - s.turnIndex
}

// EndInput signals the end of input and triggers the next response.
// This implements the EndInputter interface expected by DuplexProviderStage.
func (s *StreamSession) EndInput() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}

	// No more responses available
	if s.turnIndex >= len(s.messages) {
		s.mu.Unlock()
		return
	}

	msg := s.messages[s.turnIndex]
	s.turnIndex++
	s.mu.Unlock()

	// Send the response in a goroutine to avoid blocking
	go func() {
		_ = s.sendMessage(context.Background(), msg)
	}()
}
