package stage

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRouterStage_RoutesToSingleDestination(t *testing.T) {
	// Router that sends all elements to "output1"
	router := NewRouterStage("router", func(elem *StreamElement) []string {
		return []string{"output1"}
	})

	output1 := make(chan StreamElement, 10)
	router.RegisterOutput("output1", output1)

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1) // Default output (not used in routing)

	text := "test"
	input <- StreamElement{Text: &text}
	close(input)

	err := router.Process(context.Background(), input, output)
	require.NoError(t, err)

	// Check element was routed to output1
	select {
	case elem := <-output1:
		assert.Equal(t, "test", *elem.Text)
	default:
		t.Fatal("Expected element in output1")
	}
}

func TestRouterStage_RoutesToMultipleDestinations(t *testing.T) {
	// Router that sends elements to both outputs
	router := NewRouterStage("router", func(elem *StreamElement) []string {
		return []string{"output1", "output2"}
	})

	output1 := make(chan StreamElement, 10)
	output2 := make(chan StreamElement, 10)
	router.RegisterOutput("output1", output1)
	router.RegisterOutput("output2", output2)

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	text := "broadcast"
	input <- StreamElement{Text: &text}
	close(input)

	err := router.Process(context.Background(), input, output)
	require.NoError(t, err)

	// Check element was routed to both outputs
	select {
	case elem := <-output1:
		assert.Equal(t, "broadcast", *elem.Text)
	default:
		t.Fatal("Expected element in output1")
	}

	select {
	case elem := <-output2:
		assert.Equal(t, "broadcast", *elem.Text)
	default:
		t.Fatal("Expected element in output2")
	}
}

func TestRouterStage_DropsElementWithNoDestinations(t *testing.T) {
	// Router that drops all elements
	router := NewRouterStage("router", func(elem *StreamElement) []string {
		return []string{} // No destinations
	})

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	text := "dropped"
	input <- StreamElement{Text: &text}
	close(input)

	err := router.Process(context.Background(), input, output)
	require.NoError(t, err)
}

func TestMergeStage_MergesMultipleInputs(t *testing.T) {
	merge := NewMergeStage("merge", 2)

	input1 := make(chan StreamElement, 1)
	input2 := make(chan StreamElement, 1)
	output := make(chan StreamElement, 10)

	text1 := "from input 1"
	text2 := "from input 2"
	input1 <- StreamElement{Text: &text1}
	input2 <- StreamElement{Text: &text2}
	close(input1)
	close(input2)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := merge.ProcessMultiple(context.Background(), []<-chan StreamElement{input1, input2}, output)
		assert.NoError(t, err)
	}()

	wg.Wait()

	// Collect all outputs
	var results []string
	for elem := range output {
		if elem.Text != nil {
			results = append(results, *elem.Text)
		}
	}

	assert.Len(t, results, 2)
	assert.Contains(t, results, "from input 1")
	assert.Contains(t, results, "from input 2")
}

func TestMergeStage_AddsInputIndexMetadata(t *testing.T) {
	merge := NewMergeStage("merge", 2)

	input1 := make(chan StreamElement, 1)
	input2 := make(chan StreamElement, 1)
	output := make(chan StreamElement, 10)

	text1 := "input1"
	text2 := "input2"
	input1 <- StreamElement{Text: &text1, Metadata: map[string]interface{}{}}
	input2 <- StreamElement{Text: &text2, Metadata: map[string]interface{}{}}
	close(input1)
	close(input2)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = merge.ProcessMultiple(context.Background(), []<-chan StreamElement{input1, input2}, output)
	}()

	wg.Wait()

	// Check metadata contains input index
	for elem := range output {
		_, hasIndex := elem.Metadata["merge_input_index"]
		assert.True(t, hasIndex, "Element should have merge_input_index metadata")
	}
}

func TestStageMetrics_RecordsLatency(t *testing.T) {
	metrics := NewStageMetrics("test_stage")

	latency := 100 * time.Millisecond
	metrics.RecordElement(latency, false)

	stats := metrics.GetMetrics()
	assert.Equal(t, int64(1), stats.ElementsIn)
	assert.Equal(t, int64(1), stats.ElementsOut)
	assert.GreaterOrEqual(t, stats.AvgLatency, latency)
	assert.Equal(t, int64(0), stats.ElementsErrored)
}

