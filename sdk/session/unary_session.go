package session

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

type unarySession struct {
	id        string
	store     statestore.Store
	pipeline  *stage.StreamPipeline
	variables map[string]string
	mu        sync.RWMutex
}

// NewUnarySession creates a new unary session.
func NewUnarySession(cfg UnarySessionConfig) (UnarySession, error) {
	if cfg.ConversationID == "" {
		cfg.ConversationID = uuid.New().String()
	}
	if cfg.StateStore == nil {
		cfg.StateStore = statestore.NewMemoryStore()
	}
	if cfg.Pipeline == nil {
		return nil, fmt.Errorf("pipeline is required")
	}

	_, err := cfg.StateStore.Load(context.Background(), cfg.ConversationID)
	if err != nil {
		initialState := &statestore.ConversationState{
			ID:       cfg.ConversationID,
			UserID:   cfg.UserID,
			Messages: []types.Message{},
			Metadata: cfg.Metadata,
		}
		if err := cfg.StateStore.Save(context.Background(), initialState); err != nil {
			return nil, fmt.Errorf("failed to initialize conversation state: %w", err)
		}
	}

	return &unarySession{
		id:        cfg.ConversationID,
		store:     cfg.StateStore,
		pipeline:  cfg.Pipeline,
		variables: cfg.Variables,
	}, nil
}

// ID implements TextSession.
func (s *unarySession) ID() string {
	return s.id
}

// Execute implements TextSession.
func (s *unarySession) Execute(ctx context.Context, role, content string) (*pipeline.ExecutionResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Create input message
	message := types.Message{
		Role:    role,
		Content: content,
	}

	// Create input element
	inputElem := stage.StreamElement{
		Message:  &message,
		Metadata: map[string]interface{}{"variables": s.variables},
	}

	// Execute synchronously
	result, err := s.pipeline.ExecuteSync(ctx, inputElem)
	if err != nil {
		return nil, err
	}

	// Convert stage.ExecutionResult to pipeline.ExecutionResult
	return convertExecutionResult(result), nil
}

// ExecuteWithMessage implements TextSession.
//
//nolint:gocritic // Interface signature cannot be changed
func (s *unarySession) ExecuteWithMessage(
	ctx context.Context,
	message types.Message,
) (*pipeline.ExecutionResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Create input element
	inputElem := stage.StreamElement{
		Message:  &message,
		Metadata: map[string]interface{}{"variables": s.variables},
	}

	// Execute synchronously
	result, err := s.pipeline.ExecuteSync(ctx, inputElem)
	if err != nil {
		return nil, err
	}

	// Convert stage.ExecutionResult to pipeline.ExecutionResult
	return convertExecutionResult(result), nil
}

// ResumeWithToolResults injects tool result messages and re-executes the pipeline.
func (s *unarySession) ResumeWithToolResults(
	ctx context.Context,
	toolResults []types.Message,
) (*pipeline.ExecutionResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Build input elements: one per tool result message
	inputElems := make([]stage.StreamElement, 0, len(toolResults))
	for i := range toolResults {
		inputElems = append(inputElems, stage.StreamElement{
			Message:  &toolResults[i],
			Metadata: map[string]interface{}{"variables": s.variables},
		})
	}

	result, err := s.pipeline.ExecuteSync(ctx, inputElems...)
	if err != nil {
		return nil, err
	}

	return convertExecutionResult(result), nil
}

// ExecuteStream implements TextSession.
func (s *unarySession) ExecuteStream(ctx context.Context, role, content string) (<-chan providers.StreamChunk, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Create input message
	message := types.Message{
		Role:    role,
		Content: content,
	}

	// Create input element
	inputElem := stage.StreamElement{
		Message:  &message,
		Metadata: map[string]interface{}{"variables": s.variables},
	}

	// Create input channel
	inputChan := make(chan stage.StreamElement, 1)
	inputChan <- inputElem
	close(inputChan)

	// Execute as stream
	outputChan, err := s.pipeline.Execute(ctx, inputChan)
	if err != nil {
		return nil, err
	}

	// Convert stage output to StreamChunk output
	return convertStreamOutput(ctx, outputChan), nil
}

