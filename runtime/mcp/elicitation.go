package mcp

import (
	"context"
	"encoding/json"
	"fmt"
)

// Elicitation modes (MCP client/elicitation).
const (
	ElicitModeForm = "form"
	ElicitModeURL  = "url"
)

// Elicitation actions a user's answer can carry.
const (
	ElicitActionAccept  = "accept"
	ElicitActionDecline = "decline"
	ElicitActionCancel  = "cancel"
)

const (
	methodElicitationCreate = "elicitation/create"
	codeInvalidParams       = -32602
)

// ElicitRequest is a server's request for information from the user
// (MCP client/elicitation). In form mode the answer is structured input
// matching RequestedSchema.
type ElicitRequest struct {
	// Mode is ElicitModeForm (the default when absent) or ElicitModeURL.
	Mode            string          `json:"mode,omitempty"`
	Message         string          `json:"message"`
	RequestedSchema json.RawMessage `json:"requestedSchema,omitempty"`
	URL             string          `json:"url,omitempty"`
}

// ElicitResult is the user's answer to an ElicitRequest.
type ElicitResult struct {
	// Action is ElicitActionAccept, ElicitActionDecline or ElicitActionCancel.
	Action string `json:"action"`
	// Content is the submitted input, for an accepted form request.
	Content json.RawMessage `json:"content,omitempty"`
}

// ElicitationHandler asks the user for what a server requested and returns
// their answer. server is the name of the MCP server asking.
//
// PromptKit does not talk to users; the host does. Setting a handler on
// ClientOptions is what makes the client advertise the elicitation
// capability, so a client without one is never asked.
type ElicitationHandler func(ctx context.Context, server string, req ElicitRequest) (ElicitResult, error)

// elicit runs the handler for one elicitation request and validates the
// answer, returning a JSON-RPC error for anything the client cannot serve.
func (s *session) elicit(ctx context.Context, params json.RawMessage) (*ElicitResult, *JSONRPCError) {
	if s.opts.ElicitationHandler == nil {
		return nil, &JSONRPCError{Code: codeMethodNotFound, Message: "Method not found: " + methodElicitationCreate}
	}
	var req ElicitRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &JSONRPCError{Code: codeInvalidParams, Message: "invalid elicitation request: " + err.Error()}
	}
	if req.Mode == "" {
		req.Mode = ElicitModeForm
	}
	if req.Mode != ElicitModeForm {
		// Only form mode is advertised.
		return nil, &JSONRPCError{Code: codeInvalidParams, Message: "unsupported elicitation mode: " + req.Mode}
	}
	res, err := s.opts.ElicitationHandler(ctx, s.name, req)
	if err != nil {
		return nil, &JSONRPCError{Code: codeInternalError, Message: "elicitation failed: " + err.Error()}
	}
	switch res.Action {
	case ElicitActionAccept, ElicitActionDecline, ElicitActionCancel:
	default:
		msg := fmt.Sprintf("elicitation handler returned action %q", res.Action)
		return nil, &JSONRPCError{Code: codeInternalError, Message: msg}
	}
	if res.Action != ElicitActionAccept {
		res.Content = nil
		return &res, nil
	}
	content, err := applyElicitDefaults(req.RequestedSchema, res.Content)
	if err != nil {
		msg := "elicitation handler returned invalid content: " + err.Error()
		return nil, &JSONRPCError{Code: codeInternalError, Message: msg}
	}
	res.Content = content
	return &res, nil
}

// applyElicitDefaults fills properties the answer omits with the defaults
// the requested schema declares, so an accepted form carries the values the
// server offered as starting points (2025-11-25 client/elicitation: clients
// SHOULD pre-populate fields with defaults). Elicitation schemas are flat
// objects of primitives, so only top-level properties are considered.
func applyElicitDefaults(schema, content json.RawMessage) (json.RawMessage, error) {
	var s struct {
		Properties map[string]struct {
			Default json.RawMessage `json:"default"`
		} `json:"properties"`
	}
	if len(schema) == 0 || json.Unmarshal(schema, &s) != nil || len(s.Properties) == 0 {
		return content, nil
	}
	answer := map[string]json.RawMessage{}
	if len(content) > 0 && string(content) != jsonNull {
		if err := json.Unmarshal(content, &answer); err != nil {
			return nil, err
		}
	}
	filled := false
	for name, prop := range s.Properties {
		if _, ok := answer[name]; !ok && len(prop.Default) > 0 {
			answer[name] = prop.Default
			filled = true
		}
	}
	if !filled {
		return content, nil
	}
	return json.Marshal(answer)
}
