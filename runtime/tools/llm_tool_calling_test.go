package tools

// llm_tool_calling_test.go drives realistic LLM tool-call arguments through
// Registry.Execute and Registry.ExecuteAsync — the path a tool call takes in a
// conversation — and asserts what the tool's executor actually receives, or the
// validation error the caller gets back instead.
//
// The suite is organized by tool schema (matching real capability tools) and by
// LLM behavior pattern. A failing case means a model's output either reaches
// the tool in a shape the schema does not promise, or is rejected when the
// pipeline should have repaired it.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// argsEchoExecutor returns the arguments it was called with as its result, so
// the tool result is exactly what a tool implementation would have received.
type argsEchoExecutor struct{ calls int }

const argsEchoExecutorName = "args-echo"

func (e *argsEchoExecutor) Name() string { return argsEchoExecutorName }

func (e *argsEchoExecutor) Execute(
	_ context.Context, _ *ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	e.calls++
	return args, nil
}

// llmCallCase is one tool call as a model sent it. Exactly one of wantArgs
// (the JSON the executor receives) and wantDetail (a fragment of the
// ValidationError detail returned instead) is set.
type llmCallCase struct {
	name       string
	desc       *ToolDescriptor
	raw        string
	wantArgs   string
	wantDetail string
}

// assertLLMCallCases sends each case through both Registry.Execute and
// Registry.ExecuteAsync on a fresh registry and checks the executor's view.
func assertLLMCallCases(t *testing.T, cases []llmCallCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if (tc.wantArgs == "") == (tc.wantDetail == "") {
				t.Fatalf("case %q must set exactly one of wantArgs and wantDetail", tc.name)
			}
			desc := *tc.desc
			desc.Mode = argsEchoExecutorName
			exec := &argsEchoExecutor{}
			reg := NewRegistry()
			require.NoError(t, reg.Register(&desc))
			reg.RegisterExecutor(exec)

			syncRes, syncErr := reg.Execute(context.Background(), desc.Name, json.RawMessage(tc.raw))
			asyncRes, asyncErr := reg.ExecuteAsync(context.Background(), desc.Name, json.RawMessage(tc.raw))

			if tc.wantDetail != "" {
				for path, err := range map[string]error{"Execute": syncErr, "ExecuteAsync": asyncErr} {
					var verr *ValidationError
					require.Truef(t, errors.As(err, &verr), "%s: want *ValidationError, got %v", path, err)
					assert.Equal(t, "args_invalid", verr.Type, path)
					assert.Equal(t, desc.Name, verr.Tool, path)
					assert.Contains(t, verr.Detail, tc.wantDetail, path)
				}
				assert.Nil(t, syncRes)
				assert.Nil(t, asyncRes)
				assert.Equal(t, 0, exec.calls, "a call with invalid arguments must not reach the tool")
				return
			}

			require.NoError(t, syncErr)
			require.NoError(t, asyncErr)
			require.Empty(t, syncRes.Error)
			require.Equal(t, ToolStatusComplete, asyncRes.Status, asyncRes.Error)
			assert.JSONEq(t, tc.wantArgs, string(syncRes.Result), "Execute: args the tool received")
			assert.JSONEq(t, tc.wantArgs, string(asyncRes.Content), "ExecuteAsync: args the tool received")
			assert.Equal(t, 2, exec.calls)
		})
	}
}

// ---------------------------------------------------------------------------
// Realistic tool schemas matching capability-registered tools
// ---------------------------------------------------------------------------