// ExecuteStreamWithMessage implements TextSession.
//
//nolint:gocritic // Interface signature cannot be changed
func (s *unarySession) ExecuteStreamWithMessage(
	ctx context.Context,
	message types.Message,
) (<-chan providers.StreamChunk, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Create input element
	inputElem := stage.StreamElement{
		Message:  &message,
		Metadata: map[string]interface{}{"variables": s.variables},
	}

	// Create input channel
	inputChan := make(chan stage.StreamElement, 1)
	inputChan <- inputElem
	close(inputChan)

	// Execute as stream
	outputChan, err := s.pipeline.Execute(ctx, inputChan)
	if err != nil {
		return nil, err
	}

	// Convert stage output to StreamChunk output
	return convertStreamOutput(ctx, outputChan), nil
}

// ResumeStreamWithToolResults injects tool result messages and returns a streaming channel.
func (s *unarySession) ResumeStreamWithToolResults(
	ctx context.Context,
	toolResults []types.Message,
) (<-chan providers.StreamChunk, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Build input elements: one per tool result message
	inputElems := make([]stage.StreamElement, 0, len(toolResults))
	for i := range toolResults {
		inputElems = append(inputElems, stage.StreamElement{
			Message:  &toolResults[i],
			Metadata: map[string]interface{}{"variables": s.variables},
		})
	}

	// Create input channel from tool result messages
	inputChan := make(chan stage.StreamElement, len(inputElems))
	for i := range inputElems {
		inputChan <- inputElems[i]
	}
	close(inputChan)

	// Execute as stream
	outputChan, err := s.pipeline.Execute(ctx, inputChan)
	if err != nil {
		return nil, err
	}

	return convertStreamOutput(ctx, outputChan), nil
}

// SetVar sets a template variable that will be available for substitution.
func (s *unarySession) SetVar(name, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.variables == nil {
		s.variables = make(map[string]string)
	}
	s.variables[name] = value
}

// GetVar retrieves the value of a template variable.
func (s *unarySession) GetVar(name string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.variables[name]
	return val, ok
}

// Variables returns a copy of all template variables.
func (s *unarySession) Variables() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	vars := make(map[string]string, len(s.variables))
	for k, v := range s.variables {
		vars[k] = v
	}
	return vars
}

// Messages implements BaseSession.
func (s *unarySession) Messages(ctx context.Context) ([]types.Message, error) {
	state, err := s.store.Load(ctx, s.id)
	if err != nil {
		return nil, err
	}
	return state.Messages, nil
}

// Clear implements BaseSession.
func (s *unarySession) Clear(ctx context.Context) error {
	state := &statestore.ConversationState{
		ID:       s.id,
		Messages: nil,
	}
	return s.store.Save(ctx, state)
}

// ForkSession implements UnarySession.
func (s *unarySession) ForkSession(
	ctx context.Context,
	forkID string,
	pipelineArg *stage.StreamPipeline,
) (UnarySession, error) {
	// Fork the state in the store
	if err := s.store.Fork(ctx, s.id, forkID); err != nil {
		return nil, fmt.Errorf("failed to fork state: %w", err)
	}

	// Copy variables
	s.mu.RLock()
	forkVars := make(map[string]string, len(s.variables))
	for k, v := range s.variables {
		forkVars[k] = v
	}
	s.mu.RUnlock()

	// Create new session with forked state
	return &unarySession{
		id:        forkID,
		store:     s.store,
		pipeline:  pipelineArg,
		variables: forkVars,
	}, nil
}

