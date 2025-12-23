// Package replay provides a provider that replays recorded sessions deterministically.
package replay

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/recording"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TimingMode controls how response timing is handled during replay.
type TimingMode int

const (
	// TimingInstant delivers responses immediately without delay.
	TimingInstant TimingMode = iota

	// TimingRealTime delivers responses with original timing preserved.
	TimingRealTime

	// TimingAccelerated delivers responses with accelerated timing.
	TimingAccelerated
)

// tokenEstimateRatio approximates 4 characters per token.
const tokenEstimateRatio = 4

// defaultSpeed is the default playback speed multiplier.
const defaultSpeed = 2.0

// Config configures the replay provider.
type Config struct {
	// Timing controls response delivery timing.
	// Default: TimingInstant
	Timing TimingMode

	// Speed is the multiplier for TimingAccelerated mode.
	// Default: 2.0 (2x speed)
	Speed float64

	// MatchMode controls how requests are matched to recorded responses.
	// Default: MatchByTurn (sequential order)
	MatchMode MatchMode
}

// MatchMode controls how incoming requests are matched to recorded responses.
type MatchMode int

const (
	// MatchByTurn matches responses in sequential order (turn 1, 2, 3, ...).
	MatchByTurn MatchMode = iota

	// MatchByContent matches by comparing the last user message content.
	MatchByContent
)

// DefaultConfig returns sensible defaults for replay.
func DefaultConfig() Config {
	return Config{
		Timing:    TimingInstant,
		Speed:     defaultSpeed,
		MatchMode: MatchByTurn,
	}
}

// recordedTurn holds a recorded assistant response.
type recordedTurn struct {
	event       recording.RecordedEvent
	message     *events.MessageCreatedData
	callInfo    *events.ProviderCallCompletedData // Optional cost/token info
	offset      time.Duration
	userContent string // The user message that triggered this response
}

// Provider replays recorded session responses without making LLM calls.
type Provider struct {
	id        string
	recording *recording.SessionRecording
	config    Config

	mu         sync.Mutex
	turnIndex  int
	turns      []recordedTurn
	contentMap map[string]int // Maps user content to turn index
}

// NewProvider creates a replay provider from a session recording.
func NewProvider(rec *recording.SessionRecording, cfg *Config) (*Provider, error) {
	if rec == nil {
		return nil, fmt.Errorf("recording is required")
	}

	config := DefaultConfig()
	if cfg != nil {
		config = *cfg
	}

	p := &Provider{
		id:         "replay",
		recording:  rec,
		config:     config,
		contentMap: make(map[string]int),
	}

	// Extract assistant responses from recorded events
	if err := p.indexRecording(); err != nil {
		return nil, fmt.Errorf("index recording: %w", err)
	}

	return p, nil
}

// NewProviderFromFile loads a recording file and creates a replay provider.
func NewProviderFromFile(path string, cfg *Config) (*Provider, error) {
	rec, err := recording.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load recording: %w", err)
	}
	return NewProvider(rec, cfg)
}

// indexState holds state during recording indexing.
type indexState struct {
	lastUserContent string
	pendingTurn     *recordedTurn
}

// indexRecording extracts assistant responses and builds lookup indexes.
func (p *Provider) indexRecording() error {
	state := &indexState{}

	for i := range p.recording.Events {
		event := &p.recording.Events[i]
		p.processEvent(event, state)
	}

	if len(p.turns) == 0 {
		return fmt.Errorf("no assistant responses found in recording")
	}

	return nil
}

// processEvent handles a single event during indexing.
func (p *Provider) processEvent(event *recording.RecordedEvent, state *indexState) {
	//nolint:exhaustive // Only message and provider call events are relevant for replay
	switch event.Type {
	case events.EventMessageCreated:
		p.processMessageEvent(event, state)
	case events.EventProviderCallCompleted:
		p.processProviderCallEvent(event, state)
	}
}

