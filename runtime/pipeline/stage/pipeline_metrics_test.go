package stage

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
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

	instrumented := instrumentStageInput("test-stage", input)

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

	result := instrumentStageInput("test-stage", input)

	// Should be the same channel (no wrapper goroutine).
	count := 0
	for range result {
		count++
	}
	if count != 1 {
		t.Errorf("got %d elements, want 1", count)
	}
}
