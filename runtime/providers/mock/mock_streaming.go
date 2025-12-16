package mock

import (
	"context"
	"errors"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	defaultResponseBufferSize = 10
	hd720Width                = 1280
	hd720Height               = 720
	fullHDWidth               = 1920
	fullHDHeight              = 1080
	errSessionClosed          = "session closed"
)

// MockStreamSession implements providers.StreamInputSession for testing duplex scenarios.
//
//nolint:revive // MockStreamSession naming is intentional for clarity in test usage
type MockStreamSession struct {
	chunks      []*types.MediaChunk
	texts       []string
	responses   chan providers.StreamChunk
	doneCh      chan struct{}
	err         error
	closeCalled bool
	mu          sync.Mutex

	// Configurable behavior for testing
	sendChunkErr   error
	sendTextErr    error
	autoRespond    bool                    // If true, automatically send responses when receiving input
	responseText   string                  // Text to auto-respond with
	responseChunks []providers.StreamChunk // Custom response chunks to emit
}

// NewMockStreamSession creates a new mock stream session.
func NewMockStreamSession() *MockStreamSession {
	return &MockStreamSession{
		chunks:       make([]*types.MediaChunk, 0),
		texts:        make([]string, 0),
		responses:    make(chan providers.StreamChunk, defaultResponseBufferSize),
		doneCh:       make(chan struct{}),
		autoRespond:  false,
		responseText: "Mock response",
	}
}

// WithAutoRespond configures the session to automatically respond to inputs.
func (m *MockStreamSession) WithAutoRespond(text string) *MockStreamSession {
	m.autoRespond = true
	m.responseText = text
	return m
}

// WithResponseChunks configures custom response chunks to emit.
func (m *MockStreamSession) WithResponseChunks(chunks []providers.StreamChunk) *MockStreamSession {
	m.responseChunks = chunks
	return m
}

// WithSendChunkError configures SendChunk to return an error.
func (m *MockStreamSession) WithSendChunkError(err error) *MockStreamSession {
	m.sendChunkErr = err
	return m
}

// WithSendTextError configures SendText to return an error.
func (m *MockStreamSession) WithSendTextError(err error) *MockStreamSession {
	m.sendTextErr = err
	return m
}

// WithError sets the error returned by Error().
func (m *MockStreamSession) WithError(err error) *MockStreamSession {
	m.err = err
	return m
}

// SendChunk implements StreamInputSession.SendChunk.
func (m *MockStreamSession) SendChunk(ctx context.Context, chunk *types.MediaChunk) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closeCalled {
		return errors.New(errSessionClosed)
	}
	if m.sendChunkErr != nil {
		return m.sendChunkErr
	}

	m.chunks = append(m.chunks, chunk)

	if m.autoRespond {
		m.emitAutoResponse()
	}

	return nil
}

// SendText implements StreamInputSession.SendText.
func (m *MockStreamSession) SendText(ctx context.Context, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closeCalled {
		return errors.New(errSessionClosed)
	}
	if m.sendTextErr != nil {
		return m.sendTextErr
	}

	m.texts = append(m.texts, text)

	if m.autoRespond {
		m.emitAutoResponse()
	}

	return nil
}

// SendSystemContext implements StreamInputSession.SendSystemContext.
// Unlike SendText, this does NOT trigger a response from the model.
func (m *MockStreamSession) SendSystemContext(ctx context.Context, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closeCalled {
		return errors.New(errSessionClosed)
	}
	if m.sendTextErr != nil {
		return m.sendTextErr
	}

	// Store system context separately or with texts (for testing verification)
	m.texts = append(m.texts, "[CONTEXT] "+text)

	// Note: Unlike SendText, we do NOT emit auto-response for system context
	// because system context should not trigger immediate responses

	return nil
}

// Response implements StreamInputSession.Response.
func (m *MockStreamSession) Response() <-chan providers.StreamChunk {
	return m.responses
}

// Close implements StreamInputSession.Close.
func (m *MockStreamSession) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.closeCalled {
		m.closeCalled = true
		close(m.doneCh)
		close(m.responses)
	}
	return nil
}

// Error implements StreamInputSession.Error.
func (m *MockStreamSession) Error() error {
	return m.err
}

// Done implements StreamInputSession.Done.
func (m *MockStreamSession) Done() <-chan struct{} {
	return m.doneCh
}

