package prometheus

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRecordStageDuration(t *testing.T) {
	// Reset metrics for test isolation
	stageDuration.Reset()

	RecordStageDuration("transform_stage", "transform", 0.5)
	RecordStageDuration("transform_stage", "transform", 1.0)
	RecordStageDuration("accumulate_stage", "accumulate", 0.2)

	// Verify histogram count using CollectAndCount
	count := testutil.CollectAndCount(stageDuration)
	if count == 0 {
		t.Error("Expected non-zero histogram observations")
	}
}

func TestRecordStageElement(t *testing.T) {
	stageElementsTotal.Reset()

	RecordStageElement("my_stage", "success")
	RecordStageElement("my_stage", "success")
	RecordStageElement("my_stage", "error")

	successCount := testutil.ToFloat64(stageElementsTotal.WithLabelValues("my_stage", "success"))
	errorCount := testutil.ToFloat64(stageElementsTotal.WithLabelValues("my_stage", "error"))

	if successCount != 2 {
		t.Errorf("Expected 2 success elements, got %f", successCount)
	}
	if errorCount != 1 {
		t.Errorf("Expected 1 error element, got %f", errorCount)
	}
}

func TestRecordPipelineStartEnd(t *testing.T) {
	pipelinesActive.Set(0)
	pipelineDuration.Reset()

	RecordPipelineStart()
	active := testutil.ToFloat64(pipelinesActive)
	if active != 1 {
		t.Errorf("Expected 1 active pipeline, got %f", active)
	}

	RecordPipelineStart()
	active = testutil.ToFloat64(pipelinesActive)
	if active != 2 {
		t.Errorf("Expected 2 active pipelines, got %f", active)
	}

	RecordPipelineEnd("success", 5.0)
	active = testutil.ToFloat64(pipelinesActive)
	if active != 1 {
		t.Errorf("Expected 1 active pipeline after end, got %f", active)
	}

	RecordPipelineEnd("error", 2.0)
	active = testutil.ToFloat64(pipelinesActive)
	if active != 0 {
		t.Errorf("Expected 0 active pipelines after end, got %f", active)
	}
}

func TestRecordProviderRequest(t *testing.T) {
	providerRequestDuration.Reset()
	providerRequestsTotal.Reset()

	RecordProviderRequest("anthropic", "claude-3", "success", 1.5)
	RecordProviderRequest("openai", "gpt-4", "error", 0.5)

	successCount := testutil.ToFloat64(providerRequestsTotal.WithLabelValues("anthropic", "claude-3", "success"))
	errorCount := testutil.ToFloat64(providerRequestsTotal.WithLabelValues("openai", "gpt-4", "error"))

	if successCount != 1 {
		t.Errorf("Expected 1 success request, got %f", successCount)
	}
	if errorCount != 1 {
		t.Errorf("Expected 1 error request, got %f", errorCount)
	}
}

func TestRecordProviderTokens(t *testing.T) {
	providerTokensTotal.Reset()

	RecordProviderTokens("anthropic", "claude-3", 100, 50, 20)
	RecordProviderTokens("anthropic", "claude-3", 200, 100, 0)

	inputTokens := testutil.ToFloat64(providerTokensTotal.WithLabelValues("anthropic", "claude-3", "input"))
	outputTokens := testutil.ToFloat64(providerTokensTotal.WithLabelValues("anthropic", "claude-3", "output"))
	cachedTokens := testutil.ToFloat64(providerTokensTotal.WithLabelValues("anthropic", "claude-3", "cached"))

	if inputTokens != 300 {
		t.Errorf("Expected 300 input tokens, got %f", inputTokens)
	}
	if outputTokens != 150 {
		t.Errorf("Expected 150 output tokens, got %f", outputTokens)
	}
	if cachedTokens != 20 {
		t.Errorf("Expected 20 cached tokens, got %f", cachedTokens)
	}
}

func TestRecordProviderTokensZeroValues(t *testing.T) {
	providerTokensTotal.Reset()

	// Should not record zero values
	RecordProviderTokens("test", "model", 0, 0, 0)

	inputTokens := testutil.ToFloat64(providerTokensTotal.WithLabelValues("test", "model", "input"))
	outputTokens := testutil.ToFloat64(providerTokensTotal.WithLabelValues("test", "model", "output"))
	cachedTokens := testutil.ToFloat64(providerTokensTotal.WithLabelValues("test", "model", "cached"))

	if inputTokens != 0 {
		t.Errorf("Expected 0 input tokens for zero value, got %f", inputTokens)
	}
	if outputTokens != 0 {
		t.Errorf("Expected 0 output tokens for zero value, got %f", outputTokens)
	}
	if cachedTokens != 0 {
		t.Errorf("Expected 0 cached tokens for zero value, got %f", cachedTokens)
	}
}