// memoryRecallDescriptor matches runtime/memory/tools.go memory__recall
var memoryRecallDescriptor = &ToolDescriptor{
	Name: "memory__recall",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "What to search for in memory."},
			"types": {"type": "array", "items": {"type": "string"}, "description": "Filter by memory type."},
			"limit": {"type": "integer", "description": "Maximum number of results."},
			"min_confidence": {"type": "number", "description": "Minimum confidence threshold (0.0-1.0)."}
		},
		"required": ["query"]
	}`),
}

// memoryRememberDescriptor matches runtime/memory/tools.go memory__remember
var memoryRememberDescriptor = &ToolDescriptor{
	Name: "memory__remember",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"content": {"type": "string", "description": "What to remember."},
			"type": {"type": "string", "description": "Memory category."},
			"confidence": {"type": "number", "description": "How confident you are (0.0-1.0)."},
			"metadata": {"type": "object", "description": "Optional structured data."}
		},
		"required": ["content"]
	}`),
}

// memoryListDescriptor matches runtime/memory/tools.go memory__list
var memoryListDescriptor = &ToolDescriptor{
	Name: "memory__list",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"types": {"type": "array", "items": {"type": "string"}, "description": "Filter by memory type."},
			"limit": {"type": "integer", "description": "Maximum number of results."}
		}
	}`),
}

// memoryForgetDescriptor matches runtime/memory/tools.go memory__forget
var memoryForgetDescriptor = &ToolDescriptor{
	Name: "memory__forget",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"memory_id": {"type": "string", "description": "The ID of the memory to forget."}
		},
		"required": ["memory_id"]
	}`),
}

// workflowTransitionDescriptor matches runtime/workflow/transition_tool.go
var workflowTransitionDescriptor = &ToolDescriptor{
	Name: "workflow__transition",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"event": {"type": "string", "enum": ["Escalate", "Resolve", "Clarify"], "description": "The workflow event to trigger."},
			"context": {"type": "string", "description": "Summary of relevant context."}
		},
		"required": ["event", "context"]
	}`),
}

// workflowArtifactDescriptor matches runtime/workflow/artifact_tool.go
var workflowArtifactDescriptor = &ToolDescriptor{
	Name: "workflow__set_artifact",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "enum": ["customer_email", "order_id", "resolution_notes"], "description": "The artifact name to set."},
			"value": {"type": "string", "description": "The artifact value."}
		},
		"required": ["name", "value"]
	}`),
}

// simpleToolDescriptor — a basic tool for testing general patterns
var simpleToolDescriptor = &ToolDescriptor{
	Name: "get_weather",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"city": {"type": "string", "description": "City name."},
			"units": {"type": "string", "enum": ["celsius", "fahrenheit"], "description": "Temperature units."},
			"include_forecast": {"type": "boolean", "description": "Include 5-day forecast."},
			"days": {"type": "integer", "description": "Number of forecast days."}
		},
		"required": ["city"]
	}`),
}

// intArrayToolDescriptor — a tool with array of integers (user-defined, not capability)
var intArrayToolDescriptor = &ToolDescriptor{
	Name: "set_priorities",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"task_ids": {"type": "array", "items": {"type": "integer"}, "description": "Task IDs to prioritize."},
			"labels": {"type": "array", "items": {"type": "string"}, "description": "Labels to apply."}
		},
		"required": ["task_ids"]
	}`),
}

var nestedToolDescriptor = &ToolDescriptor{
	Name: "create_ticket",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"title": {"type": "string"},
			"priority": {"type": "string", "enum": ["low", "medium", "high"]},
			"assignee": {
				"type": "object",
				"properties": {
					"name": {"type": "string"},
					"email": {"type": "string"}
				},
				"required": ["name"]
			},
			"tags": {"type": "array", "items": {"type": "string"}}
		},
		"required": ["title"]
	}`),
}

var strictToolDescriptor = &ToolDescriptor{
	Name: "strict_tool",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string"},
			"limit": {"type": "integer"}
		},
		"required": ["query", "limit"],
		"additionalProperties": false
	}`),
}

// ---------------------------------------------------------------------------
// Strong LLM behavior: conformant calls reach the tool unchanged
// ---------------------------------------------------------------------------

