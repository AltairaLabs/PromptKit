package mock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// isVideoOrImage returns true when the chunk metadata's mime_type field
// indicates a non-audio media type, in which case PCM16 alignment doesn't
// apply.
func isVideoOrImage(meta map[string]string) bool {
	if meta == nil {
		return false
	}
	mt := meta["mime_type"]
	return strings.HasPrefix(mt, "video/") || strings.HasPrefix(mt, "image/")
}

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

	// defaultAudioFixtureSampleRate is used when an audio_file does not declare a sample rate.
	defaultAudioFixtureSampleRate = 24000
	// defaultAudioFixtureMIMEType is used when an audio_file does not declare a mime type.
	defaultAudioFixtureMIMEType = "audio/pcm"
	// audioFrameMillis is the duration of a single emitted audio chunk in milliseconds (~20ms).
	audioFrameMillis = 20
	// bytesPerPCMSample is the byte width of one s16le sample (signed 16-bit little-endian).
	bytesPerPCMSample = 2
	// millisPerSecond converts millisecond durations to/from seconds when sizing PCM frames.
	millisPerSecond = 1000
	// fallbackFramesPerSecond is the safety divisor used to recover ~20ms framing
	// when the configured sample rate is too low for the primary calculation to
	// produce a positive sample count (1 second / 20ms = 50 frames).
	fallbackFramesPerSecond = 50
)

// mockAudioFixture holds the in-memory bytes of a PCM audio fixture plus its
// declared sample rate and MIME type. Fixtures are cached per session so a
// scenario referencing the same file across multiple turns reads from disk
// only once.
type mockAudioFixture struct {
	Bytes      []byte
	SampleRate int
	MIMEType   string
}

// MockStreamSession implements providers.StreamInputSession for testing duplex scenarios.
//
//nolint:revive // MockStreamSession naming is intentional for clarity in test usage
type MockStreamSession struct {
	// BargeInSignal satisfies StreamInputSession.BargeIn(); tests can drive it
	// via the promoted SignalBargeIn() to simulate server-side barge-in.
	providers.BargeInSignal

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

	// Repository-driven auto-respond. When repo + scenarioID are set, auto-respond
	// looks up the configured Turn for the current turn number from the repository
	// and emits its text content (and audio fixture, if present) instead of the
	// static responseText. fixtureBaseDir resolves relative audio_file paths.
	repo            ResponseRepository
	scenarioID      string
	fixtureBaseDir  string
	audioCache      map[string]*mockAudioFixture // file path -> loaded fixture
	audioCacheGuard sync.Mutex

	// Simulation: Interruption behavior (mimics Gemini detecting user speech during response)
	interruptOnTurn int  // If > 0, simulate interruption on this turn number (1-indexed)
	interrupted     bool // Track if current turn was interrupted

	// Simulation: Session closure (mimics Gemini dropping connection unexpectedly)
	closeAfterTurns int  // If > 0, close session after this many turns
	closeNoResponse bool // If true with closeAfterTurns, close WITHOUT sending final response

	// toolResponses collects every tool execution result the runtime has
	// fed back into this session via SendToolResponse(s). Lets tests
	// verify the runtime delivered the expected payload for a scripted
	// tool_call. Implementation lives in mock_streaming_tools_integration.go
	// so the file scoping makes the dependency on ToolResponseSupport
	// obvious from the directory listing.
	toolResponses []providers.ToolResponse
}

