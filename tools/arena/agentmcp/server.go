// Package agentmcp is a stdio MCP server that exposes the PromptArena agent
// knowledge base (agentkb) — authoring concepts, example catalog, and JSON
// schemas — as MCP tools and resources, so any MCP client can author kits.
package agentmcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/AltairaLabs/PromptKit/runtime/mcp"
)

const (
	serverName      = "promptarena-agentkb"
	jsonRPCVersion  = "2.0"
	contentTypeText = "text"
)

// JSON-RPC 2.0 error codes.
const (
	parseError     = -32700
	invalidParams  = -32602
	methodNotFound = -32601
	internalError  = -32603
)

// scanBufferInitial / scanBufferMax bound a single JSON-RPC line
// (schemas can be large).
const (
	scanBufferInitial = 64 * 1024
	scanBufferMax     = 4 * 1024 * 1024
)

type toolHandler func(ctx context.Context, args json.RawMessage) (mcp.ToolCallResponse, error)

type toolEntry struct {
	def     mcp.Tool
	handler toolHandler
}

// Server is a stdio MCP server over the PromptArena agent knowledge base.
type Server struct {
	version string
	tools   map[string]toolEntry
	order   []string // stable tool ordering for tools/list
}

// NewServer builds a server reporting the given version in its serverInfo.
func NewServer(version string) *Server {
	s := &Server{version: version, tools: map[string]toolEntry{}}
	s.registerTools()
	return s
}

// Serve runs the newline-delimited JSON-RPC stdio loop until in is exhausted.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, scanBufferInitial), scanBufferMax)
	enc := json.NewEncoder(out)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var msg mcp.JSONRPCMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			if encErr := enc.Encode(s.errorResp(nil, parseError, "parse error")); encErr != nil {
				return encErr
			}
			continue
		}
		if resp := s.handleMessage(ctx, &msg); resp != nil {
			if err := enc.Encode(resp); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

func (s *Server) handleMessage(ctx context.Context, msg *mcp.JSONRPCMessage) *mcp.JSONRPCMessage {
	switch msg.Method {
	case "initialize":
		return s.result(msg.ID, s.initializeResult())
	case "notifications/initialized":
		return nil
	case "ping":
		return s.result(msg.ID, struct{}{})
	case "tools/list":
		return s.result(msg.ID, mcp.ToolsListResponse{Tools: s.toolDefs()})
	case "tools/call":
		return s.handleToolCall(ctx, msg)
	default:
		if msg.ID == nil {
			return nil // unknown notification — ignore
		}
		return s.errorResp(msg.ID, methodNotFound, "method not found: "+msg.Method)
	}
}

func (s *Server) initializeResult() mcp.InitializeResponse {
	return mcp.InitializeResponse{
		ProtocolVersion: mcp.ProtocolVersion,
		Capabilities: mcp.ServerCapabilities{
			Tools:     &mcp.ToolsCapability{},
			Resources: &mcp.ResourcesCapability{},
		},
		ServerInfo: mcp.Implementation{Name: serverName, Version: s.version},
	}
}

func (s *Server) toolDefs() []mcp.Tool {
	defs := make([]mcp.Tool, 0, len(s.order))
	for _, name := range s.order {
		defs = append(defs, s.tools[name].def)
	}
	return defs
}

func (s *Server) handleToolCall(ctx context.Context, msg *mcp.JSONRPCMessage) *mcp.JSONRPCMessage {
	var req mcp.ToolCallRequest
	if err := json.Unmarshal(msg.Params, &req); err != nil {
		return s.errorResp(msg.ID, invalidParams, "invalid params: "+err.Error())
	}
	entry, ok := s.tools[req.Name]
	if !ok {
		return s.result(msg.ID, errorToolResult("unknown tool: "+req.Name))
	}
	resp, err := entry.handler(ctx, req.Arguments)
	if err != nil {
		return s.result(msg.ID, errorToolResult(err.Error()))
	}
	return s.result(msg.ID, resp)
}

func (s *Server) result(id, v any) *mcp.JSONRPCMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return s.errorResp(id, internalError, err.Error())
	}
	return &mcp.JSONRPCMessage{JSONRPC: jsonRPCVersion, ID: id, Result: raw}
}

func (s *Server) errorResp(id any, code int, message string) *mcp.JSONRPCMessage {
	return &mcp.JSONRPCMessage{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Error:   &mcp.JSONRPCError{Code: code, Message: message},
	}
}

func textResult(text string) mcp.ToolCallResponse {
	return mcp.ToolCallResponse{Content: []mcp.Content{{Type: contentTypeText, Text: text}}}
}

func errorToolResult(text string) mcp.ToolCallResponse {
	return mcp.ToolCallResponse{IsError: true, Content: []mcp.Content{{Type: contentTypeText, Text: text}}}
}

// addTool registers a tool definition and handler, preserving call order.
func (s *Server) addTool(def mcp.Tool, h toolHandler) {
	s.tools[def.Name] = toolEntry{def: def, handler: h}
	s.order = append(s.order, def.Name)
}
