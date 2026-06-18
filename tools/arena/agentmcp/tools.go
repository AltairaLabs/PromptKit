package agentmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/tools/arena/agentkb"
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