func TestLLMToolCalling_StrongLLM_CorrectCalls(t *testing.T) {
	same := func(name string, desc *ToolDescriptor, raw string) llmCallCase {
		return llmCallCase{name: name, desc: desc, raw: raw, wantArgs: raw}
	}
	assertLLMCallCases(t, []llmCallCase{
		same("memory__recall with all fields", memoryRecallDescriptor,
			`{"query": "user preferences", "types": ["preference", "fact"], "limit": 5, "min_confidence": 0.7}`),
		same("memory__recall with required only", memoryRecallDescriptor, `{"query": "user preferences"}`),
		same("memory__remember with all fields", memoryRememberDescriptor,
			`{"content": "User prefers dark mode", "type": "preference", "confidence": 0.9, "metadata": {"source": "chat"}}`),
		same("memory__remember with required only", memoryRememberDescriptor, `{"content": "User prefers dark mode"}`),
		same("memory__list no args", memoryListDescriptor, `{}`),
		same("memory__forget with required", memoryForgetDescriptor, `{"memory_id": "abc-123"}`),
		same("workflow__transition correct enum", workflowTransitionDescriptor,
			`{"event": "Escalate", "context": "Customer is frustrated"}`),
		same("workflow__set_artifact correct enum", workflowArtifactDescriptor,
			`{"name": "customer_email", "value": "test@example.com"}`),
		same("simple tool correct call", simpleToolDescriptor,
			`{"city": "London", "units": "celsius", "include_forecast": true, "days": 5}`),
	})
}

// ---------------------------------------------------------------------------
// Null values for optional fields (Ollama, Llama, Mistral)
// ---------------------------------------------------------------------------

func TestLLMToolCalling_NullOptionalFields(t *testing.T) {
	assertLLMCallCases(t, []llmCallCase{
		{name: "memory__remember all optional fields null", desc: memoryRememberDescriptor,
			raw:      `{"content": "test value", "confidence": null, "metadata": null, "type": null}`,
			wantArgs: `{"content": "test value"}`},
		{name: "memory__recall optional fields null", desc: memoryRecallDescriptor,
			raw:      `{"query": "preferences", "types": null, "limit": null, "min_confidence": null}`,
			wantArgs: `{"query": "preferences"}`},
		{name: "memory__list all fields null", desc: memoryListDescriptor,
			raw: `{"types": null, "limit": null}`, wantArgs: `{}`},
		{name: "simple tool optional fields null", desc: simpleToolDescriptor,
			raw:      `{"city": "London", "units": null, "include_forecast": null, "days": null}`,
			wantArgs: `{"city": "London"}`},
		{name: "null required field still fails", desc: memoryRecallDescriptor,
			raw: `{"query": null, "types": null}`, wantDetail: "query: Invalid type. Expected: string, given: null"},
		{name: "null required field in workflow fails", desc: workflowTransitionDescriptor,
			raw: `{"event": null, "context": "some context"}`, wantDetail: "event: Invalid type. Expected: string, given: null"},
	})
}

// ---------------------------------------------------------------------------
// Empty strings for optional fields (GPT-3.5, smaller models)
// ---------------------------------------------------------------------------

func TestLLMToolCalling_EmptyStringOptionalFields(t *testing.T) {
	assertLLMCallCases(t, []llmCallCase{
		{name: "empty string for optional number field is dropped", desc: memoryRememberDescriptor,
			raw: `{"content": "test", "confidence": ""}`, wantArgs: `{"content": "test"}`},
		{name: "empty string for optional integer field is dropped", desc: memoryRecallDescriptor,
			raw: `{"query": "test", "limit": ""}`, wantArgs: `{"query": "test"}`},
		{name: "empty string for optional object field is dropped", desc: memoryRememberDescriptor,
			raw: `{"content": "test", "metadata": ""}`, wantArgs: `{"content": "test"}`},
		{name: "empty string for optional array field is dropped", desc: memoryRecallDescriptor,
			raw: `{"query": "test", "types": ""}`, wantArgs: `{"query": "test"}`},
		{name: "empty string for optional boolean field is dropped", desc: simpleToolDescriptor,
			raw: `{"city": "London", "include_forecast": ""}`, wantArgs: `{"city": "London"}`},
		{name: "empty string for optional string field is kept", desc: memoryRememberDescriptor,
			raw: `{"content": "test", "type": ""}`, wantArgs: `{"content": "test", "type": ""}`},
		{name: "empty string for required string field is kept", desc: memoryRecallDescriptor,
			raw: `{"query": ""}`, wantArgs: `{"query": ""}`},
	})
}