// NewMockStreamSession creates a new mock stream session.
func NewMockStreamSession() *MockStreamSession {
	return &MockStreamSession{
		BargeInSignal: providers.NewBargeInSignal(),
		chunks:        make([]*types.MediaChunk, 0),
		texts:         make([]string, 0),
		responses:     make(chan providers.StreamChunk, defaultResponseBufferSize),
		doneCh:        make(chan struct{}),
		autoRespond:   false,
		responseText:  "Mock response",
		audioCache:    make(map[string]*mockAudioFixture),
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

// WithRepository wires a ResponseRepository into the auto-respond path so that
// per-turn responses (including optional audio fixtures) come from the same
// scenario configuration used by the non-streaming mock provider. baseDir is
// the directory used to resolve relative audio_file paths; pass an empty
// string to use absolute paths only.
func (m *MockStreamSession) WithRepository(repo ResponseRepository, baseDir string) *MockStreamSession {
	m.repo = repo
	m.fixtureBaseDir = baseDir
	return m
}

// WithScenarioID selects which scenario the auto-respond path should consult
// when looking up turns from the repository. Without a scenario ID the session
// falls back to the static responseText.
func (m *MockStreamSession) WithScenarioID(scenarioID string) *MockStreamSession {
	m.scenarioID = scenarioID
	return m
}

// SendChunk implements StreamInputSession.SendChunk.
//
// Validates PCM16 alignment (even byte count) so tests catch upstream
// chunking bugs before they reach a real provider. We learned this the
// hard way — OpenAI Realtime rejected partial chunks from a buggy
// pumpTTSChunks with a cryptic "Invalid 'audio'... got an invalid value"
// that took a full bisection against the live API to track down. The
// mock should fail loudly on the same input shape so a unit test would
// have caught it.
func (m *MockStreamSession) SendChunk(ctx context.Context, chunk *types.MediaChunk) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closeCalled {
		return errors.New(errSessionClosed)
	}
	if m.sendChunkErr != nil {
		return m.sendChunkErr
	}

	if chunk == nil || len(chunk.Data) == 0 {
		// Match real providers (OpenAI SendChunk silently no-ops on empty).
		return nil
	}
	// PCM16 alignment check — only applies to audio chunks. Video and image
	// chunks have their own framing (annexed in metadata.mime_type).
	if !isVideoOrImage(chunk.Metadata) && len(chunk.Data)%2 != 0 {
		return fmt.Errorf("MockStreamSession.SendChunk: PCM16 alignment violation, "+
			"chunk has %d bytes (odd). Producer is emitting partial samples — "+
			"likely a reader.Read() that should be io.ReadFull()", len(chunk.Data))
	}

	m.chunks = append(m.chunks, chunk)

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

	if (m.autoRespond || len(m.responseChunks) > 0) && !m.closeCalled {
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
		logger.Info("MockStreamSession: simulating provider closure without response",
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
		m.emitTurnResponse(currentTurn)
	}

	m.responseCount++

	// Check if we should close the session unexpectedly after this turn
	// This simulates Gemini closing the connection BEFORE sending a proper response
	shouldClose := m.closeAfterTurns > 0 && m.responseCount >= m.closeAfterTurns

	// Close the response channel if configured - allows duplex tests to complete
	if (m.closeAfterResponse || shouldClose) && !m.closeCalled {
		if shouldClose {
			logger.Info("MockStreamSession: simulating provider closure after turns",
				"after_turns", m.responseCount)
		}
		m.closeCalled = true
		close(m.doneCh)
		close(m.responses)
	}
}

// emitTurnResponse emits the response for the given turn. When a repository
// and scenarioID are configured, the per-turn Turn record is consulted: any
// configured audio fixture is emitted as MediaData chunks first, followed by
// the text Content + ToolCalls + FinishReason chunk. Without a repository it
// falls back to the static responseText behavior.
//
// Must be called with m.mu held.
func (m *MockStreamSession) emitTurnResponse(turnNumber int) {
	text, audioFixture, toolCalls := m.resolveTurn(turnNumber)

	// Emit audio chunks first (if any) so consumers see them as part of the
	// response stream — providers like Gemini interleave audio + transcript,
	// but emitting audio before the final FinishReason chunk is the contract
	// the duplex pipeline relies on.
	if audioFixture != nil {
		m.emitAudioChunks(audioFixture)
	}

	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = turnTypeToolCalls
	}
	chunk := providers.StreamChunk{
		Content:      text,
		Delta:        text,
		ToolCalls:    toolCalls,
		FinishReason: &finishReason,
	}
	// Block on send (matching emitAudioChunks): the FinishReason chunk is
	// the only signal the consumer has that the turn ended. Dropping it on
	// a full buffer races into a deadlock — DuplexProviderStage parks on
	// the response channel forever after draining the queued audio chunks.
	select {
	case m.responses <- chunk:
	case <-m.doneCh:
	}
}

// convertMockToolCalls maps the YAML Turn.ToolCalls list onto runtime
// MessageToolCall values, marshaling each argument map into a JSON
// RawMessage. Mirrors the conversion in mock_tool_provider_interactive.go's
// PredictWithTools so duplex and non-duplex mock paths produce structurally
// identical tool-call shapes.
func convertMockToolCalls(turnToolCalls []ToolCall) []types.MessageToolCall {
	if len(turnToolCalls) == 0 {
		return nil
	}
	out := make([]types.MessageToolCall, 0, len(turnToolCalls))
	for i, tc := range turnToolCalls {
		argsBytes, err := json.Marshal(tc.Arguments)
		if err != nil {
			logger.Warn("MockStreamSession: failed to marshal tool call args; dropping",
				"tool", tc.Name, "error", err)
			continue
		}
		out = append(out, types.MessageToolCall{
			ID:   fmt.Sprintf("call_%d_%s", i, tc.Name),
			Name: tc.Name,
			Args: json.RawMessage(argsBytes),
		})
	}
	return out
}

// resolveTurn looks up the text + audio fixture + tool calls for the given
// turn. If a repository is configured it queries the repo for the turn;
// otherwise it returns the static responseText with no audio and no tool
// calls.
func (m *MockStreamSession) resolveTurn(turnNumber int) (string, *mockAudioFixture, []types.MessageToolCall) {
	if m.repo == nil || m.scenarioID == "" {
		return m.responseText, nil, nil
	}

	turn, err := m.repo.GetTurn(context.Background(), ResponseParams{
		ScenarioID: m.scenarioID,
		TurnNumber: turnNumber,
	})
	if err != nil || turn == nil {
		if err != nil {
			logger.Debug("MockStreamSession: repository GetTurn failed; falling back to responseText",
				"scenario_id", m.scenarioID,
				"turn", turnNumber,
				"error", err)
		}
		return m.responseText, nil, nil
	}

	text := turn.Content
	if text == "" {
		text = m.responseText
	}

	toolCalls := convertMockToolCalls(turn.ToolCalls)

	if turn.AudioFile == "" {
		return text, nil, toolCalls
	}

	fixture, loadErr := m.loadAudioFixture(turn.AudioFile, turn.AudioSampleRate, turn.AudioMIMEType)
	if loadErr != nil {
		logger.Warn("MockStreamSession: failed to load audio fixture; emitting text-only response",
			"file", turn.AudioFile,
			"error", loadErr)
		return text, nil, toolCalls
	}
	return text, fixture, toolCalls
}

// loadAudioFixture reads a PCM fixture from disk and caches it by resolved
// path so multi-turn scenarios re-use a single read. Relative paths resolve
// against fixtureBaseDir when set.
func (m *MockStreamSession) loadAudioFixture(
	filePath string, sampleRate int, mimeType string,
) (*mockAudioFixture, error) {
	resolved := filePath
	if !filepath.IsAbs(resolved) && m.fixtureBaseDir != "" {
		resolved = filepath.Join(m.fixtureBaseDir, resolved)
	}

	m.audioCacheGuard.Lock()
	defer m.audioCacheGuard.Unlock()

	if m.audioCache == nil {
		m.audioCache = make(map[string]*mockAudioFixture)
	}
	if cached, ok := m.audioCache[resolved]; ok {
		return cached, nil
	}

	data, err := os.ReadFile(resolved) //nolint:gosec // fixture path comes from a trusted local config file
	if err != nil {
		return nil, fmt.Errorf("read audio fixture %q: %w", resolved, err)
	}

	fixture := &mockAudioFixture{
		Bytes:      data,
		SampleRate: sampleRate,
		MIMEType:   mimeType,
	}
	if fixture.SampleRate <= 0 {
		fixture.SampleRate = defaultAudioFixtureSampleRate
	}
	if fixture.MIMEType == "" {
		fixture.MIMEType = defaultAudioFixtureMIMEType
	}

	m.audioCache[resolved] = fixture
	return fixture, nil
}

// emitAudioChunks pushes the fixture bytes onto the response channel as
// MediaData chunks, sliced into ~20ms frames at the fixture's sample rate
// (s16le mono). Emitting them as discrete chunks mirrors how real
// duplex providers stream audio — small frames the consumer can pace.
//
// Must be called with m.mu held (writes to m.responses).
func (m *MockStreamSession) emitAudioChunks(fixture *mockAudioFixture) {
	if fixture == nil || len(fixture.Bytes) == 0 {
		return
	}

	samplesPer20ms := fixture.SampleRate * audioFrameMillis / millisPerSecond
	if samplesPer20ms <= 0 {
		samplesPer20ms = fixture.SampleRate / fallbackFramesPerSecond
	}
	bytesPerChunk := samplesPer20ms * bytesPerPCMSample
	if bytesPerChunk <= 0 {
		bytesPerChunk = len(fixture.Bytes)
	}

	frameNum := int64(0)
	for offset := 0; offset < len(fixture.Bytes); offset += bytesPerChunk {
		end := offset + bytesPerChunk
		if end > len(fixture.Bytes) {
			end = len(fixture.Bytes)
		}
		chunk := providers.StreamChunk{
			MediaData: &providers.StreamMediaData{
				Data:       fixture.Bytes[offset:end],
				MIMEType:   fixture.MIMEType,
				SampleRate: fixture.SampleRate,
				Channels:   1,
				FrameNum:   frameNum,
			},
		}
		// Block on send — the mock provider is the producer, dropping its own
		// emitted frames would silently lose response audio. Real providers
		// pace their emission via the network frame rate; the mock relies on
		// the consumer (DuplexProviderStage) to drain the channel.
		select {
		case m.responses <- chunk:
		case <-m.doneCh:
			return
		}
		frameNum++
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

	// Repository-driven auto-respond plumbing (optional). When set, new sessions
	// inherit these so they look up scripted Turn responses (including audio
	// fixtures) instead of just emitting responseText each turn.
	repo              ResponseRepository
	defaultScenarioID string
	fixtureBaseDir    string

	// Simulation configuration (applied to new sessions)
	interruptOnTurn int  // Turn number to interrupt (1-indexed)
	closeAfterTurns int  // Close session after N turns
	closeNoResponse bool // If true, close without sending final response

	// Custom response chunks emitted by each session on input (applied to new
	// sessions). Lets tests drive the duplex stage with specific StreamChunks —
	// e.g. reasoning chunks — instead of plain auto-respond text.
	responseChunks []providers.StreamChunk
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

// WithMockResponses wires a ResponseRepository, default scenario ID and
// fixture base directory into every session created by this provider. When
// auto-respond is enabled, sessions will look up per-turn Turn responses
// (including audio_file fixtures) from the repository instead of emitting
// the static responseText.
//
// scenarioID may be overridden per session via the StreamingInputConfig
// metadata key "mock_scenario_id".
func (p *StreamingProvider) WithMockResponses(
	repo ResponseRepository,
	scenarioID, fixtureBaseDir string,
) *StreamingProvider {
	p.repo = repo
	p.defaultScenarioID = scenarioID
	p.fixtureBaseDir = fixtureBaseDir
	return p
}

// WithResponseChunks configures the provider so each created session emits the
// given StreamChunks on input (instead of auto-respond text). Useful for driving
// the duplex stage with specific chunks such as reasoning deltas.
func (p *StreamingProvider) WithResponseChunks(chunks ...providers.StreamChunk) *StreamingProvider {
	p.responseChunks = chunks
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

	// Wire repository-driven turn lookup when configured. The scenario ID
	// can be overridden per-session via StreamingInputConfig.Metadata.
	if p.repo != nil {
		session.WithRepository(p.repo, p.fixtureBaseDir)
		scenarioID := p.defaultScenarioID
		if req != nil && req.Metadata != nil {
			if override, ok := req.Metadata["mock_scenario_id"].(string); ok && override != "" {
				scenarioID = override
			}
		}
		if scenarioID != "" {
			session.WithScenarioID(scenarioID)
		}
	}

	// Apply simulation configurations
	if p.interruptOnTurn > 0 {
		session.WithInterruptOnTurn(p.interruptOnTurn)
	}
	if p.closeAfterTurns > 0 {
		session.WithCloseAfterTurns(p.closeAfterTurns, p.closeNoResponse)
	}
	if len(p.responseChunks) > 0 {
		session.WithResponseChunks(p.responseChunks)
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
