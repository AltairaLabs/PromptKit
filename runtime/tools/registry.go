package tools

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	errInvalidToolDescriptor  = "invalid tool descriptor in %s: %w"
	errResultValidationFailed = "result validation failed: %v"
)

// ToolRepository provides abstract access to tool descriptors (local interface to avoid import cycles)
type ToolRepository interface {
	LoadTool(name string) (*ToolDescriptor, error)
	ListTools() ([]string, error)
	SaveTool(descriptor *ToolDescriptor) error
}

// Registry manages tool descriptors and provides access to executors
type Registry struct {
	repository ToolRepository             // Optional repository for loading tools
	tools      map[string]*ToolDescriptor // Cache of loaded tool descriptors
	validator  *SchemaValidator           // Schema validator for tool arguments
	executors  map[string]Executor        // Registered tool executors
}

// NewRegistry creates a new tool registry without a repository backend (legacy mode)
func NewRegistry() *Registry {
	return newRegistry(nil)
}

// NewRegistryWithRepository creates a new tool registry with a repository backend
func NewRegistryWithRepository(repository ToolRepository) *Registry {
	return newRegistry(repository)
}

// newRegistry is the internal constructor for creating registries
func newRegistry(repository ToolRepository) *Registry {
	registry := &Registry{
		repository: repository,
		tools:      make(map[string]*ToolDescriptor),
		validator:  NewSchemaValidator(),
		executors:  make(map[string]Executor),
	}

	// Register default executors
	registry.RegisterExecutor(NewMockStaticExecutor())
	registry.RegisterExecutor(NewMockScriptedExecutor())

	// Preload all tools from repository into cache if repository is provided
	if repository != nil {
		if toolNames, _ := repository.ListTools(); toolNames != nil {
			for _, name := range toolNames {
				if tool, _ := repository.LoadTool(name); tool != nil {
					registry.tools[name] = tool
				}
			}
		}
	}

	return registry
}

// Register adds a tool descriptor to the registry with validation
func (r *Registry) Register(descriptor *ToolDescriptor) error {
	// Test validation setup (errors here indicate schema compilation issues, which are acceptable during registration)
	_ = r.validator.ValidateArgs(descriptor, []byte("{}"))

	// Persist to repository if available
	if r.repository != nil {
		if err := r.repository.SaveTool(descriptor); err != nil {
			return fmt.Errorf("failed to save tool to repository: %w", err)
		}
	}

	// Cache the descriptor
	r.tools[descriptor.Name] = descriptor
	return nil
}

// Get retrieves a tool descriptor by name with repository fallback
func (r *Registry) Get(name string) *ToolDescriptor {
	// Check cache first
	if tool, ok := r.tools[name]; ok {
		return tool
	}

	// Try loading from repository if available
	if r.repository != nil {
		if tool, _ := r.repository.LoadTool(name); tool != nil {
			r.tools[name] = tool // Cache the loaded tool
			return tool
		}
	}

	return nil // Not found
}

// List returns all tool names from repository or cache
func (r *Registry) List() []string {
	// Try repository first for complete list
	if r.repository != nil {
		if names, _ := r.repository.ListTools(); len(names) > 0 {
			return names
		}
	}

	// Fallback to cache
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// LoadToolFromBytes loads a tool descriptor from raw bytes data.
// This is useful when tool data has already been read from a file or
// received from another source, avoiding redundant file I/O.
// The filename parameter is used only for error reporting.
func (r *Registry) LoadToolFromBytes(filename string, data []byte) error {
	ext := strings.ToLower(filepath.Ext(filename))

	if ext == ".yaml" || ext == ".yml" {
		return r.loadYAMLTool(filename, data)
	}

	return r.loadJSONTool(filename, data)
}

// loadYAMLTool loads a tool from YAML data (K8s manifest format required)
func (r *Registry) loadYAMLTool(filename string, data []byte) error {
	var temp any
	if err := yaml.Unmarshal(data, &temp); err != nil {
		return fmt.Errorf("failed to parse YAML tool file %s: %w", filename, err)
	}

	tempMap, ok := temp.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid YAML structure in %s", filename)
	}

	// Require K8s manifest format
	if _, hasAPI := tempMap["apiVersion"].(string); !hasAPI {
		return fmt.Errorf("tool file %s must use K8s manifest format (apiVersion, kind, metadata, spec)", filename)
	}

	return r.loadK8sManifest(filename, temp)
}

// loadK8sManifest loads a K8s-style tool manifest
func (r *Registry) loadK8sManifest(filename string, temp any) error {
	jsonData, err := json.Marshal(temp)
	if err != nil {
		return fmt.Errorf("failed to convert K8s manifest to JSON for %s: %w", filename, err)
	}

	var toolConfig ToolConfig
	if err := json.Unmarshal(jsonData, &toolConfig); err != nil {
		return fmt.Errorf("failed to unmarshal K8s manifest %s: %w", filename, err)
	}

	if err := r.validateK8sManifest(filename, &toolConfig); err != nil {
		return err
	}

	// Use metadata.name as tool name (spec.name is not needed in K8s manifests)
	toolConfig.Spec.Name = toolConfig.Metadata.Name

	if err := r.validateDescriptor(&toolConfig.Spec); err != nil {
		return fmt.Errorf(errInvalidToolDescriptor, filename, err)
	}

	r.tools[toolConfig.Spec.Name] = &toolConfig.Spec
	return nil
}

