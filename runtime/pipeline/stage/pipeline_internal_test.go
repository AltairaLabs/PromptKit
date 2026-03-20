package stage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
)

// TestIsRootStage tests the precomputed root stage O(1) lookup.
func TestIsRootStage(t *testing.T) {
	// Build a pipeline: A -> B -> C (A is root, B and C are not)
	stageA := &testPassthroughStage{name: "stageA"}
	stageB := &testPassthroughStage{name: "stageB"}
	stageC := &testPassthroughStage{name: "stageC"}

	pipeline, err := NewPipelineBuilder().
		Chain(stageA, stageB, stageC).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if !pipeline.isRootStage("stageA") {
		t.Error("stageA should be a root stage")
	}
	if pipeline.isRootStage("stageB") {
		t.Error("stageB should not be a root stage")
	}
	if pipeline.isRootStage("stageC") {
		t.Error("stageC should not be a root stage")
	}
	if pipeline.isRootStage("nonexistent") {
		t.Error("nonexistent stage should not be a root stage")
	}
}

// testPassthroughStage is a minimal stage that passes elements through.
type testPassthroughStage struct {
	name string
}

func (s *testPassthroughStage) Name() string    { return s.name }
func (s *testPassthroughStage) Type() StageType { return StageTypeTransform }
func (s *testPassthroughStage) Process(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
	defer close(out)
	for elem := range in {
		out <- elem
	}
	return nil
}

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

// TestFindUpstreamStage_ReverseEdges verifies O(1) lookup via precomputed reverseEdges.
func TestFindUpstreamStage_ReverseEdges(t *testing.T) {
	stageA := &testPassthroughStage{name: "stageA"}
	stageB := &testPassthroughStage{name: "stageB"}
	stageC := &testPassthroughStage{name: "stageC"}

	pipeline, err := NewPipelineBuilder().
		Chain(stageA, stageB, stageC).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// reverseEdges should be populated by Build.
	if pipeline.reverseEdges == nil {
		t.Fatal("reverseEdges should not be nil")
	}

	if upstream := pipeline.findUpstreamStage("stageB"); upstream != "stageA" {
		t.Errorf("expected stageA upstream of stageB, got %q", upstream)
	}
	if upstream := pipeline.findUpstreamStage("stageC"); upstream != "stageB" {
		t.Errorf("expected stageB upstream of stageC, got %q", upstream)
	}
	if upstream := pipeline.findUpstreamStage("stageA"); upstream != "" {
		t.Errorf("expected empty upstream for root stageA, got %q", upstream)
	}
	if upstream := pipeline.findUpstreamStage("nonexistent"); upstream != "" {
		t.Errorf("expected empty upstream for nonexistent, got %q", upstream)
	}
}

// TestFindUpstreamStage_FallbackNilReverseEdges tests the fallback linear scan
// when reverseEdges is nil (should not happen in practice).
func TestFindUpstreamStage_FallbackNilReverseEdges(t *testing.T) {
	pipeline := &StreamPipeline{
		edges: map[string][]string{
			"A": {"B"},
			"B": {"C"},
		},
		reverseEdges: nil, // force fallback
	}

	if upstream := pipeline.findUpstreamStage("B"); upstream != "A" {
		t.Errorf("fallback: expected A upstream of B, got %q", upstream)
	}
	if upstream := pipeline.findUpstreamStage("X"); upstream != "" {
		t.Errorf("fallback: expected empty for X, got %q", upstream)
	}
}

// TestCollectOutput_ConcurrentLeafStages verifies that collectOutput fans
// in from multiple leaf stages concurrently.
func TestCollectOutput_ConcurrentLeafStages(t *testing.T) {
	// Build a fan-out pipeline: A -> B, A -> C (B and C are leaves)
	stageA := &testPassthroughStage{name: "stageA"}
	stageB := &testPassthroughStage{name: "stageB"}
	stageC := &testPassthroughStage{name: "stageC"}

	pipeline, err := NewPipelineBuilder().
		AddStage(stageA).
		AddStage(stageB).
		AddStage(stageC).
		Connect("stageA", "stageB").
		Connect("stageA", "stageC").
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Create channels mimicking the stage output channels.
	channels := map[string]chan StreamElement{
		"stageA": make(chan StreamElement, 10),
		"stageB": make(chan StreamElement, 10),
		"stageC": make(chan StreamElement, 10),
	}

	// stageA is not a leaf (has outgoing edges), close its channel.
	close(channels["stageA"])

	// stageB and stageC are leaves; push test elements.
	text1 := "from-B"
	text2 := "from-C"
	channels["stageB"] <- StreamElement{Text: &text1}
	close(channels["stageB"])
	channels["stageC"] <- StreamElement{Text: &text2}
	close(channels["stageC"])

	output := make(chan StreamElement, 10)
	go func() {
		pipeline.collectOutput(channels, output)
		close(output)
	}()

	var collected []string
	for elem := range output {
		if elem.Text != nil {
			collected = append(collected, *elem.Text)
		}
	}

	if len(collected) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(collected))
	}
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

