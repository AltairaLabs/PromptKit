package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Keys of the JSON objects the model reads for non-text content.
const (
	mcpKeyType     = "type"
	mcpKeyMimeType = "mimeType"
	mcpKeyURI      = "uri"
)

const (
	mcpToolSuccess     = "MCP tool success"
	mcpToolReturnedErr = "MCP tool returned error"
	mcpEmptySuccess    = "Operation completed successfully"
)

// MCPExecutor executes tools using MCP (Model Context Protocol) servers.
// The configured registry is used as a default; callers can attach a
// per-call registry override via WithMCPRegistry to give each concurrent
// run its own MCP routing without sharing tool-to-server mappings.
//
// Timeouts come from the caller's context (the tool registry applies the
// descriptor's TimeoutMs) and from the server's own request timeout
// (ServerConfig.TimeoutMs); the executor adds none of its own.
type MCPExecutor struct {
	registry  mcp.Registry
	validator *SchemaValidator
}

// NewMCPExecutor creates a new MCP executor
func NewMCPExecutor(registry mcp.Registry) *MCPExecutor {
	return &MCPExecutor{
		registry:  registry,
		validator: NewSchemaValidator(),
	}
}

type mcpRegistryCtxKeyType struct{}

var mcpRegistryCtxKey mcpRegistryCtxKeyType

// WithMCPRegistry attaches an MCP registry to the context. The
// MCPExecutor reads this on each Execute and routes via that registry,
// falling back to the executor's configured registry when absent.
//
// Use this to give each concurrent run its own per-run MCP registry
// (typically a Fork of the engine's parent registry) so that
// session-scoped servers registered under the same name (e.g. "sandbox")
// resolve to different URLs without collision.
func WithMCPRegistry(ctx context.Context, reg mcp.Registry) context.Context {
	if reg == nil {
		return ctx
	}
	return context.WithValue(ctx, mcpRegistryCtxKey, reg)
}

// mcpRegistryFromCtx returns a per-call MCP registry override if present.
// Returns nil when the context carries no override.
func mcpRegistryFromCtx(ctx context.Context) mcp.Registry {
	if ctx == nil {
		return nil
	}
	if reg, ok := ctx.Value(mcpRegistryCtxKey).(mcp.Registry); ok {
		return reg
	}
	return nil
}

// Name returns the executor name
func (e *MCPExecutor) Name() string {
	return modeMCP
}

// Execute executes a tool using an MCP server. Images and audio in the
// result are included in the JSON as content blocks; ExecuteMultimodal
// returns them as content parts instead.
func (e *MCPExecutor) Execute(
	ctx context.Context, descriptor *ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	result, _, err := e.execute(ctx, descriptor, args, true)
	return result, err
}

// ExecuteMultimodal executes a tool using an MCP server, returning images
// and audio from the result as content parts the model can see.
func (e *MCPExecutor) ExecuteMultimodal(
	ctx context.Context, descriptor *ToolDescriptor, args json.RawMessage,
) (json.RawMessage, []types.ContentPart, error) {
	return e.execute(ctx, descriptor, args, false)
}

func (e *MCPExecutor) execute(
	ctx context.Context, descriptor *ToolDescriptor, args json.RawMessage, inlineMedia bool,
) (json.RawMessage, []types.ContentPart, error) {
	if descriptor.Mode != modeMCP {
		return nil, nil, ErrMCPExecutorOnly
	}

	logger.Info("MCP tool call", "tool", descriptor.Name)
	logger.Debug("MCP tool call args", "tool", descriptor.Name, "args", string(args))

	response, err := e.callMCPTool(ctx, descriptor.Name, args)
	if err != nil {
		return nil, nil, err
	}

	if response.IsError {
		return nil, nil, e.handleErrorResponse(descriptor.Name, response)
	}
	if err := e.validateStructured(descriptor, response); err != nil {
		return nil, nil, err
	}
	return formatMCPResult(descriptor.Name, response, inlineMedia)
}

