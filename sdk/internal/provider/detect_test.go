package provider

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectInfo(t *testing.T) {
	// Save and clear environment
	origOpenAI := os.Getenv("OPENAI_API_KEY")
	origAnthropic := os.Getenv("ANTHROPIC_API_KEY")
	origGoogle := os.Getenv("GOOGLE_API_KEY")
	origGemini := os.Getenv("GEMINI_API_KEY")
	defer func() {
		_ = os.Setenv("OPENAI_API_KEY", origOpenAI)
		_ = os.Setenv("ANTHROPIC_API_KEY", origAnthropic)
		_ = os.Setenv("GOOGLE_API_KEY", origGoogle)
		_ = os.Setenv("GEMINI_API_KEY", origGemini)
	}()

	t.Run("no keys set", func(t *testing.T) {
		_ = os.Unsetenv("OPENAI_API_KEY")
		_ = os.Unsetenv("ANTHROPIC_API_KEY")
		_ = os.Unsetenv("GOOGLE_API_KEY")
		_ = os.Unsetenv("GEMINI_API_KEY")

		info := detectInfo()
		assert.Nil(t, info)
	})

	t.Run("openai key set", func(t *testing.T) {
		_ = os.Unsetenv("OPENAI_API_KEY")
		_ = os.Unsetenv("ANTHROPIC_API_KEY")
		_ = os.Unsetenv("GOOGLE_API_KEY")
		_ = os.Unsetenv("GEMINI_API_KEY")
		_ = os.Setenv("OPENAI_API_KEY", "sk-test-key")

		info := detectInfo()
		require.NotNil(t, info)
		assert.Equal(t, "openai", info.Name)
		assert.Equal(t, "sk-test-key", info.APIKey)
		assert.Equal(t, "gpt-4o", info.Model)
	})

	t.Run("anthropic key set", func(t *testing.T) {
		_ = os.Unsetenv("OPENAI_API_KEY")
		_ = os.Unsetenv("ANTHROPIC_API_KEY")
		_ = os.Unsetenv("GOOGLE_API_KEY")
		_ = os.Unsetenv("GEMINI_API_KEY")
		_ = os.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")

		info := detectInfo()
		require.NotNil(t, info)
		assert.Equal(t, "anthropic", info.Name)
		assert.Equal(t, "sk-ant-test", info.APIKey)
		assert.Contains(t, info.Model, "claude")
	})

	t.Run("google key set", func(t *testing.T) {
		_ = os.Unsetenv("OPENAI_API_KEY")
		_ = os.Unsetenv("ANTHROPIC_API_KEY")
		_ = os.Unsetenv("GOOGLE_API_KEY")
		_ = os.Unsetenv("GEMINI_API_KEY")
		_ = os.Setenv("GOOGLE_API_KEY", "google-key")

		info := detectInfo()
		require.NotNil(t, info)
		assert.Equal(t, "gemini", info.Name)
		assert.Equal(t, "google-key", info.APIKey)
		assert.Contains(t, info.Model, "gemini")
	})

	t.Run("gemini key set", func(t *testing.T) {
		_ = os.Unsetenv("OPENAI_API_KEY")
		_ = os.Unsetenv("ANTHROPIC_API_KEY")
		_ = os.Unsetenv("GOOGLE_API_KEY")
		_ = os.Unsetenv("GEMINI_API_KEY")
		_ = os.Setenv("GEMINI_API_KEY", "gemini-key")

		info := detectInfo()
		require.NotNil(t, info)
		assert.Equal(t, "gemini", info.Name)
		assert.Equal(t, "gemini-key", info.APIKey)
	})

	t.Run("openai takes priority", func(t *testing.T) {
		_ = os.Setenv("OPENAI_API_KEY", "openai-key")
		_ = os.Setenv("ANTHROPIC_API_KEY", "anthropic-key")
		_ = os.Setenv("GOOGLE_API_KEY", "google-key")

		info := detectInfo()
		require.NotNil(t, info)
		assert.Equal(t, "openai", info.Name)
	})
}