// processMessageEvent handles message events during indexing.
func (p *Provider) processMessageEvent(event *recording.RecordedEvent, state *indexState) {
	var msgData events.MessageCreatedData
	if json.Unmarshal(event.Data, &msgData) != nil {
		return
	}

	switch msgData.Role {
	case "user":
		state.lastUserContent = msgData.Content
	case "assistant":
		p.addAssistantTurn(event, &msgData, state)
	}
}

// addAssistantTurn creates a new turn from an assistant message.
func (p *Provider) addAssistantTurn(
	event *recording.RecordedEvent,
	msgData *events.MessageCreatedData,
	state *indexState,
) {
	turn := recordedTurn{
		event:       *event,
		message:     msgData,
		offset:      event.Offset,
		userContent: state.lastUserContent,
	}
	turnIdx := len(p.turns)
	p.turns = append(p.turns, turn)
	state.pendingTurn = &p.turns[turnIdx]

	if state.lastUserContent != "" {
		p.contentMap[state.lastUserContent] = turnIdx
	}
}

// processProviderCallEvent attaches cost info to pending turn.
func (p *Provider) processProviderCallEvent(event *recording.RecordedEvent, state *indexState) {
	if state.pendingTurn == nil {
		return
	}

	var callData events.ProviderCallCompletedData
	if json.Unmarshal(event.Data, &callData) != nil {
		return
	}

	state.pendingTurn.callInfo = &callData
	state.pendingTurn = nil
}

// ID returns the provider identifier.
func (p *Provider) ID() string {
	return p.id
}

// Predict returns the next recorded response.
func (p *Provider) Predict(
	ctx context.Context,
	req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	turn, err := p.getNextTurn(req)
	if err != nil {
		return providers.PredictionResponse{}, err
	}

	// Apply timing delay if configured
	if err := p.applyTiming(ctx, turn); err != nil {
		return providers.PredictionResponse{}, err
	}

	return p.buildResponse(turn), nil
}

// PredictStream returns the recorded response as a single stream chunk.
func (p *Provider) PredictStream(
	ctx context.Context,
	req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	turn, err := p.getNextTurn(req)
	if err != nil {
		return nil, err
	}

	ch := make(chan providers.StreamChunk, 1)

	go func() {
		defer close(ch)

		// Apply timing delay if configured
		if p.applyTiming(ctx, turn) != nil {
			return
		}

		resp := p.buildResponse(turn)
		finishReason := "stop"
		ch <- providers.StreamChunk{
			Content:      resp.Content,
			Delta:        resp.Content,
			TokenCount:   resp.CostInfo.OutputTokens,
			DeltaTokens:  resp.CostInfo.OutputTokens,
			FinishReason: &finishReason,
			CostInfo:     resp.CostInfo,
			FinalResult:  &resp,
		}
	}()

	return ch, nil
}

// getNextTurn returns the next turn to replay based on match mode.
func (p *Provider) getNextTurn(req providers.PredictionRequest) (*recordedTurn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var turnIdx int

	switch p.config.MatchMode {
	case MatchByContent:
		// Find turn by last user message
		if len(req.Messages) > 0 {
			lastMsg := req.Messages[len(req.Messages)-1]
			if idx, ok := p.contentMap[lastMsg.Content]; ok {
				turnIdx = idx
			} else {
				// Fall back to sequential if not found
				turnIdx = p.turnIndex
				p.turnIndex++
			}
		} else {
			turnIdx = p.turnIndex
			p.turnIndex++
		}

	case MatchByTurn:
		// Sequential order
		turnIdx = p.turnIndex
		p.turnIndex++
	}

	if turnIdx >= len(p.turns) {
		return nil, fmt.Errorf("replay exhausted: no more recorded responses (turn %d)", turnIdx+1)
	}

	return &p.turns[turnIdx], nil
}

// applyTiming applies the configured timing delay.
func (p *Provider) applyTiming(ctx context.Context, turn *recordedTurn) error {
	delay := p.calculateDelay(turn)
	if delay <= 0 {
		return nil
	}
	return p.waitWithContext(ctx, delay)
}