// validateK8sManifest validates the structure of a K8s manifest
func (r *Registry) validateK8sManifest(filename string, toolConfig *ToolConfig) error {
	if toolConfig.Kind == "" {
		return fmt.Errorf("tool config %s is missing kind", filename)
	}
	if toolConfig.Kind != "Tool" {
		return fmt.Errorf("tool config %s has invalid kind: expected 'Tool', got '%s'", filename, toolConfig.Kind)
	}
	if toolConfig.Metadata.Name == "" {
		return fmt.Errorf("tool config %s is missing metadata.name", filename)
	}
	return nil
}

// loadJSONTool loads a tool from JSON data
func (r *Registry) loadJSONTool(filename string, data []byte) error {
	var descriptor ToolDescriptor
	if err := json.Unmarshal(data, &descriptor); err != nil {
		return fmt.Errorf("failed to parse JSON tool file %s: %w", filename, err)
	}

	if err := r.validateDescriptor(&descriptor); err != nil {
		return fmt.Errorf(errInvalidToolDescriptor, filename, err)
	}

	r.tools[descriptor.Name] = &descriptor
	return nil
}

// GetTool retrieves a tool descriptor by name
func (r *Registry) GetTool(name string) (*ToolDescriptor, error) {
	tool, exists := r.tools[name]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrToolNotFound, name)
	}
	return tool, nil
}

// GetTools returns all loaded tool descriptors
func (r *Registry) GetTools() map[string]*ToolDescriptor {
	// Return a copy to prevent external modification
	result := make(map[string]*ToolDescriptor)
	for name, tool := range r.tools {
		result[name] = tool
	}
	return result
}

