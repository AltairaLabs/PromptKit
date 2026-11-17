package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// RepositoryToolExecutor wraps existing tool executors to provide
// repository-backed mock responses with fallback to real execution. A base
// executor that implements CallExecutor is given the whole call, caller
// included; any other base runs Execute.
// This enables deterministic tool testing while maintaining the ability
// to fall back to real tool execution when needed.
type RepositoryToolExecutor struct {
	baseExecutor Executor
	repository   ToolResponseRepository
	contextKey   string // Context identifier (scenario ID, test name, etc.)
}

// NewRepositoryToolExecutor creates a new repository-backed tool executor.
// The executor will first check the repository for configured responses,
// and fall back to the base executor if no match is found.
func NewRepositoryToolExecutor(
	baseExecutor Executor, repo ToolResponseRepository, contextKey string,
) *RepositoryToolExecutor {
	return &RepositoryToolExecutor{
		baseExecutor: baseExecutor,
		repository:   repo,
		contextKey:   contextKey,
	}
}

// Name returns the executor name with repository suffix.
func (e *RepositoryToolExecutor) Name() string {
	return e.baseExecutor.Name() + "-with-repository"
}

// Execute executes a tool, first checking the repository for mock responses.
// If a matching response is found in the repository, it returns that response.
// Otherwise, it falls back to the base executor for real execution.
func (e *RepositoryToolExecutor) Execute(
	ctx context.Context, descriptor *ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	if result, ok, err := e.repositoryResult(descriptor, args); ok {
		return result, err
	}
	return e.baseExecutor.Execute(ctx, descriptor, args)
}

// ExecuteCall implements CallExecutor: a repository response for the call's
// arguments, else the base executor's run of the call. A base that implements
// CallExecutor is given the whole call, its caller included; any other base
// runs Execute, exactly as through Execute. The wrapper never suspends a call
// and never returns content parts, whatever the base implements.
func (e *RepositoryToolExecutor) ExecuteCall(
	ctx context.Context, descriptor *ToolDescriptor, call ToolCall,
) (json.RawMessage, []types.ContentPart, error) {
	if result, ok, err := e.repositoryResult(descriptor, call.Args); ok {
		return result, nil, err
	}
	if callExec, ok := e.baseExecutor.(CallExecutor); ok {
		return callExec.ExecuteCall(ctx, descriptor, call)
	}
	result, err := e.baseExecutor.Execute(ctx, descriptor, call.Args)
	return result, nil, err
}

// repositoryResult is the repository's answer to a call with args: its
// result, or the error it configures. ok is false when the repository has no
// answer and the base executor must run the call.
func (e *RepositoryToolExecutor) repositoryResult(
	descriptor *ToolDescriptor, args json.RawMessage,
) (result json.RawMessage, ok bool, err error) {
	logger.Debug("RepositoryToolExecutor Execute",
		"tool_name", descriptor.Name,
		"context_key", e.contextKey,
		"base_executor", e.baseExecutor.Name())

	// Parse arguments to a map for matching
	var argsMap map[string]any
	if err := json.Unmarshal(args, &argsMap); err != nil {
		logger.Debug("RepositoryToolExecutor failed to parse args, falling back to base executor",
			"tool_name", descriptor.Name,
			"error", err)
		return nil, false, nil
	}

	// Check if we have a repository response for this tool call
	repoResponse, _ := e.getRepositoryResponse(descriptor.Name, argsMap)
	if repoResponse == nil {
		logger.Debug("RepositoryToolExecutor no repository response found, using base executor",
			"tool_name", descriptor.Name,
			"context_key", e.contextKey)
		return nil, false, nil
	}
	logger.Debug("RepositoryToolExecutor using repository response",
		"tool_name", descriptor.Name,
		"context_key", e.contextKey,
		"has_error", repoResponse.Error != nil)

	// Handle error responses
	if repoResponse.Error != nil {
		return nil, true, fmt.Errorf("%s: %s", repoResponse.Error.Type, repoResponse.Error.Message)
	}

	result, marshalErr := json.Marshal(repoResponse.Result)
	if marshalErr != nil {
		logger.Debug("RepositoryToolExecutor failed to marshal repository response",
			"tool_name", descriptor.Name,
			"error", marshalErr)
		return nil, false, nil
	}
	return result, true, nil
}

// getRepositoryResponse attempts to get a mock response from the repository.
// It returns nil if no matching response is found (not an error condition).
func (e *RepositoryToolExecutor) getRepositoryResponse(
	toolName string, args map[string]any,
) (*ToolResponseData, error) {
	if e.repository == nil {
		return nil, nil
	}

	response, err := e.repository.GetToolResponse(toolName, args, e.contextKey)
	if err != nil {
		// Log but don't fail - repository errors should not break tool execution
		logger.Debug("RepositoryToolExecutor repository error",
			"tool_name", toolName,
			"context_key", e.contextKey,
			"error", err)
		return nil, err
	}

	return response, nil
}

// ToolResponseRepository defines the interface for repositories that can
// provide mock tool responses based on tool name, arguments, and context.
type ToolResponseRepository interface {
	// GetToolResponse retrieves a mock response for a tool execution.
	// Returns nil if no matching response is configured (not an error).
	GetToolResponse(toolName string, args map[string]any, contextKey string) (*ToolResponseData, error)
}

// ToolResponseData represents a configured tool response with optional error.
type ToolResponseData struct {
	Result any            `json:"result,omitempty"` // Successful response data
	Error  *ToolErrorData `json:"error,omitempty"`  // Error response
}

