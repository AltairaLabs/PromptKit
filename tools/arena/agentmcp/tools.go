package agentmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/tools/arena/agentkb"
	"github.com/AltairaLabs/PromptKit/tools/arena/templates"
)

// registerTools wires the agentkb-backed MCP tools. Additional tools are added
// in later tasks; explain is the proof-of-dispatch tool.
func (s *Server) registerTools() {
	s.addTool(mcp.Tool{
		Name:        "explain",
		Description: "Explain a PromptArena authoring concept. Omit id to list all concepts.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": { "type": "string", "description": "Concept id (e.g. mock-providers). Omit to list all." }
  }
}`),
	}, s.toolExplain)

	s.addTool(mcp.Tool{
		Name:        "get_schema",
		Description: "Print the embedded JSON schema for a config type. Omit type to list types.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "type": { "type": "string", "description": "Config type (scenario, provider, ...). Omit to list." }
  }
}`),
	}, s.toolGetSchema)

	s.addTool(mcp.Tool{
		Name:        "list_examples",
		Description: "List example kits from the embedded catalog, as JSON. Optional tag filter.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "tag": { "type": "string", "description": "Filter examples by tag." }
  }
}`),
	}, s.toolListExamples)

	s.addTool(mcp.Tool{
		Name:        "show_example",
		Description: "Print an example kit's files by name (see list_examples).",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "name": { "type": "string", "description": "Catalog entry name (e.g. quick-start)." }
  },
  "required": ["name"]
}`),
	}, s.toolShowExample)
}

func (s *Server) toolGetSchema(_ context.Context, args json.RawMessage) (mcp.ToolCallResponse, error) {
	var p struct {
		Type string `json:"type"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &p); err != nil {
			return mcp.ToolCallResponse{}, fmt.Errorf("invalid arguments: %w", err)
		}
	}

	if p.Type == "" {
		names, err := agentkb.SchemaNames()
		if err != nil {
			return mcp.ToolCallResponse{}, err
		}
		return textResult(strings.Join(names, "\n")), nil
	}

	b, err := agentkb.Schema(p.Type)
	if err != nil {
		return mcp.ToolCallResponse{}, err
	}
	return textResult(string(b)), nil
}

func (s *Server) toolListExamples(_ context.Context, args json.RawMessage) (mcp.ToolCallResponse, error) {
	var p struct {
		Tag string `json:"tag"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &p); err != nil {
			return mcp.ToolCallResponse{}, fmt.Errorf("invalid arguments: %w", err)
		}
	}

	cat, err := agentkb.LoadCatalog()
	if err != nil {
		return mcp.ToolCallResponse{}, err
	}

	entries := cat.Entries
	if p.Tag != "" {
		filtered := entries[:0:0]
		for _, e := range entries {
			if slices.Contains(e.Tags, p.Tag) {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	raw, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return mcp.ToolCallResponse{}, err
	}
	return textResult(string(raw)), nil
}

func (s *Server) toolShowExample(_ context.Context, args json.RawMessage) (mcp.ToolCallResponse, error) {
	var p struct {
		Name string `json:"name"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &p); err != nil {
			return mcp.ToolCallResponse{}, fmt.Errorf("invalid arguments: %w", err)
		}
	}
	if p.Name == "" {
		return mcp.ToolCallResponse{}, fmt.Errorf("name is required")
	}

	text, err := agentkb.RenderExample(templates.NewLoader(""), p.Name)
	if err != nil {
		return mcp.ToolCallResponse{}, err
	}
	return textResult(text), nil
}

func (s *Server) toolExplain(_ context.Context, args json.RawMessage) (mcp.ToolCallResponse, error) {
	var p struct {
		ID string `json:"id"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &p); err != nil {
			return mcp.ToolCallResponse{}, fmt.Errorf("invalid arguments: %w", err)
		}
	}

	if p.ID == "" {
		concepts, err := agentkb.Concepts()
		if err != nil {
			return mcp.ToolCallResponse{}, err
		}
		var b strings.Builder
		for _, c := range concepts {
			fmt.Fprintf(&b, "%s — %s\n", c.ID, c.Summary)
		}
		return textResult(b.String()), nil
	}

	c, err := agentkb.ConceptByID(p.ID)
	if err != nil {
		return mcp.ToolCallResponse{}, err
	}
	return textResult("# " + c.Title + "\n\n" + c.Body), nil
}
