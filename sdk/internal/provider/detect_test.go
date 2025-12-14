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