// TestBaseMetadataGoroutineExitsOnContextCancel verifies that the wrapper
// goroutine in Execute exits promptly when the context is cancelled and the
// input channel is blocked (never sends).
func TestBaseMetadataGoroutineExitsOnContextCancel(t *testing.T) {
	p, err := NewPipelineBuilder().
		Chain(&testPassthroughStage{name: "pass"}).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	p.BaseMetadata = map[string]interface{}{"key": "value"}

	// Create an input channel that never sends — simulates a blocked producer.
	blocked := make(chan StreamElement)

	ctx, cancel := context.WithCancel(context.Background())

	output, err := p.Execute(ctx, blocked)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Cancel the context; the wrapper goroutine should stop even though
	// `blocked` never produces an element.
	cancel()

	// Drain the output channel. It must close within a reasonable time;
	// if the goroutine leaked, this would hang.
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-output:
			if !ok {
				return // output closed — goroutine exited correctly
			}
		case <-timer.C:
			t.Fatal("output channel not closed within 2s — wrapper goroutine likely leaked")
		}
	}
}

// TestBaseMetadata tests the BaseMetadata feature on StreamPipeline.
func TestBaseMetadata(t *testing.T) {
	buildPassthroughPipeline := func() *StreamPipeline {
		p, err := NewPipelineBuilder().
			Chain(&testPassthroughStage{name: "passthrough"}).
			Build()
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		return p
	}

	t.Run("nil by default", func(t *testing.T) {
		p := buildPassthroughPipeline()
		if p.BaseMetadata != nil {
			t.Error("BaseMetadata should be nil by default")
		}
	})

	t.Run("no effect when nil", func(t *testing.T) {
		p := buildPassthroughPipeline()
		input := StreamElement{
			Metadata: map[string]interface{}{"key": "value"},
		}
		result, err := p.ExecuteSync(context.Background(), input)
		if err != nil {
			t.Fatalf("ExecuteSync: %v", err)
		}
		if result.Metadata["key"] != "value" {
			t.Error("expected original metadata to be preserved")
		}
	})

	t.Run("merged into elements", func(t *testing.T) {
		p := buildPassthroughPipeline()
		p.BaseMetadata = map[string]interface{}{
			"session_id": "s-123",
			"tenant_id":  "t-456",
		}
		result, err := p.ExecuteSync(context.Background(), StreamElement{})
		if err != nil {
			t.Fatalf("ExecuteSync: %v", err)
		}
		if result.Metadata["session_id"] != "s-123" {
			t.Errorf("expected session_id=s-123, got %v", result.Metadata["session_id"])
		}
		if result.Metadata["tenant_id"] != "t-456" {
			t.Errorf("expected tenant_id=t-456, got %v", result.Metadata["tenant_id"])
		}
	})

	t.Run("per-element metadata takes precedence", func(t *testing.T) {
		p := buildPassthroughPipeline()
		p.BaseMetadata = map[string]interface{}{
			"session_id": "base-session",
			"tenant_id":  "base-tenant",
		}
		input := StreamElement{
			Metadata: map[string]interface{}{
				"session_id": "override-session",
			},
		}
		result, err := p.ExecuteSync(context.Background(), input)
		if err != nil {
			t.Fatalf("ExecuteSync: %v", err)
		}
		if result.Metadata["session_id"] != "override-session" {
			t.Errorf("per-element should override base, got %v", result.Metadata["session_id"])
		}
		if result.Metadata["tenant_id"] != "base-tenant" {
			t.Errorf("non-overridden base key should be preserved, got %v", result.Metadata["tenant_id"])
		}
	})

	t.Run("works with streaming Execute", func(t *testing.T) {
		p := buildPassthroughPipeline()
		p.BaseMetadata = map[string]interface{}{
			"session_id": "stream-session",
		}

		inputChan := make(chan StreamElement, 1)
		inputChan <- StreamElement{
			Metadata: map[string]interface{}{"run_id": "r-1"},
		}
		close(inputChan)

		output, err := p.Execute(context.Background(), inputChan)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}

		var found bool
		for elem := range output {
			if elem.Metadata["session_id"] == "stream-session" && elem.Metadata["run_id"] == "r-1" {
				found = true
			}
		}
		if !found {
			t.Error("expected base and per-element metadata to both be present")
		}
	})

	t.Run("element with nil metadata gets base metadata", func(t *testing.T) {
		p := buildPassthroughPipeline()
		p.BaseMetadata = map[string]interface{}{
			"tenant_id": "t-789",
		}
		input := StreamElement{Metadata: nil}
		result, err := p.ExecuteSync(context.Background(), input)
		if err != nil {
			t.Fatalf("ExecuteSync: %v", err)
		}
		if result.Metadata["tenant_id"] != "t-789" {
			t.Errorf("expected tenant_id=t-789, got %v", result.Metadata["tenant_id"])
		}
	})
}
