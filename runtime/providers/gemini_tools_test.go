package providers

import (
	"encoding/json"
	"testing"
)

func TestGeminiToolResponseParsing(t *testing.T) {
	// This is the actual response from Gemini that contains a function call
	geminiResponseJSON := `{
  "candidates": [
    {
      "content": {
        "parts": [
          {
            "functionCall": {
              "name": "getTodayStep",
              "args": {
                "project_id": "finish first draft"
              }
            },
            "thoughtSignature": "CpQCAdHtim//eFEosgdaIKSUFynLIA1Y3O+5yKnnzRmeUlMvFlCAB7lBGHVf8/7rO4/emJfKNevf7K6cRaeWu6Aa10jLOs7gNe7gWp/MgBQ586iJwBUduWQAst4er9SweS128cwOzJ2Z/CtlMuCJBvGFtVuVM1ZRsEyeCV87+HzlJAIFDl2P+XcztKMpgkhQ4OR6/eDt/h3nCqUfCclkztpy3MufXNPCFrNHpexPRKi4MskJDtg+XtjToKYBkicDu+3aeAQ/VP3t2IbK+Y9o+L/k9w16kIcP1xrAqJAqC38Gc+xR/qDSE5Qpg8BP3CEdKgeN9fgjh86mf0p2AWD2XId8CNbFlwytpEVxIMtmESjGCuZxieNy"
          }
        ],
        "role": "model"
      },
      "finishReason": "STOP",
      "index": 0,
      "finishMessage": "Model generated function call(s)."
    }
  ],
  "usageMetadata": {
    "promptTokenCount": 181,
    "candidatesTokenCount": 19,
    "totalTokenCount": 259,
    "promptTokensDetails": [
      {
        "modality": "TEXT",
        "tokenCount": 181
      }
    ],
    "thoughtsTokenCount": 59
  },
  "modelVersion": "gemini-2.5-flash",
  "responseId": "5frnaPn5DeqFkdUPiOqu6AQ"
}`

	// Create a Gemini tool provider
	provider := &GeminiToolProvider{
		GeminiProvider: &GeminiProvider{
			id:    "test-gemini",
			Model: "gemini-2.5-flash",
		},
	}

	// Parse the response
	chatResp, toolCalls, err := provider.parseToolResponse([]byte(geminiResponseJSON), ChatResponse{})
	if err != nil {
		t.Fatalf("Failed to parse Gemini response: %v", err)
	}

	// Verify that tool calls were extracted
	if len(toolCalls) == 0 {
		t.Errorf("Expected tool calls to be extracted, got 0")

		// Debug: Print what we got
		t.Logf("Chat response: %+v", chatResp)
		t.Logf("Tool calls: %+v", toolCalls)

		// Let's manually debug the parsing
		var resp geminiResponse
		if err := json.Unmarshal([]byte(geminiResponseJSON), &resp); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		t.Logf("Parsed response: %+v", resp)
		t.Logf("Number of candidates: %d", len(resp.Candidates))

		if len(resp.Candidates) > 0 {
			t.Logf("First candidate: %+v", resp.Candidates[0])
			t.Logf("Content parts: %+v", resp.Candidates[0].Content.Parts)

			for i, part := range resp.Candidates[0].Content.Parts {
				t.Logf("Part %d: %+v", i, part)

				// Try to manually extract function call
				partBytes, _ := json.Marshal(part)
				var rawPart map[string]interface{}
				if json.Unmarshal(partBytes, &rawPart) == nil {
					t.Logf("Raw part %d: %+v", i, rawPart)

					if funcCall, ok := rawPart["functionCall"].(map[string]interface{}); ok {
						t.Logf("Found functionCall: %+v", funcCall)
					} else {
						t.Logf("No functionCall found in raw part")
					}
				}
			}
		}

		return
	}

	// Verify the tool call details
	if len(toolCalls) != 1 {
		t.Errorf("Expected 1 tool call, got %d", len(toolCalls))
	}

	toolCall := toolCalls[0]
	if toolCall.Name != "getTodayStep" {
		t.Errorf("Expected tool name 'getTodayStep', got '%s'", toolCall.Name)
	}

	// Verify the arguments
	var args map[string]interface{}
	if err := json.Unmarshal(toolCall.Args, &args); err != nil {
		t.Fatalf("Failed to unmarshal tool args: %v", err)
	}

	if projectID, ok := args["project_id"].(string); !ok || projectID != "finish first draft" {
		t.Errorf("Expected project_id 'finish first draft', got %v", args["project_id"])
	}

	// Verify token counts
	if chatResp.CostInfo.InputTokens != 181 {
		t.Errorf("Expected 181 input tokens, got %d", chatResp.CostInfo.InputTokens)
	}

	if chatResp.CostInfo.OutputTokens != 19 {
		t.Errorf("Expected 19 output tokens, got %d", chatResp.CostInfo.OutputTokens)
	}

	t.Logf("SUCCESS: Tool call extracted correctly: %+v", toolCall)
}
