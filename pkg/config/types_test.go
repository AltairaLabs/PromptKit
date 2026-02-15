package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestDeployConfig_ParseFromYAML(t *testing.T) {
	yamlData := `
providers: []
deploy:
  provider: agentcore
  config:
    region: us-east-1
    instance_type: t3.medium
defaults:
  temperature: 0.7
`
	var cfg Config
	err := yaml.Unmarshal([]byte(yamlData), &cfg)
	require.NoError(t, err)
	require.NotNil(t, cfg.Deploy)
	assert.Equal(t, "agentcore", cfg.Deploy.Provider)
	assert.Equal(t, "us-east-1", cfg.Deploy.Config["region"])
	assert.Equal(t, "t3.medium", cfg.Deploy.Config["instance_type"])
}

func TestDeployConfig_MultiEnvironment(t *testing.T) {
	yamlData := `
providers: []
deploy:
  provider: ecs
  config:
    cluster: default
  environments:
    staging:
      config:
        cluster: staging-cluster
        replicas: 1
    production:
      config:
        cluster: prod-cluster
        replicas: 3
defaults:
  temperature: 0.7
`
	var cfg Config
	err := yaml.Unmarshal([]byte(yamlData), &cfg)
	require.NoError(t, err)
	require.NotNil(t, cfg.Deploy)
	assert.Equal(t, "ecs", cfg.Deploy.Provider)
	assert.Equal(t, "default", cfg.Deploy.Config["cluster"])

	require.Len(t, cfg.Deploy.Environments, 2)

	staging := cfg.Deploy.Environments["staging"]
	require.NotNil(t, staging)
	assert.Equal(t, "staging-cluster", staging.Config["cluster"])
	assert.Equal(t, 1, staging.Config["replicas"])

	production := cfg.Deploy.Environments["production"]
	require.NotNil(t, production)
	assert.Equal(t, "prod-cluster", production.Config["cluster"])
	assert.Equal(t, 3, production.Config["replicas"])
}

func TestDeployConfig_BackwardCompatibility(t *testing.T) {
	yamlData := `
providers: []
defaults:
  temperature: 0.7
  max_tokens: 1024
`
	var cfg Config
	err := yaml.Unmarshal([]byte(yamlData), &cfg)
	require.NoError(t, err)
	assert.Nil(t, cfg.Deploy)
	assert.Equal(t, float32(0.7), cfg.Defaults.Temperature)
	assert.Equal(t, 1024, cfg.Defaults.MaxTokens)
}