// calculateDelay computes the delay for the current turn based on timing mode.
func (p *Provider) calculateDelay(turn *recordedTurn) time.Duration {
	switch p.config.Timing {
	case TimingInstant:
		return 0
	case TimingRealTime:
		return p.calculateTimingOffset(turn)
	case TimingAccelerated:
		return p.calculateAcceleratedOffset(turn)
	default:
		return 0
	}
}

// calculateTimingOffset returns the original timing offset between turns.
func (p *Provider) calculateTimingOffset(turn *recordedTurn) time.Duration {
	if p.turnIndex <= 0 || p.turnIndex > len(p.turns) {
		return 0
	}
	prevOffset := p.getPreviousTurnOffset()
	return turn.offset - prevOffset
}

// calculateAcceleratedOffset returns the accelerated timing offset.
func (p *Provider) calculateAcceleratedOffset(turn *recordedTurn) time.Duration {
	originalDelay := p.calculateTimingOffset(turn)
	if p.config.Speed <= 0 {
		return originalDelay
	}
	return time.Duration(float64(originalDelay) / p.config.Speed)
}

// getPreviousTurnOffset returns the offset of the previous turn.
func (p *Provider) getPreviousTurnOffset() time.Duration {
	if p.turnIndex <= 1 {
		return 0
	}
	return p.turns[p.turnIndex-2].offset
}

// waitWithContext waits for the specified duration or context cancellation.
func (p *Provider) waitWithContext(ctx context.Context, delay time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

// buildResponse creates a PredictionResponse from a recorded turn.
func (p *Provider) buildResponse(turn *recordedTurn) providers.PredictionResponse {
	resp := providers.PredictionResponse{}

	// Extract content from the assistant message
	if turn.message != nil {
		resp.Content = turn.message.Content
		// Convert tool calls if present
		if len(turn.message.ToolCalls) > 0 {
			resp.ToolCalls = make([]types.MessageToolCall, len(turn.message.ToolCalls))
			for i, tc := range turn.message.ToolCalls {
				resp.ToolCalls[i] = types.MessageToolCall{
					ID:   tc.ID,
					Name: tc.Name,
					Args: json.RawMessage(tc.Args),
				}
			}
		}
	}

	// Use recorded cost info or estimate
	if turn.callInfo != nil {
		resp.Latency = turn.callInfo.Duration
		resp.CostInfo = &types.CostInfo{
			InputTokens:  turn.callInfo.InputTokens,
			OutputTokens: turn.callInfo.OutputTokens,
			CachedTokens: turn.callInfo.CachedTokens,
			TotalCost:    turn.callInfo.Cost,
		}
	} else {
		resp.CostInfo = p.estimateCost(resp.Content)
	}

	return resp
}

// estimateCost creates a rough cost estimate based on content length.
func (p *Provider) estimateCost(content string) *types.CostInfo {
	outputTokens := len(content) / tokenEstimateRatio
	if outputTokens == 0 {
		outputTokens = 1
	}

	return &types.CostInfo{
		InputTokens:  0, // Replays don't have real input costs
		OutputTokens: outputTokens,
		TotalCost:    0, // No real cost for replays
	}
}

// SupportsStreaming returns true as replay supports streaming.
func (p *Provider) SupportsStreaming() bool {
	return true
}

// ShouldIncludeRawOutput returns false as replays don't have raw output.
func (p *Provider) ShouldIncludeRawOutput() bool {
	return false
}

// Close is a no-op for replay provider.
func (p *Provider) Close() error {
	return nil
}

// CalculateCost returns zero cost as replays don't incur real costs.
func (p *Provider) CalculateCost(inputTokens, outputTokens, cachedTokens int) types.CostInfo {
	return types.CostInfo{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		CachedTokens: cachedTokens,
		TotalCost:    0, // No real cost for replays
	}
}

// Reset resets the provider to replay from the beginning.
func (p *Provider) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.turnIndex = 0
}

// TurnCount returns the number of recorded turns available.
func (p *Provider) TurnCount() int {
	return len(p.turns)
}

// CurrentTurn returns the current turn index (0-based).
func (p *Provider) CurrentTurn() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.turnIndex
}

// Ensure Provider implements providers.Provider.
var _ providers.Provider = (*Provider)(nil)
