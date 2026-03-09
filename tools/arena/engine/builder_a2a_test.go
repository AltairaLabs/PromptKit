package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	a2amock "github.com/AltairaLabs/PromptKit/runtime/a2a/mock"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

func TestResolveA2AAuth(t *testing.T) {
	t.Run("nil auth returns nil", func(t *testing.T) {
		assert.Nil(t, resolveA2AAuth(nil))
	})

	t.Run("empty token returns nil", func(t *testing.T) {
		auth := &config.A2AAuthConfig{Scheme: "Bearer"}
		assert.Nil(t, resolveA2AAuth(auth))
	})

	t.Run("token from config", func(t *testing.T) {
		auth := &config.A2AAuthConfig{Scheme: "Bearer", Token: "my-token"}
		opt := resolveA2AAuth(auth)
		assert.NotNil(t, opt)
	})

	t.Run("token from env", func(t *testing.T) {
		t.Setenv("TEST_A2A_TOKEN_RESOLVE", "env-token")
		auth := &config.A2AAuthConfig{Scheme: "ApiKey", TokenEnv: "TEST_A2A_TOKEN_RESOLVE"}
		opt := resolveA2AAuth(auth)
		assert.NotNil(t, opt)
	})

	t.Run("unset env returns nil", func(t *testing.T) {
		auth := &config.A2AAuthConfig{Scheme: "Bearer", TokenEnv: "UNSET_A2A_TOKEN_XXXXX"}
		assert.Nil(t, resolveA2AAuth(auth))
	})

	t.Run("empty scheme defaults to Bearer", func(t *testing.T) {
		auth := &config.A2AAuthConfig{Token: "tok"}
		opt := resolveA2AAuth(auth)
		assert.NotNil(t, opt)
	})
}

