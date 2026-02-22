package session

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
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
	return convertStreamOutput(outputChan), nil
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
	return convertStreamOutput(outputChan), nil
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

	return pipelineResult
}

const streamChunkBufferSize = 100

// convertStreamOutput converts stage StreamElement channel to StreamChunk channel
func convertStreamOutput(stageChan <-chan stage.StreamElement) <-chan providers.StreamChunk {
	chunkChan := make(chan providers.StreamChunk, streamChunkBufferSize)

	go func() {
		defer close(chunkChan)
		processStreamElements(stageChan, chunkChan)
	}()

	return chunkChan
}

// processStreamElements processes stream elements and sends chunks
func processStreamElements(stageChan <-chan stage.StreamElement, chunkChan chan<- providers.StreamChunk) {
	var accumulatedContent string
	var finalResult *pipeline.ExecutionResult

	for elem := range stageChan {
		// Handle errors
		if elem.Error != nil {
			chunkChan <- providers.StreamChunk{
				Error:        elem.Error,
				FinishReason: strPtr("error"),
			}
			continue
		}

		// Emit text chunks
		if elem.Text != nil && *elem.Text != "" {
			accumulatedContent += *elem.Text
			chunkChan <- providers.StreamChunk{
				Delta:   *elem.Text,
				Content: accumulatedContent,
			}
		}

		// Collect final result from metadata
		if elem.Metadata != nil {
			if stageResult, ok := elem.Metadata["__final_result__"].(*stage.ExecutionResult); ok {
				finalResult = convertExecutionResult(stageResult)
			}
		}
	}

	// Send final chunk
	finishReason := "stop"
	chunkChan <- providers.StreamChunk{
		FinishReason: &finishReason,
		FinalResult:  finalResult,
	}
}

func strPtr(s string) *string {
	return &s
}
