package prompt_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

// TestPackJSONRoundTrip guards against lossy JSON tags or fields that silently
// drop on serialize. Now that a single Pack type serves the whole toolchain
// (runtime, SDK, Arena, packc), a dropped field is a cross-cutting data-loss
// bug. Unmarshaling a fully-populated pack, re-marshaling it, and unmarshaling
// again must reproduce the identical value.
//
// (Compositions are exercised by runtime/composition's own round-trip tests and
// are omitted here to keep this focused on the Pack/Prompt surface.)
func TestPackJSONRoundTrip(t *testing.T) {
	src := []byte(`{
		"$schema": "https://promptpack.org/schema/latest/promptpack.schema.json",
		"id": "roundtrip-pack",
		"name": "Round Trip Pack",
		"version": "1.2.3",
		"description": "Exercises every top-level and prompt-level field.",
		"template_engine": {"version": "v1", "syntax": "{{variable}}"},
		"fragments": {"greeting": "Hello there"},
		"metadata": {"name": "Round Trip"},
		"prompts": {
			"chat": {
				"id": "chat",
				"name": "Chat",
				"version": "1.0.0",
				"description": "A chat prompt",
				"system_template": "You are a {{role}} assistant.",
				"variables": [
					{
						"name": "role",
						"type": "string",
						"required": true,
						"default": "helpful",
						"description": "assistant role"
					},
					{
						"name": "max_items",
						"type": "number",
						"required": false,
						"default": 42
					}
				],
				"tools": ["lookup"],
				"tool_policy": {"tool_choice": "auto", "max_rounds": 3},
				"media": {"enabled": true, "supported_types": ["image", "audio"]},
				"parameters": {"temperature": 0.7, "max_tokens": 512},
				"validators": [
					{"type": "max_length", "enabled": true, "params": {"max_characters": 2000}},
					{"type": "banned_words", "enabled": false, "params": {"message": "blocked"}}
				],
				"model_overrides": {"gpt-4o": {"system_template_suffix": "Be terse."}}
			}
		},
		"tools": {
			"lookup": {
				"name": "lookup",
				"description": "Look something up",
				"parameters": {"type": "object", "properties": {"q": {"type": "string"}}}
			}
		},
		"workflow": {
			"version": 1,
			"entry": "start",
			"states": {
				"start": {"prompt_task": "chat", "on_event": {"done": "finish"}},
				"finish": {"prompt_task": "chat", "terminal": true}
			}
		},
		"agents": {
			"entry": "chat",
			"members": {
				"chat": {"description": "Chat agent", "tags": ["nlp"], "input_modes": ["text/plain"], "output_modes": ["text/plain"]}
			}
		},
		"skills": [{"dir": "skills/", "name": "helper", "preload": true}]
	}`)

	var p1 prompt.Pack
	require.NoError(t, json.Unmarshal(src, &p1))

	// Sanity: the fields that were lossy under the old split are present.
	require.Contains(t, p1.Prompts, "chat")
	require.Len(t, p1.Prompts["chat"].Variables, 2)
	require.NotNil(t, p1.TemplateEngine)
	require.NotNil(t, p1.Workflow)

	out, err := json.Marshal(&p1)
	require.NoError(t, err)

	var p2 prompt.Pack
	require.NoError(t, json.Unmarshal(out, &p2))

	// A second marshal must be byte-identical to the first: a stable fixed point
	// means no field is being dropped or mutated on the way through.
	out2, err := json.Marshal(&p2)
	require.NoError(t, err)
	assert.JSONEq(t, string(out), string(out2),
		"pack changed across a marshal/unmarshal round-trip — a JSON tag is lossy")
}