func TestRecordProviderCost(t *testing.T) {
	providerCostTotal.Reset()

	RecordProviderCost("anthropic", "claude-3", 0.05)
	RecordProviderCost("anthropic", "claude-3", 0.03)
	RecordProviderCost("openai", "gpt-4", 0.10)

	anthropicCost := testutil.ToFloat64(providerCostTotal.WithLabelValues("anthropic", "claude-3"))
	openaiCost := testutil.ToFloat64(providerCostTotal.WithLabelValues("openai", "gpt-4"))

	if anthropicCost != 0.08 {
		t.Errorf("Expected 0.08 anthropic cost, got %f", anthropicCost)
	}
	if openaiCost != 0.10 {
		t.Errorf("Expected 0.10 openai cost, got %f", openaiCost)
	}
}

func TestRecordProviderCostZero(t *testing.T) {
	providerCostTotal.Reset()

	// Should not record zero cost
	RecordProviderCost("test", "model", 0)
	RecordProviderCost("test", "model", -0.01) // Negative should also be ignored

	cost := testutil.ToFloat64(providerCostTotal.WithLabelValues("test", "model"))
	if cost != 0 {
		t.Errorf("Expected 0 cost for zero/negative value, got %f", cost)
	}
}

func TestRecordToolCall(t *testing.T) {
	toolCallDuration.Reset()
	toolCallsTotal.Reset()

	RecordToolCall("web_search", "success", 2.5)
	RecordToolCall("code_exec", "error", 1.0)
	RecordToolCall("web_search", "success", 1.5)

	successCount := testutil.ToFloat64(toolCallsTotal.WithLabelValues("web_search", "success"))
	errorCount := testutil.ToFloat64(toolCallsTotal.WithLabelValues("code_exec", "error"))

	if successCount != 2 {
		t.Errorf("Expected 2 success tool calls, got %f", successCount)
	}
	if errorCount != 1 {
		t.Errorf("Expected 1 error tool call, got %f", errorCount)
	}
}

func TestRecordValidation(t *testing.T) {
	validationDuration.Reset()
	validationsTotal.Reset()

	RecordValidation("schema_validator", "output", "passed", 0.01)
	RecordValidation("regex_validator", "input", "failed", 0.005)
	RecordValidation("schema_validator", "output", "passed", 0.02)

	passedCount := testutil.ToFloat64(validationsTotal.WithLabelValues("schema_validator", "output", "passed"))
	failedCount := testutil.ToFloat64(validationsTotal.WithLabelValues("regex_validator", "input", "failed"))

	if passedCount != 2 {
		t.Errorf("Expected 2 passed validations, got %f", passedCount)
	}
	if failedCount != 1 {
		t.Errorf("Expected 1 failed validation, got %f", failedCount)
	}
}

func TestNewExporter(t *testing.T) {
	exporter := NewExporter(":9091")
	if exporter == nil {
		t.Fatal("Expected non-nil exporter")
	}
	if exporter.Registry() == nil {
		t.Error("Expected non-nil registry")
	}
}

func TestNewExporterWithRegistry(t *testing.T) {
	reg := prometheus.NewRegistry()
	exporter := NewExporterWithRegistry(":9092", reg)

	if exporter.Registry() != reg {
		t.Error("Expected custom registry to be used")
	}
}

func TestExporterHandler(t *testing.T) {
	reg := prometheus.NewRegistry()
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "test_counter",
		Help: "Test counter",
	})
	reg.MustRegister(counter)
	counter.Inc()

	exporter := NewExporterWithRegistry(":9093", reg)
	handler := exporter.Handler()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "test_counter") {
		t.Error("Expected response to contain test_counter metric")
	}
}

func TestExporterRegister(t *testing.T) {
	reg := prometheus.NewRegistry()
	exporter := NewExporterWithRegistry(":9094", reg)

	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "custom_counter",
		Help: "Custom counter",
	})

	err := exporter.Register(counter)
	if err != nil {
		t.Errorf("Expected no error registering counter, got %v", err)
	}

	// Registering again should fail
	err = exporter.Register(counter)
	if err == nil {
		t.Error("Expected error when registering duplicate counter")
	}
}

func TestExporterMustRegister(t *testing.T) {
	reg := prometheus.NewRegistry()
	exporter := NewExporterWithRegistry(":9095", reg)

	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "must_register_counter",
		Help: "Must register counter",
	})

	// Should not panic
	exporter.MustRegister(counter)
}