// validateStructured checks a structured result against the tool's declared
// output schema (MCP server/tools: clients SHOULD validate). A server MUST
// conform; a result that does not is reported as a tool error rather than
// passed to the model as if it did.
func (e *MCPExecutor) validateStructured(descriptor *ToolDescriptor, response *mcp.ToolCallResponse) error {
	if len(descriptor.OutputSchema) == 0 || !response.HasStructuredContent() {
		return nil
	}
	err := e.validator.ValidateResult(descriptor, response.StructuredContent)
	var mismatch *ValidationError
	switch {
	case err == nil:
		return nil
	case !errors.As(err, &mismatch):
		// The schema is one this validator cannot compile (it may be valid
		// JSON Schema 2020-12 that draft-07 tooling rejects). That says
		// nothing about the result, so it is passed on unchecked rather than
		// failing every call to the tool.
		logger.Warn("MCP tool output schema could not be applied; result not validated",
			"tool", descriptor.Name, "error", err)
		return nil
	}
	logger.Warn("MCP tool result does not match its output schema", "tool", descriptor.Name, "error", err)
	return fmt.Errorf("MCP tool %s returned structured content that does not match its output schema: %w",
		descriptor.Name, err)
}

func (e *MCPExecutor) callMCPTool(
	ctx context.Context, toolName string, args json.RawMessage,
) (*mcp.ToolCallResponse, error) {
	// Use the raw MCP name (without namespace prefix) for server communication
	rawName := mcpRawToolName(toolName)

	// Per-run override (if the caller attached one via WithMCPRegistry)
	// takes precedence over the executor's configured registry. This is
	// what lets each concurrent run's MCP dispatch route to its own
	// session-scoped server even though a single MCPExecutor is shared.
	registry := mcpRegistryFromCtx(ctx)
	if registry == nil {
		registry = e.registry
	}

	client, err := registry.GetClientForTool(ctx, rawName)
	if err != nil {
		logger.Error("MCP tool failed to get client", "tool", toolName, "error", err)
		return nil, fmt.Errorf("failed to get MCP client for tool %s: %w", toolName, err)
	}

	response, err := client.CallTool(ctx, rawName, args)
	if err != nil {
		logger.Error("MCP tool call failed", "tool", toolName, "error", err)
		return nil, fmt.Errorf("MCP tool call failed: %w", err)
	}

	return response, nil
}

// mcpRawToolName strips the "mcp__server__" prefix from a qualified tool name,
// returning the original name the MCP server knows. If no "mcp__" prefix is
// present, the name is returned as-is for backward compatibility.
func mcpRawToolName(qualifiedName string) string {
	ns, rest, found := strings.Cut(qualifiedName, NamespaceSep)
	if !found || ns != "mcp" {
		return qualifiedName
	}
	// rest is "server__tool"; strip the server part
	_, rawName, found := strings.Cut(rest, NamespaceSep)
	if !found {
		return qualifiedName
	}
	return rawName
}

func (e *MCPExecutor) handleErrorResponse(toolName string, response *mcp.ToolCallResponse) error {
	errorMsg := e.extractErrorMessage(response.Content)
	if errorMsg == mcpToolReturnedErr && response.HasStructuredContent() {
		errorMsg = string(response.StructuredContent)
	}
	logger.Error("MCP tool returned error", "tool", toolName, "error", errorMsg)
	return fmt.Errorf("%s", errorMsg)
}

func (e *MCPExecutor) extractErrorMessage(content []mcp.Content) string {
	if len(content) == 0 {
		return mcpToolReturnedErr
	}

	var parts []string
	for _, item := range content {
		if item.Text != "" {
			parts = append(parts, item.Text)
		}
	}

	if len(parts) == 0 {
		return mcpToolReturnedErr
	}
	return strings.Join(parts, "; ")
}