func TestDetect(t *testing.T) {
	// Save and clear environment
	origOpenAI := os.Getenv("OPENAI_API_KEY")
	origAnthropic := os.Getenv("ANTHROPIC_API_KEY")
	origGoogle := os.Getenv("GOOGLE_API_KEY")
	defer func() {
		_ = os.Setenv("OPENAI_API_KEY", origOpenAI)
		_ = os.Setenv("ANTHROPIC_API_KEY", origAnthropic)
		_ = os.Setenv("GOOGLE_API_KEY", origGoogle)
	}()

	t.Run("no keys and no apiKey", func(t *testing.T) {
		_ = os.Unsetenv("OPENAI_API_KEY")
		_ = os.Unsetenv("ANTHROPIC_API_KEY")
		_ = os.Unsetenv("GOOGLE_API_KEY")
		_ = os.Unsetenv("GEMINI_API_KEY")

		_, err := Detect("", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no provider detected")
	})

	t.Run("apiKey provided defaults to openai", func(t *testing.T) {
		_ = os.Unsetenv("OPENAI_API_KEY")
		_ = os.Unsetenv("ANTHROPIC_API_KEY")
		_ = os.Unsetenv("GOOGLE_API_KEY")
		_ = os.Unsetenv("GEMINI_API_KEY")

		prov, err := Detect("my-api-key", "")
		require.NoError(t, err)
		assert.NotNil(t, prov)
		assert.Equal(t, "openai", prov.ID())
	})

	t.Run("model override", func(t *testing.T) {
		_ = os.Setenv("OPENAI_API_KEY", "test-key")

		prov, err := Detect("", "gpt-4-turbo")
		require.NoError(t, err)
		assert.NotNil(t, prov)
	})

	t.Run("openai provider", func(t *testing.T) {
		_ = os.Unsetenv("ANTHROPIC_API_KEY")
		_ = os.Unsetenv("GOOGLE_API_KEY")
		_ = os.Setenv("OPENAI_API_KEY", "sk-test")

		prov, err := Detect("", "")
		require.NoError(t, err)
		assert.NotNil(t, prov)
		assert.Equal(t, "openai", prov.ID())
	})

	t.Run("anthropic provider", func(t *testing.T) {
		_ = os.Unsetenv("OPENAI_API_KEY")
		_ = os.Unsetenv("GOOGLE_API_KEY")
		_ = os.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")

		prov, err := Detect("", "")
		require.NoError(t, err)
		assert.NotNil(t, prov)
		assert.Equal(t, "anthropic", prov.ID())
	})

	t.Run("gemini provider", func(t *testing.T) {
		_ = os.Unsetenv("OPENAI_API_KEY")
		_ = os.Unsetenv("ANTHROPIC_API_KEY")
		_ = os.Setenv("GOOGLE_API_KEY", "google-key")

		prov, err := Detect("", "")
		require.NoError(t, err)
		assert.NotNil(t, prov)
		assert.Equal(t, "gemini", prov.ID())
	})
}

func TestCreateProvider(t *testing.T) {
	t.Run("openai", func(t *testing.T) {
		info := &Info{Name: "openai", APIKey: "test", Model: "gpt-4o"}
		prov, err := createProvider(info)
		require.NoError(t, err)
		assert.Equal(t, "openai", prov.ID())
	})

	t.Run("anthropic", func(t *testing.T) {
		info := &Info{Name: "anthropic", APIKey: "test", Model: "claude-3-opus"}
		prov, err := createProvider(info)
		require.NoError(t, err)
		assert.Equal(t, "anthropic", prov.ID())
	})

	t.Run("gemini", func(t *testing.T) {
		info := &Info{Name: "gemini", APIKey: "test", Model: "gemini-1.5-pro"}
		prov, err := createProvider(info)
		require.NoError(t, err)
		assert.Equal(t, "gemini", prov.ID())
	})

	t.Run("google alias", func(t *testing.T) {
		info := &Info{Name: "google", APIKey: "test", Model: "gemini-1.5-pro"}
		prov, err := createProvider(info)
		require.NoError(t, err)
		assert.Equal(t, "gemini", prov.ID())
	})

	t.Run("unsupported provider", func(t *testing.T) {
		info := &Info{Name: "unknown", APIKey: "test", Model: "model"}
		_, err := createProvider(info)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported provider")
	})

	t.Run("case insensitive", func(t *testing.T) {
		info := &Info{Name: "OpenAI", APIKey: "test", Model: "gpt-4o"}
		prov, err := createProvider(info)
		require.NoError(t, err)
		assert.Equal(t, "openai", prov.ID())
	})
}

func TestDefaultConstants(t *testing.T) {
	assert.Equal(t, 0.7, defaultTemperature)
	assert.Equal(t, 1.0, defaultTopP)
	assert.Equal(t, 4096, defaultMaxTokens)
}

func TestInferProviderFromModel(t *testing.T) {
	tests := []struct {
		model    string
		expected string
	}{
		// Gemini models
		{"gemini-2.0-flash-exp", "gemini"},
		{"gemini-1.5-pro", "gemini"},
		{"Gemini-Pro", "gemini"},

		// OpenAI models
		{"gpt-4o", "openai"},
		{"gpt-4-turbo", "openai"},
		{"GPT-3.5-Turbo", "openai"},
		{"o1-preview", "openai"},
		{"o1-mini", "openai"},
		{"o3-mini", "openai"},

		// Claude models
		{"claude-3-opus", "anthropic"},
		{"claude-sonnet-4-20250514", "anthropic"},
		{"Claude-3-Haiku", "anthropic"},

		// Unknown models
		{"unknown-model", ""},
		{"llama-3", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			result := inferProviderFromModel(tt.model)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDetectInfoForProvider(t *testing.T) {
	// Save and restore environment
	origOpenAI := os.Getenv("OPENAI_API_KEY")
	origAnthropic := os.Getenv("ANTHROPIC_API_KEY")
	origGoogle := os.Getenv("GOOGLE_API_KEY")
	origGemini := os.Getenv("GEMINI_API_KEY")
	defer func() {
		_ = os.Setenv("OPENAI_API_KEY", origOpenAI)
		_ = os.Setenv("ANTHROPIC_API_KEY", origAnthropic)
		_ = os.Setenv("GOOGLE_API_KEY", origGoogle)
		_ = os.Setenv("GEMINI_API_KEY", origGemini)
	}()

	t.Run("gemini with GEMINI_API_KEY", func(t *testing.T) {
		_ = os.Unsetenv("GOOGLE_API_KEY")
		_ = os.Setenv("GEMINI_API_KEY", "test-gemini-key")

		info := detectInfoForProvider("gemini")
		require.NotNil(t, info)
		assert.Equal(t, "gemini", info.Name)
		assert.Equal(t, "test-gemini-key", info.APIKey)
	})

	t.Run("gemini with GOOGLE_API_KEY", func(t *testing.T) {
		_ = os.Unsetenv("GEMINI_API_KEY")
		_ = os.Setenv("GOOGLE_API_KEY", "test-google-key")

		info := detectInfoForProvider("gemini")
		require.NotNil(t, info)
		assert.Equal(t, "gemini", info.Name)
		assert.Equal(t, "test-google-key", info.APIKey)
	})

	t.Run("openai with key", func(t *testing.T) {
		_ = os.Setenv("OPENAI_API_KEY", "test-openai-key")

		info := detectInfoForProvider("openai")
		require.NotNil(t, info)
		assert.Equal(t, "openai", info.Name)
		assert.Equal(t, "test-openai-key", info.APIKey)
	})

	t.Run("unknown provider", func(t *testing.T) {
		info := detectInfoForProvider("unknown")
		assert.Nil(t, info)
	})

	t.Run("provider without key set", func(t *testing.T) {
		_ = os.Unsetenv("ANTHROPIC_API_KEY")

		info := detectInfoForProvider("anthropic")
		assert.Nil(t, info)
	})
}

func TestDetect_ModelTakesPriority(t *testing.T) {
	// Save and restore environment
	origOpenAI := os.Getenv("OPENAI_API_KEY")
	origAnthropic := os.Getenv("ANTHROPIC_API_KEY")
	origGoogle := os.Getenv("GOOGLE_API_KEY")
	origGemini := os.Getenv("GEMINI_API_KEY")
	defer func() {
		_ = os.Setenv("OPENAI_API_KEY", origOpenAI)
		_ = os.Setenv("ANTHROPIC_API_KEY", origAnthropic)
		_ = os.Setenv("GOOGLE_API_KEY", origGoogle)
		_ = os.Setenv("GEMINI_API_KEY", origGemini)
	}()

	t.Run("gemini model selects gemini despite openai key first", func(t *testing.T) {
		// Set both keys - OpenAI would be detected first by env var order
		_ = os.Setenv("OPENAI_API_KEY", "openai-key")
		_ = os.Setenv("GEMINI_API_KEY", "gemini-key")
		_ = os.Unsetenv("ANTHROPIC_API_KEY")
		_ = os.Unsetenv("GOOGLE_API_KEY")

		// Specify gemini model - should select gemini provider
		prov, err := Detect("", "gemini-2.0-flash-exp")
		require.NoError(t, err)
		assert.Equal(t, "gemini", prov.ID())
	})

	t.Run("claude model selects anthropic despite openai key first", func(t *testing.T) {
		_ = os.Setenv("OPENAI_API_KEY", "openai-key")
		_ = os.Setenv("ANTHROPIC_API_KEY", "anthropic-key")
		_ = os.Unsetenv("GEMINI_API_KEY")
		_ = os.Unsetenv("GOOGLE_API_KEY")

		prov, err := Detect("", "claude-3-opus")
		require.NoError(t, err)
		assert.Equal(t, "anthropic", prov.ID())
	})

	t.Run("openai model still works", func(t *testing.T) {
		_ = os.Setenv("OPENAI_API_KEY", "openai-key")
		_ = os.Setenv("GEMINI_API_KEY", "gemini-key")

		prov, err := Detect("", "gpt-4o")
		require.NoError(t, err)
		assert.Equal(t, "openai", prov.ID())
	})

	t.Run("unknown model falls back to env var detection", func(t *testing.T) {
		_ = os.Setenv("OPENAI_API_KEY", "openai-key")
		_ = os.Setenv("GEMINI_API_KEY", "gemini-key")

		// Unknown model should fall back to env var order (OpenAI first)
		prov, err := Detect("", "unknown-model")
		require.NoError(t, err)
		assert.Equal(t, "openai", prov.ID())
	})

	t.Run("model inference without matching key falls back", func(t *testing.T) {
		// Only OpenAI key set, but asking for Gemini model
		_ = os.Setenv("OPENAI_API_KEY", "openai-key")
		_ = os.Unsetenv("GEMINI_API_KEY")
		_ = os.Unsetenv("GOOGLE_API_KEY")
		_ = os.Unsetenv("ANTHROPIC_API_KEY")

		// Should fall back to OpenAI since no Gemini key available
		prov, err := Detect("", "gemini-2.0-flash-exp")
		require.NoError(t, err)
		assert.Equal(t, "openai", prov.ID())
	})
}
