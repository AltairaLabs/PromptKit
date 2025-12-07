package pack

import (
"os"
"path/filepath"
"testing"

"github.com/stretchr/testify/assert"
"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	t.Run("non-existent file", func(t *testing.T) {
_, err := Load("/non/existent/path.pack.json")
assert.Error(t, err)
assert.Contains(t, err.Error(), "pack not found")
	})

	t.Run("valid pack file", func(t *testing.T) {
// Create a temp file
dir := t.TempDir()
		packPath := filepath.Join(dir, "test.pack.json")
		data := []byte(`{
			"id": "test-pack",
			"name": "Test Pack",
			"version": "1.0.0",
			"prompts": {
				"chat": {
					"id": "chat",
					"system_template": "You are helpful."
				}
			}
		}`)
		require.NoError(t, os.WriteFile(packPath, data, 0644))

		p, err := Load(packPath)
		require.NoError(t, err)
		assert.Equal(t, "test-pack", p.ID)
		assert.Equal(t, packPath, p.FilePath)
	})

	t.Run("relative path", func(t *testing.T) {
// Create a temp file in current directory
dir := t.TempDir()
		packPath := filepath.Join(dir, "rel.pack.json")
		data := []byte(`{"id": "rel", "prompts": {"p": {"id": "p", "system_template": "test"}}}`)
		require.NoError(t, os.WriteFile(packPath, data, 0644))

		// Test with relative path
		oldWd, _ := os.Getwd()
		_ = os.Chdir(dir)
		defer func() { _ = os.Chdir(oldWd) }()

		p, err := Load("rel.pack.json")
		require.NoError(t, err)
		assert.Equal(t, "rel", p.ID)
	})
}

func TestParse(t *testing.T) {
	t.Run("valid pack", func(t *testing.T) {
data := []byte(`{
			"id": "test-pack",
			"name": "Test Pack",
			"version": "1.0.0",
			"description": "A test pack",
			"prompts": {
				"chat": {
					"id": "chat",
					"name": "Chat",
					"description": "A chat prompt",
					"version": "1.0.0",
					"system_template": "You are a helpful assistant."
				}
			}
		}`)

p, err := Parse(data)
require.NoError(t, err)
assert.Equal(t, "test-pack", p.ID)
assert.Equal(t, "Test Pack", p.Name)
assert.Equal(t, "1.0.0", p.Version)
assert.Equal(t, "A test pack", p.Description)
assert.Len(t, p.Prompts, 1)

prompt := p.GetPrompt("chat")
require.NotNil(t, prompt)
assert.Equal(t, "chat", prompt.ID)
assert.Equal(t, "You are a helpful assistant.", prompt.SystemTemplate)
})

	t.Run("empty prompts", func(t *testing.T) {
data := []byte(`{"id": "test-pack", "prompts": {}}`)
_, err := Parse(data)
assert.Error(t, err)
assert.Contains(t, err.Error(), "no prompts")
	})

	t.Run("missing prompts", func(t *testing.T) {
data := []byte(`{"id": "test-pack"}`)
_, err := Parse(data)
assert.Error(t, err)
assert.Contains(t, err.Error(), "no prompts")
	})

	t.Run("invalid JSON", func(t *testing.T) {
data := []byte(`{invalid json}`)
_, err := Parse(data)
assert.Error(t, err)
assert.Contains(t, err.Error(), "failed to parse")
	})
}

func TestPackWithVariables(t *testing.T) {
	data := []byte(`{
		"id": "vars-pack",
		"prompts": {
			"greet": {
				"id": "greet",
				"system_template": "Hello {{name}}!",
				"variables": [
					{
						"name": "name",
						"type": "string",
						"description": "User's name",
						"required": true,
						"default": "World"
					}
				]
			}
		}
	}`)

	p, err := Parse(data)
	require.NoError(t, err)

	prompt := p.GetPrompt("greet")
	require.NotNil(t, prompt)
	require.Len(t, prompt.Variables, 1)

	v := prompt.Variables[0]
	assert.Equal(t, "name", v.Name)
	assert.Equal(t, "string", v.Type)
	assert.Equal(t, "User's name", v.Description)
	assert.True(t, v.Required)
	assert.Equal(t, "World", v.Default)
}

func TestPackWithTools(t *testing.T) {
	data := []byte(`{
		"id": "tools-pack",
		"prompts": {
			"assistant": {
				"id": "assistant",
				"system_template": "You have access to tools.",
				"tools": ["get_weather", "search"]
			}
		},
		"tools": {
			"get_weather": {
				"name": "get_weather",
				"description": "Get current weather",
				"parameters": {
					"type": "object",
					"properties": {
						"city": {"type": "string"}
					}
				}
			},
			"search": {
				"name": "search",
				"description": "Search the web",
				"parameters": {}
			}
		}
	}`)

	p, err := Parse(data)
	require.NoError(t, err)

	// Check tools at pack level
	assert.Len(t, p.Tools, 2)
	tool := p.GetTool("get_weather")
	require.NotNil(t, tool)
	assert.Equal(t, "get_weather", tool.Name)
	assert.Equal(t, "Get current weather", tool.Description)

	// Check tool reference in prompt
	prompt := p.GetPrompt("assistant")
	require.NotNil(t, prompt)
	require.Len(t, prompt.Tools, 2)
	assert.Contains(t, prompt.Tools, "get_weather")
	assert.Contains(t, prompt.Tools, "search")
}