// ---------------------------------------------------------------------------
// String-encoded values
// ---------------------------------------------------------------------------

func TestLLMToolCalling_StringEncodedValues(t *testing.T) {
	assertLLMCallCases(t, []llmCallCase{
		{name: "string-encoded integer", desc: memoryRecallDescriptor,
			raw: `{"query": "test", "limit": "5"}`, wantArgs: `{"query": "test", "limit": 5}`},
		{name: "string-encoded number", desc: memoryRememberDescriptor,
			raw: `{"content": "test", "confidence": "0.8"}`, wantArgs: `{"content": "test", "confidence": 0.8}`},
		{name: "string-encoded boolean", desc: simpleToolDescriptor,
			raw:      `{"city": "London", "include_forecast": "true"}`,
			wantArgs: `{"city": "London", "include_forecast": true}`},
		{name: "string-encoded object", desc: memoryRememberDescriptor,
			raw:      `{"content": "test", "metadata": "{\"source\": \"chat\"}"}`,
			wantArgs: `{"content": "test", "metadata": {"source": "chat"}}`},
		{name: "string-encoded array", desc: memoryRecallDescriptor,
			raw:      `{"query": "test", "types": "[\"preference\", \"fact\"]"}`,
			wantArgs: `{"query": "test", "types": ["preference", "fact"]}`},
		{name: "unparseable string for integer field is rejected", desc: memoryRecallDescriptor,
			raw: `{"query": "test", "limit": "many"}`, wantDetail: "limit: Invalid type. Expected: integer, given: string"},
	})
}

// ---------------------------------------------------------------------------
// Bare string instead of array (weak models)
// ---------------------------------------------------------------------------

func TestLLMToolCalling_BareStringForArray(t *testing.T) {
	assertLLMCallCases(t, []llmCallCase{
		{name: "bare string for array field", desc: memoryRecallDescriptor,
			raw: `{"query": "test", "types": "preference"}`, wantArgs: `{"query": "test", "types": ["preference"]}`},
		{name: "bare string for array in memory__list", desc: memoryListDescriptor,
			raw: `{"types": "episodic"}`, wantArgs: `{"types": ["episodic"]}`},
	})
}

// ---------------------------------------------------------------------------
// Enum case mismatch (weak models)
// ---------------------------------------------------------------------------

func TestLLMToolCalling_EnumCaseMismatch(t *testing.T) {
	assertLLMCallCases(t, []llmCallCase{
		{name: "lowercase enum value", desc: workflowTransitionDescriptor,
			raw:      `{"event": "escalate", "context": "Customer needs help"}`,
			wantArgs: `{"event": "Escalate", "context": "Customer needs help"}`},
		{name: "uppercase enum value", desc: workflowTransitionDescriptor,
			raw:      `{"event": "RESOLVE", "context": "Issue fixed"}`,
			wantArgs: `{"event": "Resolve", "context": "Issue fixed"}`},
		{name: "mixed case enum value", desc: workflowTransitionDescriptor,
			raw:      `{"event": "cLaRiFy", "context": "Need more info"}`,
			wantArgs: `{"event": "Clarify", "context": "Need more info"}`},
		{name: "artifact name exact match", desc: workflowArtifactDescriptor,
			raw:      `{"name": "customer_email", "value": "test@example.com"}`,
			wantArgs: `{"name": "customer_email", "value": "test@example.com"}`},
		{name: "artifact name wrong case", desc: workflowArtifactDescriptor,
			raw:      `{"name": "Customer_Email", "value": "test@example.com"}`,
			wantArgs: `{"name": "customer_email", "value": "test@example.com"}`},
		{name: "enum value not in list at all fails", desc: workflowTransitionDescriptor,
			raw: `{"event": "Cancel", "context": "Cancelling"}`, wantDetail: "event: event must be one of the following"},
		{name: "simple tool enum case mismatch", desc: simpleToolDescriptor,
			raw: `{"city": "London", "units": "Celsius"}`, wantArgs: `{"city": "London", "units": "celsius"}`},
	})
}

