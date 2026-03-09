package sdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

func TestResolveA2AHeaders(t *testing.T) {
	t.Run("nil headers and no env returns nil", func(t *testing.T) {
		cfg := &tools.A2AConfig{}
		result := resolveA2AHeaders(cfg)
		assert.Nil(t, result)
	})

	t.Run("static headers only", func(t *testing.T) {
		cfg := &tools.A2AConfig{
			Headers: map[string]string{
				"X-Tenant": "acme",
				"X-Region": "us-east-1",
			},
		}
		result := resolveA2AHeaders(cfg)
		assert.Equal(t, map[string]string{
			"X-Tenant": "acme",
			"X-Region": "us-east-1",
		}, result)
	})

	t.Run("env headers resolved from environment", func(t *testing.T) {
		t.Setenv("TEST_A2A_KEY", "secret-value")
		cfg := &tools.A2AConfig{
			HeadersFromEnv: []string{"X-API-Key=TEST_A2A_KEY"},
		}
		result := resolveA2AHeaders(cfg)
		assert.Equal(t, map[string]string{"X-API-Key": "secret-value"}, result)
	})

	t.Run("env header with unset env var is skipped", func(t *testing.T) {
		cfg := &tools.A2AConfig{
			HeadersFromEnv: []string{"X-Missing=UNSET_A2A_VAR_12345"},
		}
		result := resolveA2AHeaders(cfg)
		assert.Nil(t, result)
	})

	t.Run("static and env headers merged", func(t *testing.T) {
		t.Setenv("TEST_A2A_SECRET", "from-env")
		cfg := &tools.A2AConfig{
			Headers:        map[string]string{"X-Static": "value"},
			HeadersFromEnv: []string{"X-Dynamic=TEST_A2A_SECRET"},
		}
		result := resolveA2AHeaders(cfg)
		assert.Equal(t, "value", result["X-Static"])
		assert.Equal(t, "from-env", result["X-Dynamic"])
	})

	t.Run("malformed env spec without equals is skipped", func(t *testing.T) {
		cfg := &tools.A2AConfig{
			HeadersFromEnv: []string{"no-equals-sign"},
		}
		result := resolveA2AHeaders(cfg)
		assert.Nil(t, result)
	})
}

func TestEnsureA2ACapability(t *testing.T) {
	t.Run("no bridge and no agents returns caps unchanged", func(t *testing.T) {
		caps := []Capability{}
		cfg := &config{}
		result := ensureA2ACapability(caps, cfg)
		assert.Empty(t, result)
	})

	t.Run("adds capability when a2aAgents present", func(t *testing.T) {
		caps := []Capability{}
		cfg := &config{
			a2aAgents: []a2aAgentConfig{
				{url: "https://agent.example.com", config: &tools.A2AConfig{AgentURL: "https://agent.example.com"}},
			},
		}
		result := ensureA2ACapability(caps, cfg)
		require.Len(t, result, 1)
		assert.Equal(t, nsA2A, result[0].Name())
	})

	t.Run("does not duplicate when A2ACapability already exists", func(t *testing.T) {
		existing := NewA2ACapability()
		caps := []Capability{existing}
		cfg := &config{
			a2aAgents: []a2aAgentConfig{
				{url: "https://agent.example.com", config: &tools.A2AConfig{}},
			},
		}
		result := ensureA2ACapability(caps, cfg)
		require.Len(t, result, 1)
		assert.Same(t, existing, result[0])
	})
}

