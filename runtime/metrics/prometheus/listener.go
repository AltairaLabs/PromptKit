// Package prometheus provides Prometheus metrics exporters for PromptKit pipelines.
package prometheus

import (
	"github.com/AltairaLabs/PromptKit/runtime/events"
)

// Status constants for metric labels.
const (
	statusSuccess = "success"
	statusError   = "error"
	statusPassed  = "passed"
	statusFailed  = "failed"
)

// MetricsListener records pipeline events as Prometheus metrics.
// It implements the events.Listener signature and should be registered
// with an EventBus using SubscribeAll.
type MetricsListener struct{}

// NewMetricsListener creates a new MetricsListener.
func NewMetricsListener() *MetricsListener {
	return &MetricsListener{}
}

// Handle processes an event and records relevant metrics.
// This method is designed to be used with EventBus.SubscribeAll.
func (l *MetricsListener) Handle(event *events.Event) {
	//exhaustive:ignore
	switch event.Type {
	case events.EventPipelineStarted:
		RecordPipelineStart()
	case events.EventPipelineCompleted:
		l.handlePipelineCompleted(event)
	case events.EventPipelineFailed:
		l.handlePipelineFailed(event)
	case events.EventStageCompleted:
		l.handleStageCompleted(event)
	case events.EventStageFailed:
		l.handleStageFailed(event)
	case events.EventProviderCallCompleted:
		l.handleProviderCallCompleted(event)
	case events.EventProviderCallFailed:
		l.handleProviderCallFailed(event)
	case events.EventToolCallCompleted:
		l.handleToolCallCompleted(event)
	case events.EventToolCallFailed:
		l.handleToolCallFailed(event)
	case events.EventValidationPassed:
		l.handleValidationPassed(event)
	case events.EventValidationFailed:
		l.handleValidationFailed(event)
	default:
		// Ignore events that don't have metrics
	}
}

func (l *MetricsListener) handlePipelineCompleted(event *events.Event) {
	if data, ok := event.Data.(*events.PipelineCompletedData); ok {
		RecordPipelineEnd(statusSuccess, data.Duration.Seconds())
	}
}

func (l *MetricsListener) handlePipelineFailed(event *events.Event) {
	if data, ok := event.Data.(*events.PipelineFailedData); ok {
		RecordPipelineEnd(statusError, data.Duration.Seconds())
	}
}

func (l *MetricsListener) handleStageCompleted(event *events.Event) {
	if data, ok := event.Data.(*events.StageCompletedData); ok {
		RecordStageDuration(data.Name, data.StageType, data.Duration.Seconds())
		RecordStageElement(data.Name, statusSuccess)
	}
}

func (l *MetricsListener) handleStageFailed(event *events.Event) {
	if data, ok := event.Data.(*events.StageFailedData); ok {
		RecordStageDuration(data.Name, data.StageType, data.Duration.Seconds())
		RecordStageElement(data.Name, statusError)
	}
}

func (l *MetricsListener) handleProviderCallCompleted(event *events.Event) {
	if data, ok := event.Data.(*events.ProviderCallCompletedData); ok {
		RecordProviderRequest(data.Provider, data.Model, statusSuccess, data.Duration.Seconds())
		RecordProviderTokens(data.Provider, data.Model, data.InputTokens, data.OutputTokens, data.CachedTokens)
		RecordProviderCost(data.Provider, data.Model, data.Cost)
	}
}

func (l *MetricsListener) handleProviderCallFailed(event *events.Event) {
	if data, ok := event.Data.(*events.ProviderCallFailedData); ok {
		RecordProviderRequest(data.Provider, data.Model, statusError, data.Duration.Seconds())
	}
}

func (l *MetricsListener) handleToolCallCompleted(event *events.Event) {
	if data, ok := event.Data.(*events.ToolCallCompletedData); ok {
		status := statusSuccess
		if data.Status == statusError {
			status = statusError
		}
		RecordToolCall(data.ToolName, status, data.Duration.Seconds())
	}
}

func (l *MetricsListener) handleToolCallFailed(event *events.Event) {
	if data, ok := event.Data.(*events.ToolCallFailedData); ok {
		RecordToolCall(data.ToolName, statusError, data.Duration.Seconds())
	}
}

func (l *MetricsListener) handleValidationPassed(event *events.Event) {
	if data, ok := event.Data.(*events.ValidationPassedData); ok {
		RecordValidation(data.ValidatorName, data.ValidatorType, statusPassed, data.Duration.Seconds())
	}
}

func (l *MetricsListener) handleValidationFailed(event *events.Event) {
	if data, ok := event.Data.(*events.ValidationFailedData); ok {
		RecordValidation(data.ValidatorName, data.ValidatorType, statusFailed, data.Duration.Seconds())
	}
}

// Listener returns an events.Listener function that can be registered with an EventBus.
func (l *MetricsListener) Listener() events.Listener {
	return l.Handle
}
