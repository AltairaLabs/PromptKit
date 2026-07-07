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

// Messages implements BaseSession.
func (s *unarySession) Messages(ctx context.Context) ([]types.Message, error) {
	return loadMessages(ctx, s.store, s.id)
}

// Clear implements BaseSession.
func (s *unarySession) Clear(ctx context.Context) error {
	return clearSession(ctx, s.store, s.id)
}

// ForkSession implements UnarySession.
func (s *unarySession) ForkSession(
	ctx context.Context,
	forkID string,
	pipelineArg *stage.StreamPipeline,
) (UnarySession, error) {
	if err := forkOrCreate(ctx, s.store, s.id, forkID); err != nil {
		return nil, err
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

	// Fold emitted messages into finalResult so the terminal chunk carries real
	// cost/token/validation data. Without this the streaming Response reports
	// zero cost/tokens even though tokens were spent (the non-streaming Send()
	// path is unaffected). Mirrors stage.accumulateResult.
	if elem.Message != nil {
		p.accumulateMessage(elem.Message)
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

// streamRoleAssistant is the role of assistant messages whose cost/token usage
// is accumulated into the streaming final result.
const streamRoleAssistant = "assistant"

// accumulateMessage folds an emitted message into finalResult, summing cost/token
// usage from assistant messages and recording the response so the terminal
// stream chunk (and thus the streaming Response) reports real values instead of
// zeros. Mirrors stage.accumulateResult for the streaming path.
func (p *streamProcessor) accumulateMessage(msg *types.Message) {
	if p.finalResult == nil {
		p.finalResult = &pipeline.ExecutionResult{}
	}
	p.finalResult.Messages = append(p.finalResult.Messages, *msg)
	if msg.Role != streamRoleAssistant {
		return
	}
	p.finalResult.Response = &pipeline.Response{
		Role:      msg.Role,
		Content:   msg.Content,
		Parts:     msg.Parts,
		ToolCalls: msg.ToolCalls,
	}
	if msg.CostInfo != nil {
		ci := &p.finalResult.CostInfo
		ci.InputTokens += msg.CostInfo.InputTokens
		ci.OutputTokens += msg.CostInfo.OutputTokens
		ci.CachedTokens += msg.CostInfo.CachedTokens
		ci.InputCostUSD += msg.CostInfo.InputCostUSD
		ci.OutputCostUSD += msg.CostInfo.OutputCostUSD
		ci.CachedCostUSD += msg.CostInfo.CachedCostUSD
		ci.TotalCost += msg.CostInfo.TotalCost
	}
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