func TestStageMetrics_RecordsErrors(t *testing.T) {
	metrics := NewStageMetrics("test_stage")

	latency := 50 * time.Millisecond
	metrics.RecordElement(latency, true)

	stats := metrics.GetMetrics()
	assert.Equal(t, int64(1), stats.ElementsIn)
	assert.Equal(t, int64(1), stats.ElementsErrored)
}

func TestStageMetrics_Reset(t *testing.T) {
	metrics := NewStageMetrics("test_stage")

	latency := 50 * time.Millisecond
	metrics.RecordElement(latency, false)
	metrics.RecordElement(latency, false)

	stats := metrics.GetMetrics()
	assert.Equal(t, int64(2), stats.ElementsIn)

	metrics.Reset()

	stats = metrics.GetMetrics()
	assert.Equal(t, int64(0), stats.ElementsIn)
}

func TestMetricsStage_WrapsInnerStage(t *testing.T) {
	inner := NewPassthroughStage("inner")
	metricsStage := NewMetricsStage(inner)

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	text := "test"
	input <- StreamElement{Text: &text, Timestamp: time.Now()}
	close(input)

	err := metricsStage.Process(context.Background(), input, output)
	require.NoError(t, err)

	// Check element passed through
	select {
	case elem := <-output:
		assert.Equal(t, "test", *elem.Text)
	default:
		t.Fatal("Expected element in output")
	}

	// Check metrics were recorded
	stats := metricsStage.GetMetrics()
	assert.Equal(t, int64(1), stats.ElementsIn)
}

func TestPriorityChannel_PrioritizesHighPriorityElements(t *testing.T) {
	pc := NewPriorityChannel(10)

	// Send elements in reverse priority order
	lowPriority := StreamElement{Priority: PriorityLow}
	normalPriority := StreamElement{Priority: PriorityNormal}
	highPriority := StreamElement{Priority: PriorityHigh}
	criticalPriority := StreamElement{Priority: PriorityCritical}

	ctx := context.Background()
	require.NoError(t, pc.Send(ctx, lowPriority))
	require.NoError(t, pc.Send(ctx, normalPriority))
	require.NoError(t, pc.Send(ctx, highPriority))
	require.NoError(t, pc.Send(ctx, criticalPriority))

	// Receive should return highest priority first
	elem, ok, err := pc.Receive(ctx)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, PriorityCritical, elem.Priority)

	elem, ok, err = pc.Receive(ctx)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, PriorityHigh, elem.Priority)

	elem, ok, err = pc.Receive(ctx)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, PriorityNormal, elem.Priority)

	elem, ok, err = pc.Receive(ctx)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, PriorityLow, elem.Priority)
}

func TestPriorityChannel_ReturnsErrorOnClosedChannel(t *testing.T) {
	pc := NewPriorityChannel(10)
	pc.Close()

	_, ok, _ := pc.Receive(context.Background())
	assert.False(t, ok)
}

func TestPriorityChannel_Len(t *testing.T) {
	pc := NewPriorityChannel(10)

	assert.Equal(t, 0, pc.Len())

	ctx := context.Background()
	_ = pc.Send(ctx, StreamElement{Priority: PriorityNormal})
	_ = pc.Send(ctx, StreamElement{Priority: PriorityHigh})

	assert.Equal(t, 2, pc.Len())
}

func TestTracingStage_AddsTraceInfo(t *testing.T) {
	inner := NewPassthroughStage("inner")
	tracing := NewTracingStage(inner, func() string { return "test-trace-123" })

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	input <- StreamElement{Metadata: map[string]interface{}{}}
	close(input)

	err := tracing.Process(context.Background(), input, output)
	require.NoError(t, err)

	elem := <-output
	traceID, stageTimes := GetTraceInfo(&elem)
	require.NotEmpty(t, traceID)
	require.NotNil(t, stageTimes)

	assert.Equal(t, "test-trace-123", traceID)
}

func TestTracingStage_PreservesExistingTraceID(t *testing.T) {
	inner := NewPassthroughStage("inner")
	existingTraceID := "existing-trace-123"
	tracing := NewTracingStage(inner, func() string { return existingTraceID })

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	input <- StreamElement{
		Metadata: map[string]interface{}{
			"trace_id": existingTraceID,
		},
	}
	close(input)

	err := tracing.Process(context.Background(), input, output)
	require.NoError(t, err)

	elem := <-output
	traceID, _ := GetTraceInfo(&elem)

	assert.Equal(t, existingTraceID, traceID)
}