// convertExecutionResult converts stage.ExecutionResult to pipeline.ExecutionResult
func convertExecutionResult(result *stage.ExecutionResult) *pipeline.ExecutionResult {
	pipelineResult := &pipeline.ExecutionResult{
		Messages: result.Messages,
		Metadata: result.Metadata,
		Trace: pipeline.ExecutionTrace{
			LLMCalls: make([]pipeline.LLMCall, 0),
			Events:   make([]pipeline.TraceEvent, 0),
		},
	}

	if result.Response != nil {
		pipelineResult.Response = &pipeline.Response{
			Role:      result.Response.Role,
			Content:   result.Response.Content,
			Parts:     result.Response.Parts,
			ToolCalls: result.Response.ToolCalls,
		}
	}

	// Propagate pending tools from stage metadata
	if pt, ok := result.Metadata["pending_tools"]; ok {
		if pending, ok := pt.([]tools.PendingToolExecution); ok {
			pipelineResult.PendingTools = pending
		}
	}

	return pipelineResult
}

const streamChunkBufferSize = 100

// convertStreamOutput converts stage StreamElement channel to StreamChunk channel.
// It accepts a context for cancellation propagation — if the context is canceled,
// the goroutine exits promptly instead of blocking on chunkChan sends.
func convertStreamOutput(ctx context.Context, stageChan <-chan stage.StreamElement) <-chan providers.StreamChunk {
	chunkChan := make(chan providers.StreamChunk, streamChunkBufferSize)

	go func() {
		defer close(chunkChan)
		processStreamElements(ctx, stageChan, chunkChan)
	}()

	return chunkChan
}

// streamProcessor holds state for processing stream elements into chunks.
type streamProcessor struct {
	ctx                 context.Context
	chunkChan           chan<- providers.StreamChunk
	sb                  strings.Builder
	finalResult         *pipeline.ExecutionResult
	pendingToolsEmitted bool
}

// sendChunk sends a chunk to the output channel, respecting context cancellation.
// Returns false if the context was canceled.
func (p *streamProcessor) sendChunk(chunk *providers.StreamChunk) bool {
	select {
	case p.chunkChan <- *chunk:
		return true
	case <-p.ctx.Done():
		return false
	}
}

// processElement processes a single stream element. Returns false if processing should stop.
func (p *streamProcessor) processElement(elem *stage.StreamElement) bool {
	if elem.Error != nil {
		return p.sendChunk(&providers.StreamChunk{
			Error:        elem.Error,
			FinishReason: strPtr("error"),
		})
	}

	if elem.Text != nil && *elem.Text != "" {
		p.sb.WriteString(*elem.Text)
		if !p.sendChunk(&providers.StreamChunk{Delta: *elem.Text}) {
			return false
		}
	}

	if elem.Metadata != nil {
		p.collectMetadata(elem.Metadata)
	}
	return true
}

// collectMetadata extracts final results and pending tools from element metadata.
func (p *streamProcessor) collectMetadata(metadata map[string]interface{}) {
	if stageResult, ok := metadata["__final_result__"].(*stage.ExecutionResult); ok {
		p.finalResult = convertExecutionResult(stageResult)
	}

	pt, ok := metadata["pending_tools"]
	if !ok {
		return
	}
	pending, ok := pt.([]tools.PendingToolExecution)
	if !ok || len(pending) == 0 {
		return
	}
	p.sendChunk(&providers.StreamChunk{
		FinishReason: strPtr("pending_tools"),
		PendingTools: pending,
		FinalResult:  p.finalResult,
	})
	p.pendingToolsEmitted = true
}

// processStreamElements processes stream elements and sends chunks.
// It respects context cancellation to avoid goroutine leaks when the consumer abandons the channel.
func processStreamElements(
	ctx context.Context, stageChan <-chan stage.StreamElement, chunkChan chan<- providers.StreamChunk,
) {
	p := &streamProcessor{ctx: ctx, chunkChan: chunkChan}

	for elem := range stageChan {
		if !p.processElement(&elem) {
			return
		}
	}

	// Send final chunk only if we didn't already emit a pending_tools chunk.
	// Set Content to the fully accumulated text here (once) instead of on every
	// intermediate chunk, avoiding O(N^2) string allocation.
	if !p.pendingToolsEmitted {
		p.sendChunk(&providers.StreamChunk{
			Content:      p.sb.String(),
			FinishReason: strPtr("stop"),
			FinalResult:  p.finalResult,
		})
	}
}

func strPtr(s string) *string {
	return &s
}
