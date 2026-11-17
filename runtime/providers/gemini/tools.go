package gemini

import (
	"encoding/json"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// Wire-protocol field keys for the Gemini Live tool-response message.
const (
	wireKeyResult   = "result"
	wireKeyResponse = "response"
	wireKeyError    = "error"
)

// toolResponseMessage constructs Gemini's BidiGenerateContentToolResponse
// wire message from a set of tool execution results.
//
// Per the Gemini docs the shape is:
//
//	{ "toolResponse": { "functionResponses": [ { id, response }, ... ] } }
//
// response is a JSON object. A result that parses to one is sent as is; any
// other result (an array, a scalar, text) is wrapped as {"result": ...}, as a
// text turn's function response is. A failed call (IsError) is sent as
// {"error": "<text>"}: FunctionResponse has no error flag, and the model reads
// a failure from the response's "error" key.
//
// parts[i], when present, is the function response parts of responses[i]
// (a result's images, as inline data; see live_tool_media.go).
func toolResponseMessage(responses []providers.ToolResponse, parts [][]map[string]any) map[string]interface{} {
	functionResponses := make([]map[string]interface{}, len(responses))
	for i, resp := range responses {
		functionResponses[i] = map[string]interface{}{
			"id":            resp.ToolCallID,
			wireKeyResponse: liveToolResponseObject(resp),
		}
		if i < len(parts) && len(parts[i]) > 0 {
			functionResponses[i][wireKeyParts] = parts[i]
		}
	}

	return map[string]interface{}{
		"toolResponse": map[string]interface{}{
			"functionResponses": functionResponses,
		},
	}
}

// liveToolResponseObject is the response object of one function response.
func liveToolResponseObject(resp providers.ToolResponse) map[string]interface{} {
	if resp.IsError {
		return map[string]interface{}{wireKeyError: resp.Result}
	}
	var parsed interface{}
	if err := json.Unmarshal([]byte(resp.Result), &parsed); err != nil {
		return map[string]interface{}{wireKeyResult: resp.Result}
	}
	if obj, ok := parsed.(map[string]interface{}); ok {
		return obj
	}
	return map[string]interface{}{wireKeyResult: parsed}
}
