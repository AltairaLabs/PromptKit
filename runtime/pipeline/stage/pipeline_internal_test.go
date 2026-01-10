package stage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
)

// TestMonitorExecutionTimeout tests the monitorExecutionTimeout helper function.
func TestMonitorExecutionTimeout(t *testing.T) {
	t.Run("no timeout configured", func(t *testing.T) {
		config := DefaultPipelineConfig()
		config.ExecutionTimeout = 0

		pipeline := &StreamPipeline{
			config: config,
			stages: []Stage{},
		}

		// Should return immediately without starting goroutine
		pipeline.monitorExecutionTimeout(context.Background(), time.Now())
		// No assertions needed - just verify no panic
	})

	t.Run("timeout configured with deadline exceeded", func(t *testing.T) {
		config := DefaultPipelineConfig()
		config.ExecutionTimeout = 100 * time.Millisecond

		pipeline := &StreamPipeline{
			config: config,
			stages: []Stage{},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		pipeline.monitorExecutionTimeout(ctx, time.Now())

		// Wait for context to timeout
		<-ctx.Done()

		// Give goroutine time to log
		time.Sleep(10 * time.Millisecond)
		// Logging is verified by code coverage - function should run through timeout branch
	})

	t.Run("timeout configured with context cancelled", func(t *testing.T) {
		config := DefaultPipelineConfig()
		config.ExecutionTimeout = 100 * time.Millisecond

		pipeline := &StreamPipeline{
			config: config,
			stages: []Stage{},
		}

		ctx, cancel := context.WithCancel(context.Background())

		pipeline.monitorExecutionTimeout(ctx, time.Now())

		// Cancel (not timeout)
		cancel()

		// Give goroutine time to process
		time.Sleep(10 * time.Millisecond)
		// Should not log timeout message since it's cancelled, not deadline exceeded
	})
}

// TestWaitForStageErrors tests the waitForStageErrors helper function.
func TestWaitForStageErrors(t *testing.T) {
	pipeline := &StreamPipeline{}

	t.Run("empty error channel", func(t *testing.T) {
		errChan := make(chan error)
		close(errChan)

		result := pipeline.waitForStageErrors(errChan)
		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}
	})

	t.Run("single error", func(t *testing.T) {
		errChan := make(chan error, 1)
		testErr := errors.New("test error")
		errChan <- testErr
		close(errChan)

		result := pipeline.waitForStageErrors(errChan)
		if result != testErr {
			t.Errorf("expected %v, got %v", testErr, result)
		}
	})

	t.Run("multiple errors returns first", func(t *testing.T) {
		errChan := make(chan error, 3)
		firstErr := errors.New("first error")
		secondErr := errors.New("second error")
		errChan <- firstErr
		errChan <- secondErr
		errChan <- nil
		close(errChan)

		result := pipeline.waitForStageErrors(errChan)
		if result != firstErr {
			t.Errorf("expected %v, got %v", firstErr, result)
		}
	})

	t.Run("nil errors followed by real error", func(t *testing.T) {
		errChan := make(chan error, 3)
		testErr := errors.New("test error")
		errChan <- nil
		errChan <- nil
		errChan <- testErr
		close(errChan)

		result := pipeline.waitForStageErrors(errChan)
		if result != testErr {
			t.Errorf("expected %v, got %v", testErr, result)
		}
	})
}

// TestEmitCompletionEvent tests the emitCompletionEvent helper function.
func TestEmitCompletionEvent(t *testing.T) {
	t.Run("nil event emitter", func(t *testing.T) {
		pipeline := &StreamPipeline{
			eventEmitter: nil,
		}

		// Should not panic
		pipeline.emitCompletionEvent(nil, time.Second)
		pipeline.emitCompletionEvent(errors.New("error"), time.Second)
	})

	t.Run("success event with emitter", func(t *testing.T) {
		// Create a real emitter with nil bus (will no-op on emit)
		emitter := events.NewEmitter(nil, "test-run", "test-session", "test-conv")
		pipeline := &StreamPipeline{
			eventEmitter: emitter,
		}

		// Should not panic - emitter handles nil bus gracefully
		pipeline.emitCompletionEvent(nil, time.Second)
	})

	t.Run("failure event with emitter", func(t *testing.T) {
		// Create a real emitter with nil bus (will no-op on emit)
		emitter := events.NewEmitter(nil, "test-run", "test-session", "test-conv")
		pipeline := &StreamPipeline{
			eventEmitter: emitter,
		}

		testErr := errors.New("test error")
		// Should not panic - emitter handles nil bus gracefully
		pipeline.emitCompletionEvent(testErr, time.Second)
	})
}