func TestResolveA2AHeadersEngine(t *testing.T) {
	t.Run("no headers returns nil", func(t *testing.T) {
		opt, err := resolveA2AHeaders(nil, nil)
		require.NoError(t, err)
		assert.Nil(t, opt)
	})

	t.Run("static headers only", func(t *testing.T) {
		opt, err := resolveA2AHeaders(map[string]string{"X-Tenant": "acme"}, nil)
		require.NoError(t, err)
		assert.NotNil(t, opt)
	})

	t.Run("env headers resolved", func(t *testing.T) {
		t.Setenv("TEST_A2A_HDR_VAL", "secret")
		opt, err := resolveA2AHeaders(nil, []string{"X-Key=TEST_A2A_HDR_VAL"})
		require.NoError(t, err)
		assert.NotNil(t, opt)
	})

	t.Run("unset env header skipped", func(t *testing.T) {
		opt, err := resolveA2AHeaders(nil, []string{"X-Key=UNSET_HDR_XXXX"})
		require.NoError(t, err)
		assert.Nil(t, opt)
	})

	t.Run("invalid format returns error", func(t *testing.T) {
		_, err := resolveA2AHeaders(nil, []string{"no-equals"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid headers_from_env format")
	})

	t.Run("mixed static and env", func(t *testing.T) {
		t.Setenv("TEST_A2A_MIX_HDR", "from-env")
		opt, err := resolveA2AHeaders(
			map[string]string{"X-Static": "val"},
			[]string{"X-Dynamic=TEST_A2A_MIX_HDR"},
		)
		require.NoError(t, err)
		assert.NotNil(t, opt)
	})
}

func TestBuildA2AMockConfig(t *testing.T) {
	t.Run("basic agent config", func(t *testing.T) {
		agentCfg := &config.A2AAgentConfig{
			Name: "test-agent",
			Card: config.A2ACardConfig{
				Name:        "Test Agent",
				Description: "A test agent",
				Skills: []config.A2ASkillConfig{
					{ID: "echo", Name: "Echo", Description: "Echo messages", Tags: []string{"utility"}},
				},
			},
		}
		mockCfg := buildA2AMockConfig(agentCfg)

		assert.Equal(t, "test-agent", mockCfg.Name)
		assert.Equal(t, "Test Agent", mockCfg.Card.Name)
		assert.Equal(t, "A test agent", mockCfg.Card.Description)
		require.Len(t, mockCfg.Card.Skills, 1)
		assert.Equal(t, "echo", mockCfg.Card.Skills[0].ID)
		assert.Equal(t, []string{"utility"}, mockCfg.Card.Skills[0].Tags)
	})

	t.Run("with responses and match rules", func(t *testing.T) {
		agentCfg := &config.A2AAgentConfig{
			Name: "echo-agent",
			Card: config.A2ACardConfig{Name: "Echo"},
			Responses: []config.A2AResponseRule{
				{
					Skill: "echo",
					Match: &config.A2AMatchConfig{Contains: "hello", Regex: "^hello"},
					Response: &config.A2AResponseConfig{
						Parts: []config.A2APartConfig{{Text: "Hello back!"}},
					},
				},
				{
					Skill: "echo",
					Error: "not found",
				},
			},
		}
		mockCfg := buildA2AMockConfig(agentCfg)

		require.Len(t, mockCfg.Responses, 2)
		assert.Equal(t, "echo", mockCfg.Responses[0].Skill)
		require.NotNil(t, mockCfg.Responses[0].Match)
		assert.Equal(t, "hello", mockCfg.Responses[0].Match.Contains)
		assert.Equal(t, "^hello", mockCfg.Responses[0].Match.Regex)
		require.NotNil(t, mockCfg.Responses[0].Response)
		require.Len(t, mockCfg.Responses[0].Response.Parts, 1)
		assert.Equal(t, "Hello back!", mockCfg.Responses[0].Response.Parts[0].Text)

		assert.Equal(t, "not found", mockCfg.Responses[1].Error)
		assert.Nil(t, mockCfg.Responses[1].Match)
		assert.Nil(t, mockCfg.Responses[1].Response)
	})
}

func TestPopulateConfigCard(t *testing.T) {
	t.Run("populates empty card from discovered card", func(t *testing.T) {
		agentCfg := &config.A2AAgentConfig{}
		card := &a2a.AgentCard{
			Name:        "Remote Agent",
			Description: "Discovered description",
			Skills: []a2a.AgentSkill{
				{ID: "s1", Name: "Skill One", Description: "First skill", Tags: []string{"tag1"}},
				{ID: "s2", Name: "Skill Two", Description: "Second skill"},
			},
		}
		populateConfigCard(agentCfg, card)

		assert.Equal(t, "Remote Agent", agentCfg.Card.Name)
		assert.Equal(t, "Discovered description", agentCfg.Card.Description)
		require.Len(t, agentCfg.Card.Skills, 2)
		assert.Equal(t, "s1", agentCfg.Card.Skills[0].ID)
		assert.Equal(t, "Skill One", agentCfg.Card.Skills[0].Name)
		assert.Equal(t, []string{"tag1"}, agentCfg.Card.Skills[0].Tags)
		assert.Equal(t, "s2", agentCfg.Card.Skills[1].ID)
	})

	t.Run("does not overwrite existing card fields", func(t *testing.T) {
		agentCfg := &config.A2AAgentConfig{
			Card: config.A2ACardConfig{
				Name:        "Custom Name",
				Description: "Custom Description",
				Skills:      []config.A2ASkillConfig{{ID: "custom"}},
			},
		}
		card := &a2a.AgentCard{
			Name:        "Discovered Name",
			Description: "Discovered Description",
			Skills:      []a2a.AgentSkill{{ID: "discovered"}},
		}
		populateConfigCard(agentCfg, card)

		assert.Equal(t, "Custom Name", agentCfg.Card.Name)
		assert.Equal(t, "Custom Description", agentCfg.Card.Description)
		require.Len(t, agentCfg.Card.Skills, 1)
		assert.Equal(t, "custom", agentCfg.Card.Skills[0].ID)
	})

	t.Run("empty discovered skills does not overwrite", func(t *testing.T) {
		agentCfg := &config.A2AAgentConfig{}
		card := &a2a.AgentCard{Name: "Agent", Skills: nil}
		populateConfigCard(agentCfg, card)

		assert.Equal(t, "Agent", agentCfg.Card.Name)
		assert.Empty(t, agentCfg.Card.Skills)
	})
}

func TestBuildRemoteA2AServer(t *testing.T) {
	t.Run("basic remote agent", func(t *testing.T) {
		agentCfg := &config.A2AAgentConfig{
			URL:  "https://agent.example.com/a2a",
			Name: "remote",
		}
		rs, err := buildRemoteA2AServer(agentCfg)

		require.NoError(t, err)
		assert.Equal(t, "https://agent.example.com/a2a", rs.url)
		assert.Empty(t, rs.clientOpts)
		assert.Nil(t, rs.auth)
		assert.Nil(t, rs.skillFilter)
	})

	t.Run("with auth token", func(t *testing.T) {
		agentCfg := &config.A2AAgentConfig{
			URL:  "https://agent.example.com/a2a",
			Auth: &config.A2AAuthConfig{Scheme: "Bearer", Token: "tok123"},
		}
		rs, err := buildRemoteA2AServer(agentCfg)

		require.NoError(t, err)
		assert.Len(t, rs.clientOpts, 1)
		require.NotNil(t, rs.auth)
		assert.Equal(t, "Bearer", rs.auth.Scheme)
		assert.Equal(t, "tok123", rs.auth.Token)
	})

	t.Run("with auth from env", func(t *testing.T) {
		t.Setenv("TEST_REMOTE_TOKEN", "env-tok")
		agentCfg := &config.A2AAgentConfig{
			URL:  "https://agent.example.com/a2a",
			Auth: &config.A2AAuthConfig{Scheme: "Bearer", TokenEnv: "TEST_REMOTE_TOKEN"},
		}
		rs, err := buildRemoteA2AServer(agentCfg)

		require.NoError(t, err)
		assert.Len(t, rs.clientOpts, 1)
		require.NotNil(t, rs.auth)
		assert.Equal(t, "env-tok", rs.auth.Token)
	})

	t.Run("with headers", func(t *testing.T) {
		agentCfg := &config.A2AAgentConfig{
			URL:     "https://agent.example.com/a2a",
			Headers: map[string]string{"X-Tenant": "acme"},
		}
		rs, err := buildRemoteA2AServer(agentCfg)

		require.NoError(t, err)
		assert.Len(t, rs.clientOpts, 1)
		assert.Equal(t, map[string]string{"X-Tenant": "acme"}, rs.headers)
	})

	t.Run("with skill filter", func(t *testing.T) {
		agentCfg := &config.A2AAgentConfig{
			URL: "https://agent.example.com/a2a",
			SkillFilter: &config.A2ASkillFilter{
				Allowlist: []string{"echo"},
				Blocklist: []string{"debug"},
			},
		}
		rs, err := buildRemoteA2AServer(agentCfg)

		require.NoError(t, err)
		require.NotNil(t, rs.skillFilter)
		assert.Equal(t, []string{"echo"}, rs.skillFilter.Allowlist)
		assert.Equal(t, []string{"debug"}, rs.skillFilter.Blocklist)
	})

	t.Run("with timeout", func(t *testing.T) {
		agentCfg := &config.A2AAgentConfig{
			URL:       "https://agent.example.com/a2a",
			TimeoutMs: 5000,
		}
		rs, err := buildRemoteA2AServer(agentCfg)

		require.NoError(t, err)
		assert.Equal(t, 5000, rs.timeoutMs)
	})

	t.Run("invalid headers_from_env returns error", func(t *testing.T) {
		agentCfg := &config.A2AAgentConfig{
			URL:            "https://agent.example.com/a2a",
			HeadersFromEnv: []string{"bad-format"},
		}
		_, err := buildRemoteA2AServer(agentCfg)
		assert.Error(t, err)
	})
}

func TestStartA2AServers_LocalMock(t *testing.T) {
	agentCfg := []config.A2AAgentConfig{
		{
			Name: "test-echo",
			Card: config.A2ACardConfig{
				Name:        "Echo",
				Description: "Echoes messages",
				Skills: []config.A2ASkillConfig{
					{ID: "echo", Name: "echo", Description: "Echo a message"},
				},
			},
		},
	}
	servers, cleanup, err := startA2AServers(agentCfg)
	require.NoError(t, err)
	defer cleanup()

	require.Len(t, servers, 1)
	assert.NotEmpty(t, servers[0].url)
	assert.NotNil(t, servers[0].server)
	assert.False(t, servers[0].isRemote)
}

func TestDiscoverAndRegisterA2ATools_LocalMock(t *testing.T) {
	// Start a local mock A2A server.
	card := a2a.AgentCard{
		Name:               "Test Echo Agent",
		Description:        "Echoes messages",
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		Skills: []a2a.AgentSkill{
			{ID: "echo", Name: "echo", Description: "Echo a message"},
		},
	}
	server := a2amock.NewA2AServer(&card)
	url, err := server.Start()
	require.NoError(t, err)
	defer server.Close()

	agents := []config.A2AAgentConfig{{Name: "echo"}}
	servers := []a2aRunningServer{{url: url, server: server}}

	registry := tools.NewRegistry()
	totalTools, err := discoverAndRegisterA2ATools(servers, registry, agents)

	require.NoError(t, err)
	assert.Greater(t, totalTools, 0)
}
