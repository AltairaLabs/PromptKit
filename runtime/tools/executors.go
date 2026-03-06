package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"text/template"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// templateCache caches compiled Go text/templates keyed by the raw template
// string. This avoids re-parsing the same template on every mock call.
var templateCache sync.Map // map[string]*template.Template

const (
	modeMock             = "mock"
	modeLive             = "live"
	modeMCP              = "mcp"
	modeClient           = "client"
	executorMockStatic   = "mock-static"
	executorMockScripted = "mock-scripted"
)

// MockStaticExecutor executes tools using static mock data
type MockStaticExecutor struct{}

// NewMockStaticExecutor creates a new static mock executor
func NewMockStaticExecutor() *MockStaticExecutor {
	return &MockStaticExecutor{}
}

// Name returns the executor name
func (e *MockStaticExecutor) Name() string {
	return executorMockStatic
}

// Execute executes a tool using static mock data
func (e *MockStaticExecutor) Execute(
	_ context.Context, descriptor *ToolDescriptor, _ json.RawMessage,
) (json.RawMessage, error) {
	if descriptor.Mode != modeMock && descriptor.Mode != modeClient {
		return nil, ErrMockExecutorOnly
	}

	// Use the mock result from the descriptor
	if len(descriptor.MockResult) > 0 {
		return descriptor.MockResult, nil
	}

	// Load mock result from file if specified
	if descriptor.MockResultFile != "" {
		data, err := os.ReadFile(filepath.Clean(descriptor.MockResultFile))
		if err != nil {
			return nil, fmt.Errorf("failed to read mock_result_file for tool %s: %w", descriptor.Name, err)
		}
		return json.RawMessage(data), nil
	}

	// Generate a basic mock response if no mock_result is specified
	return json.RawMessage(`{"result": "mock response"}`), nil
}

// ExecuteMultimodal executes a tool and returns both JSON result and content parts.
// When MockParts are configured on the descriptor, file_path references in media
// content are resolved to base64 data, and URL references are passed through as-is.
func (e *MockStaticExecutor) ExecuteMultimodal(
	ctx context.Context, descriptor *ToolDescriptor, args json.RawMessage,
) (json.RawMessage, []types.ContentPart, error) {
	result, err := e.Execute(ctx, descriptor, args)
	if err != nil {
		return nil, nil, err
	}

	if len(descriptor.MockParts) == 0 {
		return result, nil, nil
	}

	resolvedParts, err := ResolveMockParts(descriptor.MockParts)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve mock_parts for tool %s: %w", descriptor.Name, err)
	}

	return result, resolvedParts, nil
}

// ResolveMockParts processes a slice of ContentPart, resolving any file_path
// references in MediaContent to base64-encoded data. URL references and
// already-encoded data are passed through unchanged.
func ResolveMockParts(parts []types.ContentPart) ([]types.ContentPart, error) {
	if len(parts) == 0 {
		return nil, nil
	}
	resolved := make([]types.ContentPart, len(parts))
	for i, part := range parts {
		if part.Media != nil && part.Media.FilePath != nil && *part.Media.FilePath != "" {
			resolvedPart, err := resolveFilePathPart(part)
			if err != nil {
				return nil, err
			}
			resolved[i] = resolvedPart
		} else {
			resolved[i] = part
		}
	}
	return resolved, nil
}

// resolveFilePathPart reads a file from disk and converts it to a base64 data part.
func resolveFilePathPart(part types.ContentPart) (types.ContentPart, error) {
	data, err := os.ReadFile(filepath.Clean(*part.Media.FilePath))
	if err != nil {
		return types.ContentPart{}, fmt.Errorf("failed to read file %s: %w", *part.Media.FilePath, err)
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	mediaCopy := *part.Media
	mediaCopy.Data = &encoded
	mediaCopy.FilePath = nil

	return types.ContentPart{
		Type:  part.Type,
		Text:  part.Text,
		Media: &mediaCopy,
	}, nil
}

// MockScriptedExecutor executes tools using templated mock data
type MockScriptedExecutor struct{}

// NewMockScriptedExecutor creates a new scripted mock executor
func NewMockScriptedExecutor() *MockScriptedExecutor {
	return &MockScriptedExecutor{}
}

// Name returns the executor name
func (e *MockScriptedExecutor) Name() string {
	return executorMockScripted
}

// Execute executes a tool using templated mock data
func (e *MockScriptedExecutor) Execute(
	_ context.Context, descriptor *ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	if descriptor.Mode != modeMock && descriptor.Mode != modeClient {
		return nil, ErrMockExecutorOnly
	}

	// Choose template source
	tmpl := descriptor.MockTemplate
	if tmpl == "" && descriptor.MockTemplateFile != "" {
		data, err := os.ReadFile(filepath.Clean(descriptor.MockTemplateFile))
		if err != nil {
			return nil, fmt.Errorf("failed to read mock_template_file for tool %s: %w", descriptor.Name, err)
		}
		tmpl = string(data)
	}

	if tmpl == "" {
		return nil, fmt.Errorf("no mock template specified for tool %s", descriptor.Name)
	}

	// Parse arguments for template processing
	var argsMap map[string]any
	if err := json.Unmarshal(args, &argsMap); err != nil {
		return nil, fmt.Errorf("failed to parse arguments for templating: %w", err)
	}

	// Render template using text/template for basic conditional logic
	result, err := e.processTemplate(tmpl, argsMap)
	if err != nil {
		return nil, fmt.Errorf("failed to render mock template for tool %s: %w", descriptor.Name, err)
	}

	// Try to parse as JSON to validate
	var jsonResult any
	if err := json.Unmarshal([]byte(result), &jsonResult); err != nil {
		// If not valid JSON, wrap it as a string value
		jsonResult = map[string]any{
			"result": result,
		}
	}

	return json.Marshal(jsonResult)
}

// processTemplate renders a Go text/template with provided arguments.
// Compiled templates are cached by template string via templateCache (sync.Map)
// so that repeated calls with the same template skip the parse step.
func (e *MockScriptedExecutor) processTemplate(tmpl string, args map[string]any) (string, error) {
	t, err := getOrParseTemplate(tmpl)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := t.Execute(&out, args); err != nil {
		return "", err
	}
	return out.String(), nil
}

// getOrParseTemplate returns a cached compiled template or parses and caches it.
func getOrParseTemplate(tmpl string) (*template.Template, error) {
	if cached, ok := templateCache.Load(tmpl); ok {
		return cached.(*template.Template), nil
	}

	t, err := template.New("mock").Option("missingkey=zero").Parse(tmpl)
	if err != nil {
		return nil, err
	}

	// Store and return (race-safe: if another goroutine stored first, we use theirs).
	actual, _ := templateCache.LoadOrStore(tmpl, t)
	return actual.(*template.Template), nil
}
