package sdk

import (
	"context"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/session"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
)

// BiDiStream represents a bidirectional streaming session.
// It provides channels for both sending input (text/media) and receiving output.
type BiDiStream struct {
	// Input sends user input to the LLM (use SendText/SendChunk)
	session session.BidirectionalSession

	// Output receives LLM responses
	output <-chan StreamChunk

	// done signals when the stream is complete
	done chan struct{}

	// err stores any error that occurred
	err error

	// conv reference for potential cleanup
	conv *Conversation
}

// SendText sends a text message to the bidirectional stream.
// This is a convenience method that wraps the session's SendText.
func (b *BiDiStream) SendText(ctx context.Context, text string) error {
	return b.session.SendText(ctx, text)
}

// SendChunk sends a raw chunk to the bidirectional stream.
// Use this for sending media or more complex input.
func (b *BiDiStream) SendChunk(ctx context.Context, chunk *providers.StreamChunk) error {
	return b.session.SendChunk(ctx, chunk)
}

// Output returns a receive-only channel of response chunks.
// Iterate over this channel to receive LLM responses in real-time.
func (b *BiDiStream) Output() <-chan StreamChunk {
	return b.output
}

// Done returns a channel that's closed when the stream ends.
func (b *BiDiStream) Done() <-chan struct{} {
	return b.done
}

// Error returns any error that occurred during the stream.
func (b *BiDiStream) Error() error {
	if b.err != nil {
		return b.err
	}
	return b.session.Error()
}

// Close ends the bidirectional streaming session.
func (b *BiDiStream) Close() error {
	return b.session.Close()
}

// StreamBiDi creates a bidirectional streaming session.
//
// This enables real-time bidirectional communication with the LLM,
// useful for voice chat, live transcription, or interactive agents.
//
// Example usage:
//
//	bidi, err := conv.StreamBiDi(ctx)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer bidi.Close()
//
//	// Send input
//	go func() {
//	    bidi.SendText(ctx, "Hello!")
//	    time.Sleep(time.Second)
//	    bidi.SendText(ctx, "How are you?")
//	}()
//
//	// Receive output
//	for chunk := range bidi.Output() {
//	    if chunk.Error != nil {
//	        log.Printf("Error: %v", chunk.Error)
//	        break
//	    }
//	    if chunk.Type == ChunkText {
//	        fmt.Print(chunk.Text)
//	    }
//	}
func (c *Conversation) StreamBiDi(ctx context.Context) (*BiDiStream, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, ErrConversationClosed
	}
	c.mu.RUnlock()

	// Check if provider supports streaming input
	inputProvider, ok := c.provider.(providers.StreamInputSupport)
	if !ok {
		return nil, fmt.Errorf("provider does not support bidirectional streaming (must implement StreamInputSupport)")
	}

	// Build session configuration
	config := c.buildBiDiConfig()

	// Create bidirectional session
	biDiSession, err := session.NewBidirectionalSessionFromProvider(
		ctx,
		config.conversationID,
		config.store,
		inputProvider,
		&providers.StreamInputRequest{
			Temperature: float32(config.temperature),
			MaxTokens:   config.maxTokens,
		},
		config.variables,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create bidirectional session: %w", err)
	}

	// Create output channel and start processing
	outputCh := make(chan StreamChunk, streamChannelBufferSize)
	doneCh := make(chan struct{})

	bidi := &BiDiStream{
		session: biDiSession,
		output:  outputCh,
		done:    doneCh,
		conv:    c,
	}

	// Start goroutine to process responses
	go c.processBiDiOutput(ctx, biDiSession, outputCh, doneCh, bidi)

	return bidi, nil
}

// biDiConfig holds configuration for bidirectional session creation.
type biDiConfig struct {
	temperature    float64
	maxTokens      int
	store          statestore.Store
	conversationID string
	variables      map[string]string
}

// buildBiDiConfig constructs configuration for bidirectional session.
func (c *Conversation) buildBiDiConfig() *biDiConfig {
	config := &biDiConfig{}

	// Get model parameters
	config.temperature = defaultTemperature
	if c.prompt.Parameters != nil && c.prompt.Parameters.Temperature != nil {
		config.temperature = *c.prompt.Parameters.Temperature
	}

	config.maxTokens = defaultMaxTokens
	if c.prompt.Parameters != nil && c.prompt.Parameters.MaxTokens != nil {
		config.maxTokens = *c.prompt.Parameters.MaxTokens
	}

	// Get state store
	if c.config.stateStore != nil {
		config.store = c.config.stateStore
	} else {
		config.store = statestore.NewMemoryStore()
	}

	// Get or generate conversation ID
	config.conversationID = c.config.conversationID
	if config.conversationID == "" && c.textSession != nil {
		config.conversationID = c.textSession.ID()
	}
	if config.conversationID == "" {
		config.conversationID = fmt.Sprintf("bidi-%d", time.Now().UnixNano())
	}

	// Get variables
	config.variables = c.collectBiDiVariables()

	return config
}

