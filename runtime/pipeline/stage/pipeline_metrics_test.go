package stage

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

func TestInstrumentStageInput_CountsElements(t *testing.T) {
	// NOT parallel: both tests mutate the global DefaultStreamMetrics.
	providers.ResetDefaultStreamMetrics()
	t.Cleanup(providers.ResetDefaultStreamMetrics)
	reg := prometheus.NewRegistry()
	m := providers.RegisterDefaultStreamMetrics(reg, "test", nil)

	input := make(chan StreamElement, 3)
	input <- StreamElement{Audio: &AudioData{Samples: []byte{1, 2, 3}, SampleRate: 16000, Format: AudioFormatPCM16}}
	input <- StreamElement{Audio: &AudioData{Samples: []byte{4, 5}, SampleRate: 16000, Format: AudioFormatPCM16}}
	text := "hello"
	input <- StreamElement{Text: &text}
	close(input)

	instrumented := instrumentStageInput("test-stage", input, nil)

	// Drain
	count := 0
	for range instrumented {
		count++
	}

	if count != 3 {
		t.Errorf("got %d elements, want 3", count)
	}

	if got := testutil.ToFloat64(m.PipelineStageElementsVec().WithLabelValues("test-stage")); got != 3 {
		t.Errorf("element counter = %v, want 3", got)
	}
	if got := testutil.ToFloat64(m.PipelineStageAudioBytesVec().WithLabelValues("test-stage")); got != 5 {
		t.Errorf("audio bytes counter = %v, want 5", got)
	}
}

func TestInstrumentStageInput_NilMetricsPassthrough(t *testing.T) {
	// NOT parallel: both tests mutate the global DefaultStreamMetrics.
	providers.ResetDefaultStreamMetrics()
	t.Cleanup(providers.ResetDefaultStreamMetrics)

	input := make(chan StreamElement, 1)
	input <- StreamElement{}
	close(input)

	result := instrumentStageInput("test-stage", input, nil)

	// Should be the same channel (no wrapper goroutine).
	count := 0
	for range result {
		count++
	}
	if count != 1 {
		t.Errorf("got %d elements, want 1", count)
	}
}

// The metrics relay stops once its stage has returned, instead of waiting
// forever to hand an element to a stage that stopped reading (#2847).
func TestInstrumentStageInput_StopsWhenTheStageReturns(t *testing.T) {
	// NOT parallel: mutates the global DefaultStreamMetrics.
	providers.ResetDefaultStreamMetrics()
	t.Cleanup(providers.ResetDefaultStreamMetrics)
	providers.RegisterDefaultStreamMetrics(prometheus.NewRegistry(), "test", nil)

	input := make(chan StreamElement) // unbuffered: so is the relay's output
	stageDone := make(chan struct{})
	instrumented := instrumentStageInput("test-stage", input, stageDone)
	input <- StreamElement{} // the relay now waits to hand it on; nobody reads
	close(stageDone)

	time.Sleep(50 * time.Millisecond) // the relay sees its stage gone

	select {
	case _, ok := <-instrumented:
		if ok {
			t.Fatal("the relay handed on an element after its stage returned")
		}
	case <-time.After(time.Second):
		t.Fatal("the relay did not stop after its stage returned")
	}
	close(input)
}

// An idle relay, its input still open, also stops once its stage returns.
func TestInstrumentStageInput_IdleRelayStopsWhenTheStageReturns(t *testing.T) {
	// NOT parallel: mutates the global DefaultStreamMetrics.
	providers.ResetDefaultStreamMetrics()
	t.Cleanup(providers.ResetDefaultStreamMetrics)
	providers.RegisterDefaultStreamMetrics(prometheus.NewRegistry(), "test", nil)

	input := make(chan StreamElement)
	defer close(input)
	stageDone := make(chan struct{})
	instrumented := instrumentStageInput("test-stage", input, stageDone)
	close(stageDone)

	select {
	case _, ok := <-instrumented:
		if ok {
			t.Fatal("the relay handed on an element it was never given")
		}
	case <-time.After(time.Second):
		t.Fatal("the idle relay did not stop after its stage returned")
	}
}