// ToolErrorData represents an error response for tool execution.
type ToolErrorData struct {
	Type    string `json:"type"`    // Error type/category
	Message string `json:"message"` // Error message
}

// FileToolResponseRepository implements ToolResponseRepository from a
// scenario's configured tool responses. It answers only for calls whose context
// key equals its scenario ID.
type FileToolResponseRepository struct {
	scenarioID    string
	toolResponses map[string][]MockToolResponseConfig // tool name -> response configs
}

// MockToolResponseConfig represents a single tool response configuration.
type MockToolResponseConfig struct {
	CallArgs map[string]any       `yaml:"call_args"`
	Result   any                  `yaml:"result,omitempty"`
	Error    *MockToolErrorConfig `yaml:"error,omitempty"`
}

// MockToolErrorConfig represents an error configuration.
type MockToolErrorConfig struct {
	Type    string `yaml:"type"`
	Message string `yaml:"message"`
}

// NewFileToolResponseRepository creates a repository from scenario tool responses.
// This is typically used by Arena to provide tool mocking from YAML scenarios.
func NewFileToolResponseRepository(
	scenarioID string, toolResponses map[string][]MockToolResponseConfig,
) *FileToolResponseRepository {
	return &FileToolResponseRepository{
		scenarioID:    scenarioID,
		toolResponses: toolResponses,
	}
}

// GetToolResponse implements ToolResponseRepository. It returns the first
// configured response for the tool whose CallArgs all appear, with deeply equal
// values, in args. Numbers compare by value at any depth, so int, float64 and
// json.Number forms of 42 are equal. An entry with no CallArgs matches any call. It returns nil
// when the context key is not the scenario ID or nothing matches.
func (r *FileToolResponseRepository) GetToolResponse(
	toolName string, args map[string]any, contextKey string,
) (*ToolResponseData, error) {
	logger.Debug("FileToolResponseRepository GetToolResponse",
		"tool_name", toolName,
		"scenario_id", r.scenarioID,
		"context_key", contextKey,
		"arg_count", len(args))

	// Check if context matches our scenario
	if contextKey != r.scenarioID {
		logger.Debug("FileToolResponseRepository context mismatch",
			"expected_scenario", r.scenarioID,
			"actual_context", contextKey)
		return nil, nil
	}

	// Find tool responses for this tool name
	responses, exists := r.toolResponses[toolName]
	if !exists {
		logger.Debug("FileToolResponseRepository no responses configured for tool",
			"tool_name", toolName)
		return nil, nil
	}

	// Find matching response based on arguments
	for i, response := range responses {
		if r.argumentsMatch(args, response.CallArgs) {
			logger.Debug("FileToolResponseRepository found matching response",
				"tool_name", toolName,
				"response_index", i,
				"has_error", response.Error != nil)

			result := &ToolResponseData{
				Result: response.Result,
			}

			if response.Error != nil {
				result.Error = &ToolErrorData{
					Type:    response.Error.Type,
					Message: response.Error.Message,
				}
			}

			return result, nil
		}
	}

	logger.Debug("FileToolResponseRepository no matching response found",
		"tool_name", toolName,
		"response_count", len(responses))
	return nil, nil
}

// argumentsMatch checks if the provided arguments match the expected arguments.
// It performs a deep comparison of argument values in which numbers compare by
// value, so a YAML call_args 42 (int) matches a JSON argument 42 (float64).
func (r *FileToolResponseRepository) argumentsMatch(provided, expected map[string]any) bool {
	if len(expected) == 0 {
		return true // Empty expected args match any provided args
	}

	// Check that all expected arguments match provided values
	for key, expectedValue := range expected {
		providedValue, exists := provided[key]
		if !exists {
			return false
		}

		if !argValuesEqual(providedValue, expectedValue) {
			return false
		}
	}

	return true
}

// InMemoryToolResponseRepository implements ToolResponseRepository using
// in-memory storage. This is useful for SDK unit tests and programmatic
// configuration of tool responses.
type InMemoryToolResponseRepository struct {
	responses map[string]map[string]*ToolResponseData // contextKey -> toolName -> response
}

// NewInMemoryToolResponseRepository creates a new in-memory tool response repository.
func NewInMemoryToolResponseRepository() *InMemoryToolResponseRepository {
	return &InMemoryToolResponseRepository{
		responses: make(map[string]map[string]*ToolResponseData),
	}
}

// AddResponse adds a tool response for a specific context and tool name.
// This method supports simple responses where argument matching is not needed.
func (r *InMemoryToolResponseRepository) AddResponse(contextKey, toolName string, response *ToolResponseData) {
	if r.responses[contextKey] == nil {
		r.responses[contextKey] = make(map[string]*ToolResponseData)
	}
	r.responses[contextKey][toolName] = response
}

// GetToolResponse implements ToolResponseRepository.
// For simplicity, this implementation only matches by tool name and context,
// not by arguments. For argument-based matching, use FileToolResponseRepository
// or implement a custom repository.
func (r *InMemoryToolResponseRepository) GetToolResponse(
	toolName string, args map[string]any, contextKey string,
) (*ToolResponseData, error) {
	contextResponses, exists := r.responses[contextKey]
	if !exists {
		return nil, nil
	}

	response, exists := contextResponses[toolName]
	if !exists {
		return nil, nil
	}

	return response, nil
}
