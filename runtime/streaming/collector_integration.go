package streaming

import (
	"context"
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// ErrSessionEnded is returned when the streaming session has ended.
// This is not necessarily an error, just indicates the session is complete.
var ErrSessionEnded = errors.New("session ended")

// ResponseCollectorConfig configures response collection behavior.
type ResponseCollectorConfig struct {
	// ToolExecutor is called when tool calls are received.
	// If nil, tool calls will result in an error.
	ToolExecutor ToolExecutor

	// LogPrefix is prepended to log messages for identification.
	LogPrefix string
}

// ResponseCollector manages response collection from a streaming session.
// It processes streaming elements, handles tool calls, and signals completion.
type ResponseCollector struct {
	config ResponseCollectorConfig
}

// NewResponseCollector creates a new response collector with the given configuration.
func NewResponseCollector(config ResponseCollectorConfig) *ResponseCollector {
	return &ResponseCollector{
		config: config,
	}
}

// Start begins collecting responses in a goroutine.
// Returns a channel that receives nil on success or an error on failure.
//
// The collector will:
// 1. Process incoming stream elements
// 2. Execute tool calls via the ToolExecutor (if configured)
// 3. Send tool results back through inputChan
// 4. Signal completion or error through the returned channel
func (c *ResponseCollector) Start(
	ctx context.Context,
	outputChan <-chan stage.StreamElement,
	inputChan chan<- stage.StreamElement,
) <-chan error {
	responseDone := make(chan error, 1)
	go c.collect(ctx, outputChan, inputChan, responseDone)
	return responseDone
}

// collect processes elements from the output channel until complete.
func (c *ResponseCollector) collect(
	ctx context.Context,
	outputChan <-chan stage.StreamElement,
	inputChan chan<- stage.StreamElement,
	responseDone chan<- error,
) {
	logPrefix := c.config.LogPrefix
	if logPrefix == "" {
		logPrefix = "ResponseCollector"
	}

	for {
		select {
		case <-ctx.Done():
			responseDone <- ctx.Err()
			return

		case elem, ok := <-outputChan:
			if !ok {
				logger.Debug("response channel closed before receiving complete response", "component", logPrefix)
				responseDone <- fmt.Errorf("session ended before receiving response: %w", ErrSessionEnded)
				return
			}

			action, err := ProcessResponseElement(&elem, logPrefix)
			done := c.handleAction(ctx, action, err, &elem, inputChan, responseDone, logPrefix)
			if done {
				return
			}
		}
	}
}

// handleAction processes the response action and returns true if collection is done.
func (c *ResponseCollector) handleAction(
	ctx context.Context,
	action ResponseAction,
	err error,
	elem *stage.StreamElement,
	inputChan chan<- stage.StreamElement,
	responseDone chan<- error,
	logPrefix string,
) bool {
	switch action {
	case ResponseActionContinue:
		return false

	case ResponseActionComplete:
		responseDone <- nil
		return true

	case ResponseActionError:
		responseDone <- err
		return true

	case ResponseActionToolCalls:
		if elem.Message != nil && len(elem.Message.ToolCalls) > 0 {
			if err := c.executeAndSendToolResults(ctx, elem, inputChan, logPrefix); err != nil {
				responseDone <- err
				return true
			}
		}
		return false
	}
	return false
}

// executeAndSendToolResults executes tool calls and sends results back.
func (c *ResponseCollector) executeAndSendToolResults(
	ctx context.Context,
	elem *stage.StreamElement,
	inputChan chan<- stage.StreamElement,
	logPrefix string,
) error {
	if c.config.ToolExecutor == nil {
		logger.Warn("received tool calls but no ToolExecutor configured", "component", logPrefix)
		return errors.New("received tool calls but no ToolExecutor configured")
	}

	result, err := c.config.ToolExecutor.Execute(ctx, elem.Message.ToolCalls)
	if err != nil {
		logger.Error("tool execution failed", "component", logPrefix, "error", err)
		return err
	}

	if result != nil && len(result.ProviderResponses) > 0 {
		if err := SendToolResults(ctx, result, inputChan); err != nil {
			logger.Error("failed to send tool results", "component", logPrefix, "error", err)
			return err
		}
	}

	return nil
}

// DrainStaleMessages removes any buffered messages from the output channel.
// This is useful for clearing state between turns.
//
// Returns the number of messages drained, or an error if the session ended.
func DrainStaleMessages(outputChan <-chan stage.StreamElement) (int, error) {
	drainCount := 0
	for {
		select {
		case elem, ok := <-outputChan:
			if !ok {
				logger.Debug("DrainStaleMessages: session ended during drain (channel closed)")
				return drainCount, ErrSessionEnded
			}
			drainCount++
			logger.Debug("DrainStaleMessages: drained stale element",
				"hasText", elem.Text != nil, "endOfStream", elem.EndOfStream)
		default:
			if drainCount > 0 {
				logger.Debug("DrainStaleMessages: drained stale messages", "count", drainCount)
			}
			return drainCount, nil
		}
	}
}

// WaitForResponse waits for the response collection to complete.
// This is a convenience function for blocking until a response is received.
func WaitForResponse(ctx context.Context, responseDone <-chan error) error {
	select {
	case err := <-responseDone:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