// formatMCPResult turns a tool result into what the model reads.
//
// structuredContent, when present, is the result. Otherwise the content
// blocks are: text as text; an embedded text resource as its text with its
// URI; a resource link as a link object; images and audio as content parts
// (or, when inlineMedia, as their content blocks in the JSON). A lone string
// stays a JSON string and several strings a JSON array, as before.
func formatMCPResult(
	toolName string, response *mcp.ToolCallResponse, inlineMedia bool,
) (json.RawMessage, []types.ContentPart, error) {
	var items []any
	var parts []types.ContentPart
	for i := range response.Content {
		item, part := convertMCPContent(&response.Content[i], inlineMedia)
		if item != nil {
			items = append(items, item)
		}
		if part != nil {
			parts = append(parts, *part)
		}
	}

	if response.HasStructuredContent() {
		logger.Info(mcpToolSuccess, "tool", toolName, "result_type", "structuredContent", "media_parts", len(parts))
		return response.StructuredContent, parts, nil
	}
	logger.Info(mcpToolSuccess, "tool", toolName, "content_items", len(items), "media_parts", len(parts))
	switch {
	case len(items) == 0 && len(parts) == 0:
		result, err := json.Marshal(mcpEmptySuccess)
		return result, nil, err
	case len(items) == 0:
		result, err := json.Marshal(fmt.Sprintf("Returned %d media item(s).", len(parts)))
		return result, parts, err
	case len(items) == 1:
		if s, ok := items[0].(string); ok {
			result, err := json.Marshal(s)
			return result, parts, err
		}
	}
	result, err := json.Marshal(items)
	return result, parts, err
}

// convertMCPContent converts one content block: the item the model reads
// in the JSON result, and/or a media part.
func convertMCPContent(c *mcp.Content, inlineMedia bool) (any, *types.ContentPart) {
	switch c.Type {
	case mcp.ContentTypeText:
		if c.Text == "" {
			return nil, nil
		}
		return c.Text, nil
	case mcp.ContentTypeImage, mcp.ContentTypeAudio:
		if inlineMedia {
			return c, nil
		}
		return nil, mediaPart(c.Type, c.Data, c.MimeType)
	case mcp.ContentTypeResourceLink:
		return resourceLinkItem(c), nil
	case mcp.ContentTypeResource:
		if c.Resource == nil {
			return c, nil // malformed: passed through rather than dropped
		}
		return embeddedResourceItem(c.Resource, inlineMedia)
	}
	return c, nil // an unknown block type is passed through as-is
}

// mediaPart builds a content part for base64 media, or nil for a type the
// model cannot take as a part.
func mediaPart(kind, data, mimeType string) *types.ContentPart {
	var part types.ContentPart
	switch {
	case kind == mcp.ContentTypeImage || strings.HasPrefix(mimeType, "image/"):
		part = types.NewImagePartFromData(data, mimeType, nil)
	case kind == mcp.ContentTypeAudio || strings.HasPrefix(mimeType, "audio/"):
		part = types.NewAudioPartFromData(data, mimeType)
	default:
		return nil
	}
	return &part
}

func resourceLinkItem(c *mcp.Content) map[string]any {
	item := map[string]any{mcpKeyType: mcp.ContentTypeResourceLink, mcpKeyURI: c.URI}
	for k, v := range map[string]string{
		"name": c.Name, "title": c.Title, "description": c.Description, mcpKeyMimeType: c.MimeType,
	} {
		if v != "" {
			item[k] = v
		}
	}
	if c.Size != nil {
		item["size"] = *c.Size
	}
	return item
}

// embeddedResourceItem converts an embedded resource: its text, or for a
// binary resource a media part (images, audio) or a description of it.
func embeddedResourceItem(r *mcp.ResourceContents, inlineMedia bool) (any, *types.ContentPart) {
	if r.Blob == "" {
		return map[string]any{
			mcpKeyType: mcp.ContentTypeResource, mcpKeyURI: r.URI, mcpKeyMimeType: r.MimeType, "text": r.Text,
		}, nil
	}
	if !inlineMedia {
		if part := mediaPart("", r.Blob, r.MimeType); part != nil {
			return nil, part
		}
	}
	return map[string]any{
		mcpKeyType: mcp.ContentTypeResource, mcpKeyURI: r.URI, mcpKeyMimeType: r.MimeType,
		"note": fmt.Sprintf("binary content (%d base64 characters) not shown", len(r.Blob)),
	}, nil
}
