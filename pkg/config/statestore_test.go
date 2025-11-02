package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfig_GetStateStoreType_DefaultsToMemory(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		expected string
	}{
		{
			name: "nil StateStore config",
			config: &Config{
				StateStore: nil,
			},
			expected: "memory",
		},
		{
			name: "empty Type in StateStore config",
			config: &Config{
				StateStore: &StateStoreConfig{
					Type: "",
				},
			},
			expected: "memory",
		},
		{
			name: "explicit memory type",
			config: &Config{
				StateStore: &StateStoreConfig{
					Type: "memory",
				},
			},
			expected: "memory",
		},
		{
			name: "explicit redis type",
			config: &Config{
				StateStore: &StateStoreConfig{
					Type: "redis",
				},
			},
			expected: "redis",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetStateStoreType()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConfig_GetStateStoreConfig_DefaultsToMemory(t *testing.T) {
	t.Run("nil StateStore config returns default memory config", func(t *testing.T) {
		config := &Config{
			StateStore: nil,
		}
		result := config.GetStateStoreConfig()
		assert.NotNil(t, result)
		assert.Equal(t, "memory", result.Type)
	})

	t.Run("empty Type gets set to memory", func(t *testing.T) {
		config := &Config{
			StateStore: &StateStoreConfig{
				Type: "",
			},
		}
		result := config.GetStateStoreConfig()
		assert.NotNil(t, result)
		assert.Equal(t, "memory", result.Type)
	})

	t.Run("existing config is returned as-is", func(t *testing.T) {
		config := &Config{
			StateStore: &StateStoreConfig{
				Type: "redis",
				Redis: &RedisConfig{
					Address: "localhost:6379",
				},
			},
		}
		result := config.GetStateStoreConfig()
		assert.NotNil(t, result)
		assert.Equal(t, "redis", result.Type)
		assert.NotNil(t, result.Redis)
		assert.Equal(t, "localhost:6379", result.Redis.Address)
	})
}
