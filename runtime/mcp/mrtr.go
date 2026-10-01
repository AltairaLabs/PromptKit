package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// Multi round-trip requests (2026-07-28 basic/patterns/mrtr). A modern server
// that needs input from the client — elicitation — does not send a request
// of its own: it answers the client's request with an input_required result
// listing what it needs, and the client retries the original request with
// the answers.

// maxInputRounds bounds how many times one request is retried with input,
// so a server that keeps asking cannot hold the client in a loop.
const maxInputRounds = 10

// mrtrMethods are the client requests a server may answer with
// input_required.
var mrtrMethods = map[string]bool{
	methodToolsCall:     true,
	methodPromptsGet:    true,
	methodResourcesRead: true,
}

// inputRequiredError carries an input_required result back from call to the
// caller that knows how to retry the request.
type inputRequiredError struct {
	result json.RawMessage
}

func (e *inputRequiredError) Error() string {
	return "mcp: server requires input before it can complete the request"
}

// InputRequiredResult is a server's interim answer asking for input before
// it completes a request.
type InputRequiredResult struct {
	ResultType    string                     `json:"resultType"`
	InputRequests map[string]InputRequest    `json:"inputRequests,omitempty"`
	RequestState  json.RawMessage            `json:"requestState,omitempty"`
	Meta          map[string]json.RawMessage `json:"_meta,omitempty"`
}

// InputRequest is one request for input inside an InputRequiredResult.
type InputRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// callTool invokes a tool. It is never retried on failure: a tool may have
// side effects, and a JSON-RPC error or a timeout does not mean it did not
// run. It is retried — with a new id — when a modern server answers
// input_required: with the client's answers to what it asked, and the
// server's requestState echoed back unchanged.
func (s *session) callTool(ctx context.Context, name string, arguments json.RawMessage) (*ToolCallResponse, error) {
	req := ToolCallRequest{Name: name, Arguments: arguments}
	for round := 0; ; round++ {
		var resp ToolCallResponse
		err := s.call(ctx, methodToolsCall, req, &resp, callOpts{})
		var ir *inputRequiredError
		switch {
		case err == nil:
			return &resp, nil
		case !errors.As(err, &ir):
			return nil, fmt.Errorf("tools/call request failed: %w", err)
		case round >= maxInputRounds:
			return nil, fmt.Errorf("tools/call request failed: server %s still requires input after %d rounds", s.name, round)
		}
		if req.InputResponses, req.RequestState, err = s.fulfill(ctx, ir.result); err != nil {
			return nil, fmt.Errorf("tools/call request failed: %w", err)
		}
	}
}

// fulfill answers an input_required result's requests. It returns the
// responses and the requestState to send with the retry: exactly as the
// server sent it, or nil if it sent none.
func (s *session) fulfill(
	ctx context.Context, result json.RawMessage,
) (map[string]json.RawMessage, json.RawMessage, error) {
	var ir InputRequiredResult
	if err := json.Unmarshal(result, &ir); err != nil {
		return nil, nil, fmt.Errorf("mcp: malformed input_required result: %w", err)
	}
	if len(ir.InputRequests) == 0 && len(ir.RequestState) == 0 {
		return nil, nil, errors.New("mcp: input_required result with neither inputRequests nor requestState")
	}
	var responses map[string]json.RawMessage
	keys := make([]string, 0, len(ir.InputRequests))
	for k := range ir.InputRequests {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		in := ir.InputRequests[key]
		answer, err := s.answerInput(ctx, in)
		if err != nil {
			return nil, nil, fmt.Errorf("mcp: cannot provide input %q: %w", key, err)
		}
		if responses == nil {
			responses = make(map[string]json.RawMessage, len(keys))
		}
		responses[key] = answer
	}
	return responses, ir.RequestState, nil
}

// answerInput produces the client's answer to one input request. Only
// elicitation is advertised, so only elicitation is answered.
func (s *session) answerInput(ctx context.Context, in InputRequest) (json.RawMessage, error) {
	if in.Method != methodElicitationCreate {
		return nil, fmt.Errorf("server asked for %q, which this client does not advertise", in.Method)
	}
	res, rpcErr := s.elicit(ctx, in.Params)
	if rpcErr != nil {
		return nil, errors.New(rpcErr.Message)
	}
	return json.Marshal(res)
}