// ---------------------------------------------------------------------------
// Whitespace issues (various LLMs)
// ---------------------------------------------------------------------------

func TestLLMToolCalling_WhitespaceIssues(t *testing.T) {
	assertLLMCallCases(t, []llmCallCase{
		{name: "trailing whitespace in enum value", desc: workflowTransitionDescriptor,
			raw:      `{"event": "Escalate ", "context": "Customer needs help"}`,
			wantArgs: `{"event": "Escalate", "context": "Customer needs help"}`},
		{name: "leading whitespace in enum value", desc: workflowTransitionDescriptor,
			raw:      `{"event": " Escalate", "context": "Customer needs help"}`,
			wantArgs: `{"event": "Escalate", "context": "Customer needs help"}`},
		{name: "whitespace in regular string is preserved", desc: memoryRecallDescriptor,
			raw: `{"query": " user preferences "}`, wantArgs: `{"query": " user preferences "}`},
	})
}

// ---------------------------------------------------------------------------
// Boolean variants (weak models)
// ---------------------------------------------------------------------------

func TestLLMToolCalling_BooleanVariants(t *testing.T) {
	forecast := func(name, value, want string) llmCallCase {
		return llmCallCase{name: name, desc: simpleToolDescriptor,
			raw:      `{"city": "London", "include_forecast": ` + value + `}`,
			wantArgs: `{"city": "London", "include_forecast": ` + want + `}`}
	}
	assertLLMCallCases(t, []llmCallCase{
		forecast("yes for boolean", `"yes"`, "true"),
		forecast("no for boolean", `"no"`, "false"),
		forecast("Yes capitalized for boolean", `"Yes"`, "true"),
		forecast("string 1 for boolean", `"1"`, "true"),
		forecast("string 0 for boolean", `"0"`, "false"),
		forecast("integer 1 for boolean", "1", "true"),
		forecast("integer 0 for boolean", "0", "false"),
		{name: "integer 2 for boolean is rejected", desc: simpleToolDescriptor,
			raw:        `{"city": "London", "include_forecast": 2}`,
			wantDetail: "include_forecast: Invalid type. Expected: boolean, given: integer"},
	})
}

// ---------------------------------------------------------------------------
// Number type confusion (weak models)
// ---------------------------------------------------------------------------

func TestLLMToolCalling_NumberTypeConfusion(t *testing.T) {
	assertLLMCallCases(t, []llmCallCase{
		{name: "float with .0 for integer field", desc: memoryRecallDescriptor,
			raw: `{"query": "test", "limit": 5.0}`, wantArgs: `{"query": "test", "limit": 5}`},
		{name: "float with fractional part for integer field fails", desc: memoryRecallDescriptor,
			raw: `{"query": "test", "limit": 5.5}`, wantDetail: "limit: Invalid type. Expected: integer, given: number"},
		{name: "negative number for confidence", desc: memoryRememberDescriptor,
			raw: `{"content": "test", "confidence": -0.5}`, wantArgs: `{"content": "test", "confidence": -0.5}`},
		{name: "string-encoded float for integer field", desc: memoryRecallDescriptor,
			raw: `{"query": "test", "limit": "5.0"}`, wantArgs: `{"query": "test", "limit": 5}`},
	})
}

// ---------------------------------------------------------------------------
// Extra/unknown fields (various LLMs)
// ---------------------------------------------------------------------------

