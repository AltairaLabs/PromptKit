package gemini

import (
	"encoding/json"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// Wire-protocol field keys for the Gemini Live tool-response message.
const (
	wireKeyResult   = "result"
	wireKeyResponse = "response"
)

// buildToolResponseMessage constructs Gemini's BidiGenerateContentToolResponse
// wire message from a set of tool execution results.
//
// Per the Gemini docs the shape is:
//
//	{ "toolResponse": { "functionResponses": [ { id, response, [error] }, ... ] } }
//
// Each result string is parsed as JSON when possible so the model receives a
// structured object; a non-JSON string is wrapped as {"result": "<string>"}.
// A failed tool execution (IsError) sets "error": true on its entry.
func buildToolResponseMessage(responses []providers.ToolResponse) map[string]interface{} {
	functionResponses := make([]map[string]interface{}, len(responses))
	for i, resp := range responses {
		// Parse result as JSON if possible, otherwise wrap as string
		var resultObj interface{}
		if err := json.Unmarshal([]byte(resp.Result), &resultObj); err != nil {
			// Result is not valid JSON, wrap it
			resultObj = map[string]interface{}{wireKeyResult: resp.Result}
		}

		funcResp := map[string]interface{}{
			"id":            resp.ToolCallID,
			wireKeyResponse: resultObj,
		}

		// Add error flag if the tool execution failed
		if resp.IsError {
			funcResp["error"] = true
		}

		functionResponses[i] = funcResp
	}

	return map[string]interface{}{
		"toolResponse": map[string]interface{}{
			"functionResponses": functionResponses,
		},
	}
}