// EmitChunk sends a response chunk (for testing).
func (m *MockStreamSession) EmitChunk(chunk *providers.StreamChunk) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closeCalled {
		m.responses <- *chunk
	}
}

// GetChunks returns all received media chunks (for testing).
func (m *MockStreamSession) GetChunks() []*types.MediaChunk {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.chunks
}

// GetTexts returns all received text messages (for testing).
func (m *MockStreamSession) GetTexts() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.texts
}

// emitAutoResponse sends configured response chunks (must be called with lock held).
func (m *MockStreamSession) emitAutoResponse() {
	if len(m.responseChunks) > 0 {
		// Emit custom chunks
		for i := range m.responseChunks {
			m.responses <- m.responseChunks[i]
		}
	} else {
		// Emit simple text response
		finishReason := "stop"
		chunk := providers.StreamChunk{
			Content:      m.responseText,
			Delta:        m.responseText,
			FinishReason: &finishReason,
		}
		select {
		case m.responses <- chunk:
			// Sent successfully
		default:
			// Channel full or closed - this shouldn't happen with buffered channel
		}
	}
}

// StreamingProvider extends Provider with StreamInputSupport for duplex testing.
type StreamingProvider struct {
	*Provider
	sessions         []*MockStreamSession // Track all created sessions
	createSessionErr error
	mu               sync.Mutex
}

// NewStreamingProvider creates a mock provider with duplex streaming support.
func NewStreamingProvider(id, model string, includeRawOutput bool) *StreamingProvider {
	return &StreamingProvider{
		Provider: NewProvider(id, model, includeRawOutput),
		sessions: make([]*MockStreamSession, 0),
	}
}

// NewStreamingProviderWithRepository creates a mock streaming provider with a custom repository.
func NewStreamingProviderWithRepository(
	id, model string,
	includeRawOutput bool,
	repo ResponseRepository,
) *StreamingProvider {
	return &StreamingProvider{
		Provider: NewProviderWithRepository(id, model, includeRawOutput, repo),
		sessions: make([]*MockStreamSession, 0),
	}
}

// WithCreateSessionError configures CreateStreamSession to return an error.
func (p *StreamingProvider) WithCreateSessionError(err error) *StreamingProvider {
	p.createSessionErr = err
	return p
}

// CreateStreamSession implements StreamInputSupport.CreateStreamSession.
func (p *StreamingProvider) CreateStreamSession(
	ctx context.Context,
	req *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	if p.createSessionErr != nil {
		return nil, p.createSessionErr
	}
	// Create a new session and track it (no auto-respond by default for testing)
	session := NewMockStreamSession()

	p.mu.Lock()
	p.sessions = append(p.sessions, session)
	p.mu.Unlock()

	return session, nil
}

// SupportsStreamInput implements StreamInputSupport.SupportsStreamInput.
func (p *StreamingProvider) SupportsStreamInput() []string {
	return []string{types.ContentTypeAudio, types.ContentTypeVideo}
}

// GetStreamingCapabilities implements StreamInputSupport.GetStreamingCapabilities.
func (p *StreamingProvider) GetStreamingCapabilities() providers.StreamingCapabilities {
	return providers.StreamingCapabilities{
		SupportedMediaTypes: []string{types.ContentTypeAudio, types.ContentTypeVideo},
		Audio: &providers.AudioStreamingCapabilities{
			SupportedEncodings:   []string{"pcm", "opus"},
			SupportedSampleRates: []int{16000, 24000, 48000},
			SupportedChannels:    []int{1, 2},
		},
		Video: &providers.VideoStreamingCapabilities{
			SupportedEncodings: []string{"h264", "vp8"},
			SupportedResolutions: []providers.VideoResolution{
				{Width: hd720Width, Height: hd720Height},
				{Width: fullHDWidth, Height: fullHDHeight},
			},
		},
	}
}

// GetSession returns the first/most recent mock session for testing access to sent chunks/texts.
// For multiple sessions, use GetSessions() instead.
func (p *StreamingProvider) GetSession() *MockStreamSession {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.sessions) == 0 {
		return nil
	}
	return p.sessions[len(p.sessions)-1]
}

// GetSessions returns all created sessions for testing.
func (p *StreamingProvider) GetSessions() []*MockStreamSession {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sessions
}