// GetToolsByNames returns tool descriptors for the specified names
func (r *Registry) GetToolsByNames(names []string) ([]*ToolDescriptor, error) {
	var tools []*ToolDescriptor
	for _, name := range names {
		tool, err := r.GetTool(name)
		if err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

// RegisterExecutor registers a tool executor
func (r *Registry) RegisterExecutor(executor Executor) {
	r.executors[executor.Name()] = executor
}

// Execute executes a tool with the given arguments
func (r *Registry) Execute(toolName string, args json.RawMessage) (*ToolResult, error) {
	tool, err := r.GetTool(toolName)
	if err != nil {
		return nil, err
	}

	// Validate arguments
	if err := r.validator.ValidateArgs(tool, args); err != nil {
		return nil, err
	}

	// Find appropriate executor using shared logic
	executor, err := r.getExecutorForTool(tool)
	if err != nil {
		return nil, err
	}

	// Execute the tool
	start := getCurrentTimeMs()
	result, err := executor.Execute(tool, args)
	latency := getCurrentTimeMs() - start

	if err != nil {
		return &ToolResult{
			Name:      toolName,
			Error:     err.Error(),
			LatencyMs: latency,
		}, nil
	}

	// Skip validation for MCP tools - MCP is a standard protocol with trusted responses
	// MCP tool schemas often don't match actual response formats (strings vs objects)
	if tool.Mode == modeMCP {
		return &ToolResult{
			Name:      toolName,
			Result:    result,
			LatencyMs: latency,
		}, nil
	}

	// Validate and potentially coerce the result for non-MCP tools
	validatedResult, _, err := r.validator.CoerceResult(tool, result)
	if err != nil {
		return &ToolResult{
			Name:      toolName,
			Error:     fmt.Sprintf(errResultValidationFailed, err),
			LatencyMs: latency,
		}, nil
	}

	return &ToolResult{
		Name:      toolName,
		Result:    validatedResult,
		LatencyMs: latency,
	}, nil
}

// ExecuteAsync executes a tool with async support, checking if it implements AsyncToolExecutor.
// Returns ToolExecutionResult with status (complete/pending/failed).
func (r *Registry) ExecuteAsync(toolName string, args json.RawMessage) (*ToolExecutionResult, error) {
	tool, err := r.GetTool(toolName)
	if err != nil {
		return nil, err
	}

	// Validate arguments
	if err := r.validator.ValidateArgs(tool, args); err != nil {
		return nil, err
	}

	executor, err := r.getExecutorForTool(tool)
	if err != nil {
		return nil, err
	}

	// Try async execution if supported
	if asyncExecutor, ok := executor.(AsyncToolExecutor); ok {
		return r.executeWithAsyncExecutor(asyncExecutor, tool, toolName, args)
	}

	// Fall back to synchronous execution
	return r.executeSyncFallback(executor, tool, toolName, args)
}

// getExecutorForTool finds the appropriate executor for a tool.
// Priority order:
// 1. Built-in mode mapping (mock, live, mcp) for backwards compatibility
// 2. If tool.Mode matches a registered executor name, use it (enables custom executors)
func (r *Registry) getExecutorForTool(tool *ToolDescriptor) (Executor, error) {
	var executorName string

	// First, handle built-in modes with their established mappings
	switch tool.Mode {
	case modeMock, "": // Empty mode defaults to mock behavior
		// Check for templated mock first
		if tool.MockTemplate != "" || tool.MockTemplateFile != "" {
			executorName = executorMockScripted
		} else {
			executorName = executorMockStatic
		}
	case modeLive:
		executorName = "http"
	case modeMCP:
		executorName = modeMCP
	default:
		// For non-built-in modes, use Mode as the executor name directly
		// This enables custom executors by setting Mode to the executor's name
		executorName = tool.Mode
	}

	executor, exists := r.executors[executorName]
	if !exists {
		return nil, fmt.Errorf("executor %s not available for tool %s", executorName, tool.Name)
	}
	return executor, nil
}

// executeWithAsyncExecutor executes a tool with async support
func (r *Registry) executeWithAsyncExecutor(asyncExecutor AsyncToolExecutor, tool *ToolDescriptor, toolName string, args json.RawMessage) (*ToolExecutionResult, error) {
	start := getCurrentTimeMs()
	result, err := asyncExecutor.ExecuteAsync(tool, args)
	_ = getCurrentTimeMs() - start // Track latency but unused for now

	if err != nil {
		return &ToolExecutionResult{
			Status: ToolStatusFailed,
			Error:  err.Error(),
		}, nil
	}

	// Return immediately for pending or failed status
	if result.Status == ToolStatusPending || result.Status == ToolStatusFailed {
		return result, nil
	}

	// Skip validation for MCP tools
	if tool.Mode == modeMCP {
		return result, nil
	}

	// result.Content is already json.RawMessage
	return r.validateAndCoerceResult(tool, result.Content)
}

// executeSyncFallback executes a tool synchronously for non-async executors
func (r *Registry) executeSyncFallback(executor Executor, tool *ToolDescriptor, toolName string, args json.RawMessage) (*ToolExecutionResult, error) {
	start := getCurrentTimeMs()
	result, err := executor.Execute(tool, args)
	_ = getCurrentTimeMs() - start // Track latency but unused for now

	if err != nil {
		return &ToolExecutionResult{
			Status: ToolStatusFailed,
			Error:  err.Error(),
		}, nil
	}

	// Skip validation for MCP tools
	if tool.Mode == modeMCP {
		return &ToolExecutionResult{
			Status:  ToolStatusComplete,
			Content: result,
		}, nil
	}

	return r.validateAndCoerceResult(tool, result)
}

// validateAndCoerceResult validates and coerces tool execution results
func (r *Registry) validateAndCoerceResult(tool *ToolDescriptor, content json.RawMessage) (*ToolExecutionResult, error) {
	validatedResult, _, err := r.validator.CoerceResult(tool, content)
	if err != nil {
		return &ToolExecutionResult{
			Status: ToolStatusFailed,
			Error:  fmt.Sprintf(errResultValidationFailed, err),
		}, nil
	}

	return &ToolExecutionResult{
		Status:  ToolStatusComplete,
		Content: validatedResult,
	}, nil
}

// validateDescriptor validates a tool descriptor
func (r *Registry) validateDescriptor(descriptor *ToolDescriptor) error {
	if descriptor.Name == "" {
		return ErrToolNameRequired
	}

	if descriptor.Description == "" {
		return ErrToolDescriptionRequired
	}

	if len(descriptor.InputSchema) == 0 {
		return ErrInputSchemaRequired
	}

	if len(descriptor.OutputSchema) == 0 {
		return ErrOutputSchemaRequired
	}

	// Mode must be empty (defaults to mock), a built-in mode, or a registered executor name
	isBuiltinMode := descriptor.Mode == "" || descriptor.Mode == modeMock ||
		descriptor.Mode == modeLive || descriptor.Mode == modeMCP
	_, isRegisteredExecutor := r.executors[descriptor.Mode]
	if !isBuiltinMode && !isRegisteredExecutor {
		return ErrInvalidToolMode
	}

	if descriptor.TimeoutMs <= 0 {
		descriptor.TimeoutMs = 3000 // default timeout
	}

	// Validate schemas by attempting to compile them
	if _, err := r.validator.getSchema(string(descriptor.InputSchema)); err != nil {
		return fmt.Errorf("invalid input schema: %w", err)
	}

	if _, err := r.validator.getSchema(string(descriptor.OutputSchema)); err != nil {
		return fmt.Errorf("invalid output schema: %w", err)
	}

	return nil
}

// getCurrentTimeMs returns current time in milliseconds
func getCurrentTimeMs() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}