// collectBiDiVariables collects variables from config and text session.
func (c *Conversation) collectBiDiVariables() map[string]string {
	vars := make(map[string]string)

	// Add config variables
	if c.config.initialVariables != nil {
		for k, v := range c.config.initialVariables {
			vars[k] = v
		}
	}

	// Merge text session variables
	if c.textSession != nil {
		for k, v := range c.textSession.Variables() {
			vars[k] = v
		}
	}

	return vars
}

// processBiDiOutput processes responses from the bidirectional session.
func (c *Conversation) processBiDiOutput(
	ctx context.Context,
	biDiSession session.BidirectionalSession,
	outputCh chan<- StreamChunk,
	doneCh chan struct{},
	bidi *BiDiStream,
) {
	defer close(outputCh)
	defer close(doneCh)

	startTime := time.Now()
	state := &streamState{}
	responseCh := biDiSession.Response()

	for {
		select {
		case <-ctx.Done():
			c.handleBiDiContextDone(outputCh, bidi, ctx.Err())
			return

		case <-biDiSession.Done():
			c.handleBiDiSessionDone(outputCh, bidi, biDiSession, state, startTime)
			return

		case chunk, ok := <-responseCh:
			if !ok {
				c.handleBiDiChannelClosed(outputCh, state, startTime)
				return
			}

			if chunk.Error != nil {
				c.handleBiDiChunkError(outputCh, bidi, chunk.Error)
				return
			}

			c.emitBiDiStreamChunk(&chunk, outputCh, state)
		}
	}
}

// handleBiDiContextDone handles context cancellation.
func (c *Conversation) handleBiDiContextDone(outputCh chan<- StreamChunk, bidi *BiDiStream, err error) {
	bidi.err = err
	outputCh <- StreamChunk{Error: err}
}

// handleBiDiSessionDone handles session completion.
func (c *Conversation) handleBiDiSessionDone(
	outputCh chan<- StreamChunk,
	bidi *BiDiStream,
	biDiSession session.BidirectionalSession,
	state *streamState,
	startTime time.Time,
) {
	if state.accumulatedContent != "" || len(state.lastToolCalls) > 0 {
		resp := c.buildStreamingResponse(state.finalResult, state.accumulatedContent, state.lastToolCalls, startTime)
		outputCh <- StreamChunk{
			Type:    ChunkDone,
			Message: resp,
		}
	}
	if err := biDiSession.Error(); err != nil {
		bidi.err = err
		outputCh <- StreamChunk{Error: err}
	}
}

// handleBiDiChannelClosed handles response channel closure.
func (c *Conversation) handleBiDiChannelClosed(outputCh chan<- StreamChunk, state *streamState, startTime time.Time) {
	if state.accumulatedContent != "" || len(state.lastToolCalls) > 0 {
		resp := c.buildStreamingResponse(state.finalResult, state.accumulatedContent, state.lastToolCalls, startTime)
		outputCh <- StreamChunk{
			Type:    ChunkDone,
			Message: resp,
		}
	}
}

// handleBiDiChunkError handles chunk errors.
func (c *Conversation) handleBiDiChunkError(outputCh chan<- StreamChunk, bidi *BiDiStream, err error) {
	bidi.err = err
	outputCh <- StreamChunk{Error: err}
}

// emitBiDiStreamChunk converts a provider chunk to SDK chunk(s) and updates state.
func (c *Conversation) emitBiDiStreamChunk(
	chunk *providers.StreamChunk,
	outCh chan<- StreamChunk,
	state *streamState,
) {
	// Emit text delta
	if chunk.Delta != "" {
		state.accumulatedContent += chunk.Delta
		outCh <- StreamChunk{Type: ChunkText, Text: chunk.Delta}
	}

	// Emit content (for full text updates)
	if chunk.Content != "" && chunk.Delta == "" {
		state.accumulatedContent = chunk.Content
		outCh <- StreamChunk{Type: ChunkText, Text: chunk.Content}
	}

	// Emit media delta
	if chunk.MediaDelta != nil {
		outCh <- StreamChunk{Type: ChunkMedia, Media: chunk.MediaDelta}
	}

	// Emit new tool calls
	if len(chunk.ToolCalls) > len(state.lastToolCalls) {
		for i := len(state.lastToolCalls); i < len(chunk.ToolCalls); i++ {
			outCh <- StreamChunk{Type: ChunkToolCall, ToolCall: &chunk.ToolCalls[i]}
		}
		state.lastToolCalls = chunk.ToolCalls
	}

	// Note: We don't emit ChunkDone here because bidirectional streams
	// can have multiple back-and-forth interactions. ChunkDone is emitted
	// when the session ends or explicitly closed.
}
