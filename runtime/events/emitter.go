package events

import "time"

// Emitter provides helpers for publishing runtime events with shared metadata.
type Emitter struct {
	bus            *EventBus
	runID          string
	sessionID      string
	conversationID string
}

// NewEmitter creates a new event emitter.
func NewEmitter(bus *EventBus, runID, sessionID, conversationID string) *Emitter {
	return &Emitter{
		bus:            bus,
		runID:          runID,
		sessionID:      sessionID,
		conversationID: conversationID,
	}
}

// emit publishes an event with shared context fields.
func (e *Emitter) emit(eventType EventType, data EventData) {
	if e == nil || e.bus == nil {
		return
	}

	event := &Event{
		Type:           eventType,
		Timestamp:      time.Now(),
		RunID:          e.runID,
		SessionID:      e.sessionID,
		ConversationID: e.conversationID,
		Data:           data,
	}

	e.bus.Publish(event)
}

// PipelineStarted emits the pipeline.started event.
func (e *Emitter) PipelineStarted(middlewareCount int) {
	e.emit(EventPipelineStarted, PipelineStartedData{
		MiddlewareCount: middlewareCount,
	})
}

// PipelineCompleted emits the pipeline.completed event.
func (e *Emitter) PipelineCompleted(
	duration time.Duration,
	totalCost float64,
	inputTokens, outputTokens, messageCount int,
) {
	e.emit(EventPipelineCompleted, PipelineCompletedData{
		Duration:     duration,
		TotalCost:    totalCost,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		MessageCount: messageCount,
	})
}

// PipelineFailed emits the pipeline.failed event.
func (e *Emitter) PipelineFailed(err error, duration time.Duration) {
	e.emit(EventPipelineFailed, PipelineFailedData{
		Error:    err,
		Duration: duration,
	})
}

// MiddlewareStarted emits the middleware.started event.
func (e *Emitter) MiddlewareStarted(name string, index int) {
	e.emit(EventMiddlewareStarted, MiddlewareStartedData{
		Name:  name,
		Index: index,
	})
}

// MiddlewareCompleted emits the middleware.completed event.
func (e *Emitter) MiddlewareCompleted(name string, index int, duration time.Duration) {
	e.emit(EventMiddlewareCompleted, MiddlewareCompletedData{
		Name:     name,
		Index:    index,
		Duration: duration,
	})
}

// MiddlewareFailed emits the middleware.failed event.
func (e *Emitter) MiddlewareFailed(name string, index int, err error, duration time.Duration) {
	e.emit(EventMiddlewareFailed, MiddlewareFailedData{
		Name:     name,
		Index:    index,
		Error:    err,
		Duration: duration,
	})
}

// ProviderCallStarted emits the provider.call.started event.
func (e *Emitter) ProviderCallStarted(provider, model string, messageCount, toolCount int) {
	e.emit(EventProviderCallStarted, ProviderCallStartedData{
		Provider:     provider,
		Model:        model,
		MessageCount: messageCount,
		ToolCount:    toolCount,
	})
}

// ProviderCallCompleted emits the provider.call.completed event.
func (e *Emitter) ProviderCallCompleted(data *ProviderCallCompletedData) {
	if data == nil {
		return
	}
	e.emit(EventProviderCallCompleted, data)
}

// ProviderCallFailed emits the provider.call.failed event.
func (e *Emitter) ProviderCallFailed(provider, model string, err error, duration time.Duration) {
	e.emit(EventProviderCallFailed, ProviderCallFailedData{
		Provider: provider,
		Model:    model,
		Error:    err,
		Duration: duration,
	})
}

// ToolCallStarted emits the tool.call.started event.
func (e *Emitter) ToolCallStarted(toolName, callID string, args map[string]interface{}) {
	e.emit(EventToolCallStarted, ToolCallStartedData{
		ToolName: toolName,
		CallID:   callID,
		Args:     args,
	})
}