func TestLLMToolCalling_ExtraFields(t *testing.T) {
	assertLLMCallCases(t, []llmCallCase{
		{name: "extra fields pass through to the tool", desc: memoryRecallDescriptor,
			raw:      `{"query": "test", "extra_field": "something", "another": 42}`,
			wantArgs: `{"query": "test", "extra_field": "something", "another": 42}`},
		{name: "extra fields with workflow tool", desc: workflowTransitionDescriptor,
			raw:      `{"event": "escalate", "context": "Help needed", "reason": "customer angry"}`,
			wantArgs: `{"event": "Escalate", "context": "Help needed", "reason": "customer angry"}`},
	})
}

// ---------------------------------------------------------------------------
// Combined weak-LLM patterns (realistic multi-issue calls)
// ---------------------------------------------------------------------------

func TestLLMToolCalling_CombinedWeakPatterns(t *testing.T) {
	assertLLMCallCases(t, []llmCallCase{
		{name: "Ollama-style: nulls + string-encoded values", desc: memoryRememberDescriptor,
			raw:      `{"content": "user prefers dark mode", "confidence": "0.8", "metadata": null, "type": null}`,
			wantArgs: `{"content": "user prefers dark mode", "confidence": 0.8}`},
		{name: "Llama-style: bare strings + nulls", desc: memoryRecallDescriptor,
			raw:      `{"query": "food preferences", "types": "preference", "limit": null, "min_confidence": null}`,
			wantArgs: `{"query": "food preferences", "types": ["preference"]}`},
		{name: "Mistral-style: empty strings everywhere", desc: memoryRememberDescriptor,
			raw:      `{"content": "test", "confidence": "", "metadata": "", "type": ""}`,
			wantArgs: `{"content": "test", "type": ""}`},
		{name: "weak model workflow: wrong case + trailing space", desc: workflowTransitionDescriptor,
			raw:      `{"event": "escalate ", "context": "needs help"}`,
			wantArgs: `{"event": "Escalate", "context": "needs help"}`},
		{name: "weak model: string numbers + null + bare array", desc: memoryRecallDescriptor,
			raw:      `{"query": "test", "types": "fact", "limit": "10", "min_confidence": null}`,
			wantArgs: `{"query": "test", "types": ["fact"], "limit": 10}`},
		{name: "weak model: yes/no boolean + null optional", desc: simpleToolDescriptor,
			raw:      `{"city": "London", "include_forecast": "yes", "days": null, "units": null}`,
			wantArgs: `{"city": "London", "include_forecast": true}`},
		{name: "weak model: integer boolean + empty string enum", desc: simpleToolDescriptor,
			raw:      `{"city": "London", "include_forecast": 1, "units": ""}`,
			wantArgs: `{"city": "London", "include_forecast": true}`},
	})
}

// ---------------------------------------------------------------------------
// Array element type coercion (arbitrary tool schemas)
// ---------------------------------------------------------------------------

func TestLLMToolCalling_ArrayElementCoercion(t *testing.T) {
	assertLLMCallCases(t, []llmCallCase{
		{name: "string elements in integer array", desc: intArrayToolDescriptor,
			raw: `{"task_ids": ["1", "2", "3"]}`, wantArgs: `{"task_ids": [1, 2, 3]}`},
		{name: "mixed string and int in integer array", desc: intArrayToolDescriptor,
			raw: `{"task_ids": [1, "2", 3]}`, wantArgs: `{"task_ids": [1, 2, 3]}`},
		{name: "string array elements stay as strings", desc: intArrayToolDescriptor,
			raw:      `{"task_ids": [1, 2], "labels": ["urgent", "10"]}`,
			wantArgs: `{"task_ids": [1, 2], "labels": ["urgent", "10"]}`},
		{name: "null element in integer array fails", desc: intArrayToolDescriptor,
			raw: `{"task_ids": [1, null, 3]}`, wantDetail: "task_ids.1: Invalid type. Expected: integer, given: null"},
	})
}

// ---------------------------------------------------------------------------
// Nested object patterns (arbitrary tool schemas)
// ---------------------------------------------------------------------------

