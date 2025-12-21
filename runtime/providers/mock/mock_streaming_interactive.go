package mock

import (
	"context"
	"errors"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
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

	// DefaultMockStreamingResponse is the default response text for auto-respond mode.
	DefaultMockStreamingResponse = "Mock streaming response"

	// logPreviewMaxLen is the max length for text preview in debug logs.
	logPreviewMaxLen = 20
	// partialTextDivisor is used to calculate partial text length (half the response).
	partialTextDivisor = 2
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
	sendChunkErr       error
	sendTextErr        error
	autoRespond        bool                    // If true, automatically send responses when receiving input
	responseText       string                  // Text to auto-respond with
	responseChunks     []providers.StreamChunk // Custom response chunks to emit
	closeAfterResponse bool                    // If true, close response channel after auto-responding
	responseCount      int                     // Track number of responses sent

	// Simulation: Interruption behavior (mimics Gemini detecting user speech during response)
	interruptOnTurn int  // If > 0, simulate interruption on this turn number (1-indexed)
	interrupted     bool // Track if current turn was interrupted

	// Simulation: Session closure (mimics Gemini dropping connection unexpectedly)
	closeAfterTurns int  // If > 0, close session after this many turns
	closeNoResponse bool // If true with closeAfterTurns, close WITHOUT sending final response
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
// The session stays open to handle multiple turns - call Close() when done.
func (m *MockStreamSession) WithAutoRespond(text string) *MockStreamSession {
	m.autoRespond = true
	m.responseText = text
	m.closeAfterResponse = false // Keep session open for multiple turns
	return m
}

// WithCloseAfterResponse configures whether to close the response channel after auto-responding.
func (m *MockStreamSession) WithCloseAfterResponse(closeAfter bool) *MockStreamSession {
	m.closeAfterResponse = closeAfter
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

// WithInterruptOnTurn configures the session to simulate an interruption on a specific turn.
// This mimics Gemini detecting user speech while the model is responding.
// The turn is 1-indexed (first turn = 1).
func (m *MockStreamSession) WithInterruptOnTurn(turnNumber int) *MockStreamSession {
	m.interruptOnTurn = turnNumber
	return m
}

// WithCloseAfterTurns configures the session to close unexpectedly after N turns.
// This simulates Gemini dropping the connection mid-conversation.
// If noResponse is true, the session closes WITHOUT sending the final response
// (mimics Gemini closing after interrupted turnComplete).
func (m *MockStreamSession) WithCloseAfterTurns(turns int, noResponse ...bool) *MockStreamSession {
	m.closeAfterTurns = turns
	if len(noResponse) > 0 && noResponse[0] {
		m.closeNoResponse = true
	}
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

	// Don't log every chunk - too noisy
	// Only respond once per turn (not on every chunk)
	// We respond when EndInput is called, not on each SendChunk

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

// EndInput signals the end of input for the current turn.
// For mock sessions with auto-respond enabled, this triggers the response.
func (m *MockStreamSession) EndInput() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.autoRespond && !m.closeCalled {
		logger.Debug("MockStreamSession.EndInput: emitting auto-response",
			"chunks_received", len(m.chunks))
		m.emitAutoResponse()
	}
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
	currentTurn := m.responseCount + 1 // 1-indexed turn number

	// Check if we should close WITHOUT responding (simulates Gemini closing after interruption)
	if m.closeNoResponse && m.closeAfterTurns > 0 && currentTurn >= m.closeAfterTurns {
		logger.Info("MockStreamSession: SIMULATING GEMINI CLOSURE - closing WITHOUT response",
			"turn", currentTurn)
		m.responseCount++
		m.closeCalled = true
		close(m.doneCh)
		close(m.responses)
		return
	}

	// Check if we should simulate an interruption on this turn
	shouldInterrupt := m.interruptOnTurn > 0 && currentTurn == m.interruptOnTurn

	if shouldInterrupt {
		// Simulate interruption: emit partial response, then interrupted flag, then empty turnComplete
		logger.Debug("MockStreamSession: simulating interruption",
			"turn", currentTurn,
			"partialText", m.responseText[:min(len(m.responseText), logPreviewMaxLen)])

		// 1. Emit partial content before interruption
		partialText := m.responseText[:min(len(m.responseText), len(m.responseText)/partialTextDivisor)]
		partialChunk := providers.StreamChunk{
			Content: partialText,
			Delta:   partialText,
		}
		select {
		case m.responses <- partialChunk:
		default:
		}

		// 2. Emit interrupted flag (mimics Gemini's serverContent.interrupted)
		interruptChunk := providers.StreamChunk{
			Interrupted: true,
		}
		select {
		case m.responses <- interruptChunk:
		default:
		}

		// 3. Emit empty turnComplete (mimics Gemini's turnComplete after interruption)
		finishReason := "complete"
		emptyComplete := providers.StreamChunk{
			FinishReason: &finishReason,
		}
		select {
		case m.responses <- emptyComplete:
		default:
		}

		m.interrupted = true
	} else if len(m.responseChunks) > 0 {
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

	m.responseCount++

	// Check if we should close the session unexpectedly after this turn
	// This simulates Gemini closing the connection BEFORE sending a proper response
	shouldClose := m.closeAfterTurns > 0 && m.responseCount >= m.closeAfterTurns

	// Close the response channel if configured - allows duplex tests to complete
	if (m.closeAfterResponse || shouldClose) && !m.closeCalled {
		if shouldClose {
			logger.Info("MockStreamSession: SIMULATING GEMINI CLOSURE - closing session unexpectedly",
				"afterTurns", m.responseCount)
		}
		m.closeCalled = true
		close(m.doneCh)
		close(m.responses)
	}
}

// StreamingProvider extends Provider with StreamInputSupport for duplex testing.
type StreamingProvider struct {
	*Provider
	sessions         []*MockStreamSession // Track all created sessions
	createSessionErr error
	mu               sync.Mutex

	// Auto-respond configuration for duplex testing
	autoRespond  bool   // If true, sessions auto-respond to inputs
	responseText string // Text to respond with

	// Simulation configuration (applied to new sessions)
	interruptOnTurn int  // Turn number to interrupt (1-indexed)
	closeAfterTurns int  // Close session after N turns
	closeNoResponse bool // If true, close without sending final response
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

// WithAutoRespond configures the provider to create sessions that auto-respond to inputs.
func (p *StreamingProvider) WithAutoRespond(responseText string) *StreamingProvider {
	p.autoRespond = true
	p.responseText = responseText
	return p
}

// WithInterruptOnTurn configures the provider to create sessions that simulate
// an interruption on a specific turn. This mimics Gemini detecting user speech
// while the model is responding.
func (p *StreamingProvider) WithInterruptOnTurn(turnNumber int) *StreamingProvider {
	p.interruptOnTurn = turnNumber
	return p
}

// WithCloseAfterTurns configures the provider to create sessions that close
// unexpectedly after N turns. This simulates Gemini dropping the connection.
// If noResponse is true, the session closes WITHOUT sending the final response.
func (p *StreamingProvider) WithCloseAfterTurns(turns int, noResponse ...bool) *StreamingProvider {
	p.closeAfterTurns = turns
	if len(noResponse) > 0 && noResponse[0] {
		p.closeNoResponse = true
	}
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
	// Create a new session and track it
	session := NewMockStreamSession()

	// Configure auto-respond if enabled on the provider
	if p.autoRespond {
		responseText := p.responseText
		if responseText == "" {
			responseText = DefaultMockStreamingResponse
		}
		session.WithAutoRespond(responseText)
	}

	// Apply simulation configurations
	if p.interruptOnTurn > 0 {
		session.WithInterruptOnTurn(p.interruptOnTurn)
	}
	if p.closeAfterTurns > 0 {
		session.WithCloseAfterTurns(p.closeAfterTurns, p.closeNoResponse)
	}

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