// ToolCallCompleted emits the tool.call.completed event.
func (e *Emitter) ToolCallCompleted(toolName, callID string, duration time.Duration, status string) {
	e.emit(EventToolCallCompleted, ToolCallCompletedData{
		ToolName: toolName,
		CallID:   callID,
		Duration: duration,
		Status:   status,
	})
}

// ToolCallFailed emits the tool.call.failed event.
func (e *Emitter) ToolCallFailed(toolName, callID string, err error, duration time.Duration) {
	e.emit(EventToolCallFailed, ToolCallFailedData{
		ToolName: toolName,
		CallID:   callID,
		Error:    err,
		Duration: duration,
	})
}

// ValidationStarted emits the validation.started event.
func (e *Emitter) ValidationStarted(validatorName, validatorType string) {
	e.emit(EventValidationStarted, ValidationStartedData{
		ValidatorName: validatorName,
		ValidatorType: validatorType,
	})
}

// ValidationPassed emits the validation.passed event.
func (e *Emitter) ValidationPassed(validatorName, validatorType string, duration time.Duration) {
	e.emit(EventValidationPassed, ValidationPassedData{
		ValidatorName: validatorName,
		ValidatorType: validatorType,
		Duration:      duration,
	})
}

// ValidationFailed emits the validation.failed event.
func (e *Emitter) ValidationFailed(
	validatorName, validatorType string,
	err error,
	duration time.Duration,
	violations []string,
) {
	e.emit(EventValidationFailed, ValidationFailedData{
		ValidatorName: validatorName,
		ValidatorType: validatorType,
		Error:         err,
		Duration:      duration,
		Violations:    violations,
	})
}

// ContextBuilt emits the context.built event.
func (e *Emitter) ContextBuilt(messageCount, tokenCount, tokenBudget int, truncated bool) {
	e.emit(EventContextBuilt, ContextBuiltData{
		MessageCount: messageCount,
		TokenCount:   tokenCount,
		TokenBudget:  tokenBudget,
		Truncated:    truncated,
	})
}

// TokenBudgetExceeded emits the context.token_budget_exceeded event.
func (e *Emitter) TokenBudgetExceeded(required, budget, excess int) {
	e.emit(EventTokenBudgetExceeded, TokenBudgetExceededData{
		RequiredTokens: required,
		Budget:         budget,
		Excess:         excess,
	})
}

// StateLoaded emits the state.loaded event.
func (e *Emitter) StateLoaded(conversationID string, messageCount int) {
	e.emit(EventStateLoaded, StateLoadedData{
		ConversationID: conversationID,
		MessageCount:   messageCount,
	})
}

// StateSaved emits the state.saved event.
func (e *Emitter) StateSaved(conversationID string, messageCount int) {
	e.emit(EventStateSaved, StateSavedData{
		ConversationID: conversationID,
		MessageCount:   messageCount,
	})
}

// StreamInterrupted emits the stream.interrupted event.
func (e *Emitter) StreamInterrupted(reason string) {
	e.emit(EventStreamInterrupted, StreamInterruptedData{
		Reason: reason,
	})
}

// EmitCustom allows middleware to emit arbitrary event types with structured payloads.
func (e *Emitter) EmitCustom(
	eventType EventType,
	middlewareName, eventName string,
	data map[string]interface{},
	message string,
) {
	e.emit(eventType, CustomEventData{
		MiddlewareName: middlewareName,
		EventName:      eventName,
		Data:           data,
		Message:        message,
	})
}

// MessageCreated emits the message.created event.
func (e *Emitter) MessageCreated(role, content string, index int) {
	e.emit(EventMessageCreated, MessageCreatedData{
		Role:    role,
		Content: content,
		Index:   index,
	})
}

// MessageUpdated emits the message.updated event.
func (e *Emitter) MessageUpdated(index int, latencyMs int64, inputTokens, outputTokens int, totalCost float64) {
	e.emit(EventMessageUpdated, MessageUpdatedData{
		Index:        index,
		LatencyMs:    latencyMs,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalCost:    totalCost,
	})
}
