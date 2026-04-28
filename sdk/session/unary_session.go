package session

import (
	"context"
	"errors"
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

	// Seed initial metadata if provided. Otherwise the conversation is
	// created lazily on the first typed write (AppendMessages, MergeMetadata,
	// SaveSummary). No bulk Save needed.
	if len(cfg.Metadata) > 0 {
		if accessor, ok := cfg.StateStore.(statestore.MetadataAccessor); ok {
			if err := accessor.MergeMetadata(context.Background(), cfg.ConversationID, cfg.Metadata); err != nil {
				return nil, fmt.Errorf("failed to seed conversation metadata: %w", err)
			}
		} else {
			return nil, fmt.Errorf("session: store does not implement MetadataAccessor; cannot seed initial metadata")
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
		Message: &message,
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
		Message: &message,
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
			Message: &toolResults[i],
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
		Message: &message,
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
		Message: &message,
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
			Message: &toolResults[i],
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

// Messages implements BaseSession. Conversations are created lazily on the
// first typed write, so a brand-new session may not yet exist in the store.
// Treat ErrNotFound as an empty conversation rather than propagating the error.
func (s *unarySession) Messages(ctx context.Context) ([]types.Message, error) {
	state, err := s.store.Load(ctx, s.id)
	if err != nil {
		if errors.Is(err, statestore.ErrNotFound) {
			return []types.Message{}, nil
		}
		return nil, err
	}
	return state.Messages, nil
}

// Clear implements BaseSession. Bulk operation — requires the store to
// implement BulkWriter. Stores without bulk-write support cannot honor
// Clear and return an error.
func (s *unarySession) Clear(ctx context.Context) error {
	bulkWriter, ok := s.store.(statestore.BulkWriter)
	if !ok {
		return fmt.Errorf("session clear: store does not implement BulkWriter")
	}
	return bulkWriter.Save(ctx, &statestore.ConversationState{
		ID:       s.id,
		Messages: nil,
	})
}

// ForkSession implements UnarySession.
func (s *unarySession) ForkSession(
	ctx context.Context,
	forkID string,
	pipelineArg *stage.StreamPipeline,
) (UnarySession, error) {
	// Fork the state in the store. If the source conversation has not yet been
	// materialized (e.g. a session that has only run pipelines without typed
	// writes), create an empty fork target via BulkWriter when available.
	if err := s.store.Fork(ctx, s.id, forkID); err != nil {
		if !errors.Is(err, statestore.ErrNotFound) {
			return nil, fmt.Errorf("failed to fork state: %w", err)
		}
		bulkWriter, ok := s.store.(statestore.BulkWriter)
		if !ok {
			return nil, fmt.Errorf("failed to fork state: %w", err)
		}
		if saveErr := bulkWriter.Save(ctx, &statestore.ConversationState{ID: forkID}); saveErr != nil {
			return nil, fmt.Errorf("failed to fork state: %w", saveErr)
		}
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
		Messages:     result.Messages,
		CostInfo:     result.CostInfo,
		Metadata:     result.Metadata,
		PendingTools: result.PendingTools,
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

	if len(elem.Meta.PendingTools) > 0 {
		p.emitPendingTools(elem.Meta.PendingTools)
	}
	return true
}

// emitPendingTools surfaces pending tool calls as a final stream chunk.
func (p *streamProcessor) emitPendingTools(pending []tools.PendingToolExecution) {
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