func TestExporterStartShutdown(t *testing.T) {
	exporter := NewExporterWithRegistry(":0", prometheus.NewRegistry())

	// Start in goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- exporter.Start()
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := exporter.Shutdown(ctx)
	if err != nil {
		t.Errorf("Expected no error on shutdown, got %v", err)
	}

	// Start should have returned with ErrServerClosed
	select {
	case err := <-errCh:
		if err != http.ErrServerClosed {
			t.Errorf("Expected ErrServerClosed, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("Timeout waiting for server to stop")
	}
}

func TestExporterDoubleStart(t *testing.T) {
	exporter := NewExporterWithRegistry(":0", prometheus.NewRegistry())

	go func() {
		_ = exporter.Start()
	}()

	time.Sleep(100 * time.Millisecond)

	// Second start should return nil immediately
	err := exporter.Start()
	if err != nil {
		t.Errorf("Expected nil on double start, got %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = exporter.Shutdown(ctx)
}

func TestMetricsListener(t *testing.T) {
	// Reset all metrics
	pipelinesActive.Set(0)
	pipelineDuration.Reset()
	stageDuration.Reset()
	stageElementsTotal.Reset()
	providerRequestDuration.Reset()
	providerRequestsTotal.Reset()
	providerTokensTotal.Reset()
	providerCostTotal.Reset()
	toolCallDuration.Reset()
	toolCallsTotal.Reset()
	validationDuration.Reset()
	validationsTotal.Reset()

	listener := NewMetricsListener()

	// Test pipeline started
	listener.Handle(&events.Event{
		Type: events.EventPipelineStarted,
		Data: &events.PipelineStartedData{},
	})
	active := testutil.ToFloat64(pipelinesActive)
	if active != 1 {
		t.Errorf("Expected 1 active pipeline after start event, got %f", active)
	}

	// Test pipeline completed
	listener.Handle(&events.Event{
		Type: events.EventPipelineCompleted,
		Data: &events.PipelineCompletedData{
			Duration: 5 * time.Second,
		},
	})
	active = testutil.ToFloat64(pipelinesActive)
	if active != 0 {
		t.Errorf("Expected 0 active pipelines after completed event, got %f", active)
	}

	// Test pipeline failed
	pipelinesActive.Inc() // Simulate another pipeline start
	listener.Handle(&events.Event{
		Type: events.EventPipelineFailed,
		Data: &events.PipelineFailedData{
			Duration: 2 * time.Second,
		},
	})
	active = testutil.ToFloat64(pipelinesActive)
	if active != 0 {
		t.Errorf("Expected 0 active pipelines after failed event, got %f", active)
	}

	// Test stage completed
	listener.Handle(&events.Event{
		Type: events.EventStageCompleted,
		Data: &events.StageCompletedData{
			Name:      "test_stage",
			StageType: "transform",
			Duration:  500 * time.Millisecond,
		},
	})
	successCount := testutil.ToFloat64(stageElementsTotal.WithLabelValues("test_stage", "success"))
	if successCount != 1 {
		t.Errorf("Expected 1 stage success, got %f", successCount)
	}

	// Test stage failed
	listener.Handle(&events.Event{
		Type: events.EventStageFailed,
		Data: &events.StageFailedData{
			Name:      "test_stage",
			StageType: "transform",
			Duration:  200 * time.Millisecond,
		},
	})
	errorCount := testutil.ToFloat64(stageElementsTotal.WithLabelValues("test_stage", "error"))
	if errorCount != 1 {
		t.Errorf("Expected 1 stage error, got %f", errorCount)
	}

	// Test provider call completed
	listener.Handle(&events.Event{
		Type: events.EventProviderCallCompleted,
		Data: &events.ProviderCallCompletedData{
			Provider:     "anthropic",
			Model:        "claude-3",
			Duration:     2 * time.Second,
			InputTokens:  100,
			OutputTokens: 50,
			CachedTokens: 10,
			Cost:         0.05,
		},
	})
	providerSuccess := testutil.ToFloat64(providerRequestsTotal.WithLabelValues("anthropic", "claude-3", "success"))
	if providerSuccess != 1 {
		t.Errorf("Expected 1 provider success, got %f", providerSuccess)
	}
	inputTokens := testutil.ToFloat64(providerTokensTotal.WithLabelValues("anthropic", "claude-3", "input"))
	if inputTokens != 100 {
		t.Errorf("Expected 100 input tokens, got %f", inputTokens)
	}

	// Test provider call failed
	listener.Handle(&events.Event{
		Type: events.EventProviderCallFailed,
		Data: &events.ProviderCallFailedData{
			Provider: "openai",
			Model:    "gpt-4",
			Duration: 1 * time.Second,
		},
	})
	providerError := testutil.ToFloat64(providerRequestsTotal.WithLabelValues("openai", "gpt-4", "error"))
	if providerError != 1 {
		t.Errorf("Expected 1 provider error, got %f", providerError)
	}

	// Test tool call completed
	listener.Handle(&events.Event{
		Type: events.EventToolCallCompleted,
		Data: &events.ToolCallCompletedData{
			ToolName: "web_search",
			Duration: 500 * time.Millisecond,
			Status:   "success",
		},
	})
	toolSuccess := testutil.ToFloat64(toolCallsTotal.WithLabelValues("web_search", "success"))
	if toolSuccess != 1 {
		t.Errorf("Expected 1 tool success, got %f", toolSuccess)
	}

	// Test tool call failed
	listener.Handle(&events.Event{
		Type: events.EventToolCallFailed,
		Data: &events.ToolCallFailedData{
			ToolName: "code_exec",
			Duration: 1 * time.Second,
		},
	})
	toolError := testutil.ToFloat64(toolCallsTotal.WithLabelValues("code_exec", "error"))
	if toolError != 1 {
		t.Errorf("Expected 1 tool error, got %f", toolError)
	}

	// Test validation passed
	listener.Handle(&events.Event{
		Type: events.EventValidationPassed,
		Data: &events.ValidationPassedData{
			ValidatorName: "schema_validator",
			ValidatorType: "output",
			Duration:      10 * time.Millisecond,
		},
	})
	validationPassed := testutil.ToFloat64(validationsTotal.WithLabelValues("schema_validator", "output", "passed"))
	if validationPassed != 1 {
		t.Errorf("Expected 1 validation passed, got %f", validationPassed)
	}

	// Test validation failed
	listener.Handle(&events.Event{
		Type: events.EventValidationFailed,
		Data: &events.ValidationFailedData{
			ValidatorName: "regex_validator",
			ValidatorType: "input",
			Duration:      5 * time.Millisecond,
		},
	})
	validationFailed := testutil.ToFloat64(validationsTotal.WithLabelValues("regex_validator", "input", "failed"))
	if validationFailed != 1 {
		t.Errorf("Expected 1 validation failed, got %f", validationFailed)
	}
}

func TestMetricsListenerFunction(t *testing.T) {
	listener := NewMetricsListener()
	fn := listener.Listener()

	if fn == nil {
		t.Error("Expected non-nil listener function")
	}

	// Verify it's callable
	pipelinesActive.Set(0)
	fn(&events.Event{
		Type: events.EventPipelineStarted,
		Data: &events.PipelineStartedData{},
	})

	active := testutil.ToFloat64(pipelinesActive)
	if active != 1 {
		t.Errorf("Expected 1 active pipeline via listener function, got %f", active)
	}
}

func TestMetricsListenerToolCallCompletedWithError(t *testing.T) {
	toolCallsTotal.Reset()

	listener := NewMetricsListener()

	// Tool call completed with error status
	listener.Handle(&events.Event{
		Type: events.EventToolCallCompleted,
		Data: &events.ToolCallCompletedData{
			ToolName: "failing_tool",
			Duration: 100 * time.Millisecond,
			Status:   "error",
		},
	})

	errorCount := testutil.ToFloat64(toolCallsTotal.WithLabelValues("failing_tool", "error"))
	if errorCount != 1 {
		t.Errorf("Expected 1 tool error for completed with error status, got %f", errorCount)
	}
}

func TestMetricsListenerIgnoresUnknownEvents(t *testing.T) {
	listener := NewMetricsListener()

	// These should not panic
	listener.Handle(&events.Event{
		Type: events.EventContextBuilt,
		Data: &events.ContextBuiltData{},
	})

	listener.Handle(&events.Event{
		Type: events.EventMessageCreated,
		Data: &events.MessageCreatedData{},
	})

	listener.Handle(&events.Event{
		Type: events.EventStageStarted,
		Data: &events.StageStartedData{},
	})
}

func TestMetricsListenerNilData(t *testing.T) {
	listener := NewMetricsListener()

	// These should not panic even with nil data
	listener.Handle(&events.Event{
		Type: events.EventPipelineCompleted,
		Data: nil,
	})

	listener.Handle(&events.Event{
		Type: events.EventStageCompleted,
		Data: nil,
	})
}