func TestLLMToolCalling_NestedObjects(t *testing.T) {
	assertLLMCallCases(t, []llmCallCase{
		{name: "correct nested object", desc: nestedToolDescriptor,
			raw:      `{"title": "Bug report", "assignee": {"name": "Alice", "email": "alice@example.com"}}`,
			wantArgs: `{"title": "Bug report", "assignee": {"name": "Alice", "email": "alice@example.com"}}`},
		{name: "null optional nested object", desc: nestedToolDescriptor,
			raw:      `{"title": "Bug report", "assignee": null, "tags": null, "priority": null}`,
			wantArgs: `{"title": "Bug report"}`},
		{name: "string-encoded nested object", desc: nestedToolDescriptor,
			raw:      `{"title": "Bug report", "assignee": "{\"name\": \"Alice\"}"}`,
			wantArgs: `{"title": "Bug report", "assignee": {"name": "Alice"}}`},
		{name: "top-level enum case mismatch beside nested object", desc: nestedToolDescriptor,
			raw: `{"title": "Bug report", "priority": "High"}`, wantArgs: `{"title": "Bug report", "priority": "high"}`},
		{name: "empty string for optional enum with nested", desc: nestedToolDescriptor,
			raw: `{"title": "Bug report", "priority": "", "tags": null}`, wantArgs: `{"title": "Bug report"}`},
		{name: "nested required field missing fails", desc: nestedToolDescriptor,
			raw: `{"title": "Bug report", "assignee": {"email": "a@example.com"}}`, wantDetail: "assignee: name is required"},
	})
}

// ---------------------------------------------------------------------------
// additionalProperties:false (OpenAI strict mode schemas)
// ---------------------------------------------------------------------------

func TestLLMToolCalling_StrictSchemas(t *testing.T) {
	assertLLMCallCases(t, []llmCallCase{
		{name: "extra fields rejected in strict schema", desc: strictToolDescriptor,
			raw: `{"query": "test", "limit": 5, "extra": "field"}`, wantDetail: "Additional property extra is not allowed"},
		{name: "null required field not stripped in strict schema", desc: strictToolDescriptor,
			raw: `{"query": "test", "limit": null}`, wantDetail: "limit: Invalid type. Expected: integer, given: null"},
		{name: "empty string for required integer is not stripped", desc: strictToolDescriptor,
			raw: `{"query": "test", "limit": ""}`, wantDetail: "limit: Invalid type. Expected: integer, given: string"},
		{name: "string coercion still works in strict schema", desc: strictToolDescriptor,
			raw: `{"query": "test", "limit": "5"}`, wantArgs: `{"query": "test", "limit": 5}`},
	})
}

// ---------------------------------------------------------------------------
// Edge cases that must still fail (guard rails)
// ---------------------------------------------------------------------------

func TestLLMToolCalling_ShouldStillFail(t *testing.T) {
	assertLLMCallCases(t, []llmCallCase{
		{name: "completely missing required fields", desc: memoryRecallDescriptor,
			raw: `{}`, wantDetail: "query is required"},
		{name: "wrong type for required field", desc: memoryRecallDescriptor,
			raw: `{"query": 42}`, wantDetail: "query: Invalid type. Expected: string, given: integer"},
		{name: "completely invalid enum value", desc: workflowTransitionDescriptor,
			raw: `{"event": "Destroy", "context": "bye"}`, wantDetail: "event: event must be one of the following"},
		{name: "null for all required fields", desc: workflowTransitionDescriptor,
			raw: `{"event": null, "context": null}`, wantDetail: "context: Invalid type. Expected: string, given: null"},
		{name: "empty object when fields required", desc: workflowTransitionDescriptor,
			raw: `{}`, wantDetail: "event is required"},
		{name: "array for string field", desc: memoryRecallDescriptor,
			raw: `{"query": ["a", "b"]}`, wantDetail: "query: Invalid type. Expected: string, given: array"},
	})
}