func TestPackWithToolPolicy(t *testing.T) {
	data := []byte(`{
		"id": "policy-pack",
		"prompts": {
			"agent": {
				"id": "agent",
				"system_template": "Agent",
				"tool_policy": {
					"tool_choice": "auto",
					"max_rounds": 5,
					"max_tool_calls_per_turn": 3,
					"blocklist": ["dangerous_tool"]
				}
			}
		}
	}`)

	p, err := Parse(data)
	require.NoError(t, err)

	prompt := p.GetPrompt("agent")
	require.NotNil(t, prompt)
	require.NotNil(t, prompt.ToolPolicy)
	assert.Equal(t, "auto", prompt.ToolPolicy.ToolChoice)
	assert.Equal(t, 5, prompt.ToolPolicy.MaxRounds)
	assert.Equal(t, 3, prompt.ToolPolicy.MaxToolCallsPerTurn)
	assert.Contains(t, prompt.ToolPolicy.Blocklist, "dangerous_tool")
}

func TestPackWithParameters(t *testing.T) {
	data := []byte(`{
		"id": "params-pack",
		"prompts": {
			"creative": {
				"id": "creative",
				"system_template": "Be creative",
				"parameters": {
					"temperature": 0.9,
					"max_tokens": 2000,
					"top_p": 0.95,
					"top_k": 40
				}
			}
		}
	}`)

	p, err := Parse(data)
	require.NoError(t, err)

	prompt := p.GetPrompt("creative")
	require.NotNil(t, prompt)
	require.NotNil(t, prompt.Parameters)
	require.NotNil(t, prompt.Parameters.Temperature)
	assert.InDelta(t, 0.9, *prompt.Parameters.Temperature, 0.001)
	require.NotNil(t, prompt.Parameters.MaxTokens)
	assert.Equal(t, 2000, *prompt.Parameters.MaxTokens)
	require.NotNil(t, prompt.Parameters.TopP)
	assert.InDelta(t, 0.95, *prompt.Parameters.TopP, 0.001)
	require.NotNil(t, prompt.Parameters.TopK)
	assert.Equal(t, 40, *prompt.Parameters.TopK)
}

func TestPackWithValidators(t *testing.T) {
	data := []byte(`{
		"id": "val-pack",
		"prompts": {
			"safe": {
				"id": "safe",
				"system_template": "Be safe",
				"validators": [
					{
						"type": "banned_words",
						"config": {"words": ["bad", "evil"]}
					},
					{
						"type": "max_length",
						"config": {"max": 1000}
					}
				]
			}
		}
	}`)

	p, err := Parse(data)
	require.NoError(t, err)

	prompt := p.GetPrompt("safe")
	require.NotNil(t, prompt)
	require.Len(t, prompt.Validators, 2)
	assert.Equal(t, "banned_words", prompt.Validators[0].Type)
	assert.Equal(t, "max_length", prompt.Validators[1].Type)
}

func TestPackWithMediaConfig(t *testing.T) {
	data := []byte(`{
		"id": "media-pack",
		"prompts": {
			"vision": {
				"id": "vision",
				"system_template": "Analyze images",
				"media": {
					"allowed_types": ["image/png", "image/jpeg"],
					"max_size": 10485760
				}
			}
		}
	}`)

	p, err := Parse(data)
	require.NoError(t, err)

	prompt := p.GetPrompt("vision")
	require.NotNil(t, prompt)
	require.NotNil(t, prompt.MediaConfig)
	assert.Len(t, prompt.MediaConfig.AllowedTypes, 2)
	assert.Contains(t, prompt.MediaConfig.AllowedTypes, "image/png")
	assert.Equal(t, 10485760, prompt.MediaConfig.MaxSize)
}

func TestPackWithFragments(t *testing.T) {
	data := []byte(`{
		"id": "frag-pack",
		"prompts": {
			"chat": {"id": "chat", "system_template": "Test"}
		},
		"fragments": {
			"greeting": "Hello, welcome!",
			"farewell": "Goodbye!"
		}
	}`)

	p, err := Parse(data)
	require.NoError(t, err)

	assert.Len(t, p.Fragments, 2)
	assert.Equal(t, "Hello, welcome!", p.Fragments["greeting"])
	assert.Equal(t, "Goodbye!", p.Fragments["farewell"])
}

func TestPackListMethods(t *testing.T) {
	data := []byte(`{
		"id": "list-pack",
		"prompts": {
			"chat": {"id": "chat", "system_template": "Chat"},
			"agent": {"id": "agent", "system_template": "Agent"},
			"code": {"id": "code", "system_template": "Code"}
		},
		"tools": {
			"search": {"name": "search", "description": "Search"},
			"calc": {"name": "calc", "description": "Calculate"}
		}
	}`)

	p, err := Parse(data)
	require.NoError(t, err)

	prompts := p.ListPrompts()
	assert.Len(t, prompts, 3)
	assert.Contains(t, prompts, "chat")
	assert.Contains(t, prompts, "agent")
	assert.Contains(t, prompts, "code")

	tools := p.ListTools()
	assert.Len(t, tools, 2)
	assert.Contains(t, tools, "search")
	assert.Contains(t, tools, "calc")
}

func TestPackGetNonExistent(t *testing.T) {
	data := []byte(`{
		"id": "test",
		"prompts": {"chat": {"id": "chat", "system_template": "Test"}}
	}`)

	p, err := Parse(data)
	require.NoError(t, err)

	assert.Nil(t, p.GetPrompt("nonexistent"))
	assert.Nil(t, p.GetTool("nonexistent"))
}

func TestPackListToolsEmpty(t *testing.T) {
	data := []byte(`{
		"id": "test",
		"prompts": {"chat": {"id": "chat", "system_template": "Test"}}
	}`)

	p, err := Parse(data)
	require.NoError(t, err)

	tools := p.ListTools()
	assert.Nil(t, tools)
}