func TestA2AAgentBuilder(t *testing.T) {
	t.Run("basic creation with URL", func(t *testing.T) {
		b := NewA2AAgent("https://agent.example.com")
		cfg := b.Build()

		assert.Equal(t, "https://agent.example.com", cfg.AgentURL)
		assert.Nil(t, cfg.Auth)
		assert.Empty(t, cfg.HeadersFromEnv)
		assert.Equal(t, 0, cfg.TimeoutMs)
		assert.Nil(t, cfg.RetryPolicy)
		assert.Nil(t, cfg.SkillFilter)
	})

	t.Run("WithAuth sets auth config", func(t *testing.T) {
		cfg := NewA2AAgent("https://agent.example.com").
			WithAuth("Bearer", "my-token").
			Build()

		require.NotNil(t, cfg.Auth)
		assert.Equal(t, "Bearer", cfg.Auth.Scheme)
		assert.Equal(t, "my-token", cfg.Auth.Token)
		assert.Empty(t, cfg.Auth.TokenEnv)
	})

	t.Run("WithAuthFromEnv sets token env", func(t *testing.T) {
		cfg := NewA2AAgent("https://agent.example.com").
			WithAuthFromEnv("Bearer", "AGENT_TOKEN").
			Build()

		require.NotNil(t, cfg.Auth)
		assert.Equal(t, "Bearer", cfg.Auth.Scheme)
		assert.Empty(t, cfg.Auth.Token)
		assert.Equal(t, "AGENT_TOKEN", cfg.Auth.TokenEnv)
	})

	t.Run("WithHeader adds headers", func(t *testing.T) {
		cfg := NewA2AAgent("https://agent.example.com").
			WithHeader("X-Tenant-ID", "acme").
			WithHeader("X-Request-ID", "123").
			Build()

		assert.Equal(t, "acme", cfg.Headers["X-Tenant-ID"])
		assert.Equal(t, "123", cfg.Headers["X-Request-ID"])
	})

	t.Run("WithHeaderFromEnv adds env-based headers", func(t *testing.T) {
		cfg := NewA2AAgent("https://agent.example.com").
			WithHeaderFromEnv("X-API-Key=API_KEY_ENV").
			WithHeaderFromEnv("X-Secret=SECRET_ENV").
			Build()

		assert.Equal(t, []string{"X-API-Key=API_KEY_ENV", "X-Secret=SECRET_ENV"}, cfg.HeadersFromEnv)
	})

	t.Run("WithTimeout sets timeout", func(t *testing.T) {
		cfg := NewA2AAgent("https://agent.example.com").
			WithTimeout(5000).
			Build()

		assert.Equal(t, 5000, cfg.TimeoutMs)
	})

	t.Run("WithRetryPolicy sets retry config", func(t *testing.T) {
		cfg := NewA2AAgent("https://agent.example.com").
			WithRetryPolicy(3, 100, 5000).
			Build()

		require.NotNil(t, cfg.RetryPolicy)
		assert.Equal(t, 3, cfg.RetryPolicy.MaxRetries)
		assert.Equal(t, 100, cfg.RetryPolicy.InitialDelayMs)
		assert.Equal(t, 5000, cfg.RetryPolicy.MaxDelayMs)
	})

	t.Run("WithSkillFilter sets skill filter", func(t *testing.T) {
		filter := &tools.A2ASkillFilter{
			Allowlist: []string{"forecast", "alerts"},
		}
		cfg := NewA2AAgent("https://agent.example.com").
			WithSkillFilter(filter).
			Build()

		require.NotNil(t, cfg.SkillFilter)
		assert.Equal(t, []string{"forecast", "alerts"}, cfg.SkillFilter.Allowlist)
	})

	t.Run("full builder chain", func(t *testing.T) {
		cfg := NewA2AAgent("https://agent.example.com").
			WithAuth("Bearer", "tok").
			WithHeader("X-Tenant", "acme").
			WithHeaderFromEnv("X-Key=KEY_ENV").
			WithTimeout(3000).
			WithRetryPolicy(2, 200, 10000).
			WithSkillFilter(&tools.A2ASkillFilter{Blocklist: []string{"debug"}}).
			Build()

		assert.Equal(t, "https://agent.example.com", cfg.AgentURL)
		assert.Equal(t, "Bearer", cfg.Auth.Scheme)
		assert.Equal(t, "tok", cfg.Auth.Token)
		assert.Equal(t, "acme", cfg.Headers["X-Tenant"])
		assert.Equal(t, []string{"X-Key=KEY_ENV"}, cfg.HeadersFromEnv)
		assert.Equal(t, 3000, cfg.TimeoutMs)
		assert.Equal(t, 2, cfg.RetryPolicy.MaxRetries)
		assert.Equal(t, []string{"debug"}, cfg.SkillFilter.Blocklist)
	})
}

func TestWithA2AAgent(t *testing.T) {
	t.Run("adds agent to config", func(t *testing.T) {
		agent := NewA2AAgent("https://agent.example.com").
			WithAuth("Bearer", "token123").
			WithTimeout(5000)

		opt := WithA2AAgent(agent)
		c := &config{}
		err := opt(c)

		require.NoError(t, err)
		require.Len(t, c.a2aAgents, 1)
		assert.Equal(t, "https://agent.example.com", c.a2aAgents[0].url)
		assert.Equal(t, "https://agent.example.com", c.a2aAgents[0].config.AgentURL)
		assert.Equal(t, 5000, c.a2aAgents[0].config.TimeoutMs)
	})
}

func TestMCPServerBuilder_NewMethods(t *testing.T) {
	t.Run("WithWorkingDir sets working dir", func(t *testing.T) {
		cfg := NewMCPServer("test", "cmd").
			WithWorkingDir("/tmp/workdir").
			Build()

		assert.Equal(t, "/tmp/workdir", cfg.WorkingDir)
	})

	t.Run("WithTimeout sets timeout", func(t *testing.T) {
		cfg := NewMCPServer("test", "cmd").
			WithTimeout(10000).
			Build()

		assert.Equal(t, 10000, cfg.TimeoutMs)
	})

	t.Run("WithToolFilter sets tool filter", func(t *testing.T) {
		filter := &mcp.ToolFilter{
			Allowlist: []string{"read", "write"},
		}
		cfg := NewMCPServer("test", "cmd").
			WithToolFilter(filter).
			Build()

		require.NotNil(t, cfg.ToolFilter)
		assert.Equal(t, []string{"read", "write"}, cfg.ToolFilter.Allowlist)
	})

	t.Run("full builder chain with new methods", func(t *testing.T) {
		cfg := NewMCPServer("github", "npx", "@mcp/server-github").
			WithEnv("GITHUB_TOKEN", "tok").
			WithWorkingDir("/repos/myproject").
			WithTimeout(5000).
			WithToolFilter(&mcp.ToolFilter{Blocklist: []string{"dangerous_tool"}}).
			Build()

		assert.Equal(t, "github", cfg.Name)
		assert.Equal(t, "npx", cfg.Command)
		assert.Equal(t, []string{"@mcp/server-github"}, cfg.Args)
		assert.Equal(t, "tok", cfg.Env["GITHUB_TOKEN"])
		assert.Equal(t, "/repos/myproject", cfg.WorkingDir)
		assert.Equal(t, 5000, cfg.TimeoutMs)
		assert.Equal(t, []string{"dangerous_tool"}, cfg.ToolFilter.Blocklist)
	})
}
