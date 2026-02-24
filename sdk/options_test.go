package sdk

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestWithModel(t *testing.T) {
	opt := WithModel("gpt-4")
	assert.NotNil(t, opt)

	cfg := &config{}
	err := opt(cfg)
	assert.NoError(t, err)
	assert.Equal(t, "gpt-4", cfg.model)
}

func TestWithAPIKey(t *testing.T) {
	opt := WithAPIKey("test-key")
	assert.NotNil(t, opt)

	cfg := &config{}
	err := opt(cfg)
	assert.NoError(t, err)
	assert.Equal(t, "test-key", cfg.apiKey)
}

func TestWithConversationID(t *testing.T) {
	opt := WithConversationID("conv-123")
	assert.NotNil(t, opt)

	cfg := &config{}
	err := opt(cfg)
	assert.NoError(t, err)
	assert.Equal(t, "conv-123", cfg.conversationID)
}

func TestWithTokenBudget(t *testing.T) {
	opt := WithTokenBudget(1000)
	assert.NotNil(t, opt)

	cfg := &config{}
	err := opt(cfg)
	assert.NoError(t, err)
	assert.Equal(t, 1000, cfg.tokenBudget)
}

func TestWithTruncation(t *testing.T) {
	tests := []struct {
		name     string
		strategy string
		want     string
	}{
		{"sliding", "sliding", "sliding"},
		{"summarize", "summarize", "summarize"},
		{"none", "none", "none"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opt := WithTruncation(tt.strategy)
			cfg := &config{}
			err := opt(cfg)
			assert.NoError(t, err)
			assert.Equal(t, tt.want, cfg.truncationStrategy)
		})
	}
}

func TestWithRelevanceTruncation(t *testing.T) {
	t.Run("sets strategy to relevance", func(t *testing.T) {
		opt := WithRelevanceTruncation(&RelevanceConfig{
			MinRecentMessages:   5,
			SimilarityThreshold: 0.3,
			QuerySource:         "last_user",
		})
		cfg := &config{}
		err := opt(cfg)
		assert.NoError(t, err)
		assert.Equal(t, "relevance", cfg.truncationStrategy)
		assert.NotNil(t, cfg.relevanceConfig)
		assert.Equal(t, 5, cfg.relevanceConfig.MinRecentMessages)
		assert.Equal(t, 0.3, cfg.relevanceConfig.SimilarityThreshold)
		assert.Equal(t, "last_user", cfg.relevanceConfig.QuerySource)
	})

	t.Run("with nil config", func(t *testing.T) {
		opt := WithRelevanceTruncation(nil)
		cfg := &config{}
		err := opt(cfg)
		assert.NoError(t, err)
		assert.Equal(t, "relevance", cfg.truncationStrategy)
		assert.Nil(t, cfg.relevanceConfig)
	})
}

func TestWithProviderHook(t *testing.T) {
	hook := &testProviderHook{name: "test-hook"}
	opt := WithProviderHook(hook)
	cfg := &config{}
	err := opt(cfg)
	assert.NoError(t, err)
	require.Len(t, cfg.providerHooks, 1)
	assert.Equal(t, "test-hook", cfg.providerHooks[0].Name())
}

func TestWithToolHook(t *testing.T) {
	hook := &testToolHook{name: "tool-hook"}
	opt := WithToolHook(hook)
	cfg := &config{}
	err := opt(cfg)
	assert.NoError(t, err)
	require.Len(t, cfg.toolHooks, 1)
	assert.Equal(t, "tool-hook", cfg.toolHooks[0].Name())
}

func TestWithSessionHook(t *testing.T) {
	hook := &testSessionHook{name: "session-hook"}
	opt := WithSessionHook(hook)
	cfg := &config{}
	err := opt(cfg)
	assert.NoError(t, err)
	require.Len(t, cfg.sessionHooks, 1)
	assert.Equal(t, "session-hook", cfg.sessionHooks[0].Name())
}

func TestBuildHookRegistry(t *testing.T) {
	t.Run("returns nil when no hooks", func(t *testing.T) {
		cfg := &config{}
		assert.Nil(t, cfg.buildHookRegistry())
	})

	t.Run("builds registry with all hook types", func(t *testing.T) {
		cfg := &config{
			providerHooks: []hooks.ProviderHook{&testProviderHook{name: "p1"}},
			toolHooks:     []hooks.ToolHook{&testToolHook{name: "t1"}},
			sessionHooks:  []hooks.SessionHook{&testSessionHook{name: "s1"}},
		}
		reg := cfg.buildHookRegistry()
		assert.NotNil(t, reg)
		assert.False(t, reg.IsEmpty())
	})
}

func TestSendOptions(t *testing.T) {
	t.Run("WithImageFile", func(t *testing.T) {
		opt := WithImageFile("/path/to/image.png")
		assert.NotNil(t, opt)

		cfg := &sendConfig{}
		err := opt(cfg)
		assert.NoError(t, err)
		assert.Equal(t, 1, len(cfg.parts))
	})

	t.Run("WithImageURL", func(t *testing.T) {
		opt := WithImageURL("https://example.com/image.png")
		assert.NotNil(t, opt)

		cfg := &sendConfig{}
		err := opt(cfg)
		assert.NoError(t, err)
		assert.Equal(t, 1, len(cfg.parts))
	})

	t.Run("WithImageData", func(t *testing.T) {
		data := []byte("fake image data")
		opt := WithImageData(data, "image/png")
		assert.NotNil(t, opt)

		cfg := &sendConfig{}
		err := opt(cfg)
		assert.NoError(t, err)
		assert.Equal(t, 1, len(cfg.parts))
	})

	t.Run("WithAudioFile", func(t *testing.T) {
		opt := WithAudioFile("/path/to/audio.mp3")
		assert.NotNil(t, opt)

		cfg := &sendConfig{}
		err := opt(cfg)
		assert.NoError(t, err)
		assert.Equal(t, 1, len(cfg.parts))
	})

	t.Run("WithAudioData", func(t *testing.T) {
		data := []byte("fake audio data")
		opt := WithAudioData(data, "audio/mp3")
		assert.NotNil(t, opt)

		cfg := &sendConfig{}
		err := opt(cfg)
		assert.NoError(t, err)
		assert.Equal(t, 1, len(cfg.parts))
	})

	t.Run("WithVideoFile", func(t *testing.T) {
		opt := WithVideoFile("/path/to/video.mp4")
		assert.NotNil(t, opt)

		cfg := &sendConfig{}
		err := opt(cfg)
		assert.NoError(t, err)
		assert.Equal(t, 1, len(cfg.parts))
	})

	t.Run("WithVideoData", func(t *testing.T) {
		data := []byte("fake video data")
		opt := WithVideoData(data, "video/mp4")
		assert.NotNil(t, opt)

		cfg := &sendConfig{}
		err := opt(cfg)
		assert.NoError(t, err)
		assert.Equal(t, 1, len(cfg.parts))
	})

	t.Run("WithFile", func(t *testing.T) {
		opt := WithFile("doc.pdf", []byte("pdf content"))
		assert.NotNil(t, opt)

		cfg := &sendConfig{}
		err := opt(cfg)
		assert.NoError(t, err)
		assert.Equal(t, 1, len(cfg.parts))
	})

	t.Run("WithDocumentFile", func(t *testing.T) {
		opt := WithDocumentFile("/path/to/document.pdf")
		assert.NotNil(t, opt)

		cfg := &sendConfig{}
		err := opt(cfg)
		assert.NoError(t, err)
		assert.Equal(t, 1, len(cfg.parts))
	})

	t.Run("WithDocumentData", func(t *testing.T) {
		data := []byte("fake pdf data")
		opt := WithDocumentData(data, types.MIMETypePDF)
		assert.NotNil(t, opt)

		cfg := &sendConfig{}
		err := opt(cfg)
		assert.NoError(t, err)
		assert.Equal(t, 1, len(cfg.parts))
	})

	t.Run("multiple parts", func(t *testing.T) {
		cfg := &sendConfig{}
		_ = WithImageFile("/image1.png")(cfg)
		_ = WithImageFile("/image2.png")(cfg)
		_ = WithAudioFile("/audio.mp3")(cfg)

		assert.Equal(t, 3, len(cfg.parts))
	})
}

func TestWithStateStore(t *testing.T) {
	opt := WithStateStore(nil)
	assert.NotNil(t, opt)

	cfg := &config{}
	err := opt(cfg)
	assert.NoError(t, err)
}

func TestWithProvider(t *testing.T) {
	opt := WithProvider(nil)
	assert.NotNil(t, opt)

	cfg := &config{}
	err := opt(cfg)
	assert.NoError(t, err)
}

func TestWithToolRegistry(t *testing.T) {
	opt := WithToolRegistry(nil)
	assert.NotNil(t, opt)

	cfg := &config{}
	err := opt(cfg)
	assert.NoError(t, err)
}

func TestWithEventBus(t *testing.T) {
	opt := WithEventBus(nil)
	assert.NotNil(t, opt)

	cfg := &config{}
	err := opt(cfg)
	assert.NoError(t, err)
}

func TestWithImageFileWithDetail(t *testing.T) {
	detail := "high"
	opt := WithImageFile("/path/to/image.png", &detail)
	assert.NotNil(t, opt)

	cfg := &sendConfig{}
	err := opt(cfg)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(cfg.parts))
}

func TestWithImageURLWithDetail(t *testing.T) {
	detail := "low"
	opt := WithImageURL("https://example.com/image.png", &detail)
	assert.NotNil(t, opt)

	cfg := &sendConfig{}
	err := opt(cfg)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(cfg.parts))
}

func TestWithImageDataWithDetail(t *testing.T) {
	detail := "auto"
	data := []byte("fake image data")
	opt := WithImageData(data, "image/png", &detail)
	assert.NotNil(t, opt)

	cfg := &sendConfig{}
	err := opt(cfg)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(cfg.parts))
}

func TestWithVariableProvider(t *testing.T) {
	// Create a mock provider
	provider := &mockVariableProvider{name: "test"}

	opt := WithVariableProvider(provider)
	assert.NotNil(t, opt)

	cfg := &config{}
	err := opt(cfg)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(cfg.variableProviders))
	assert.Equal(t, "test", cfg.variableProviders[0].Name())
}

func TestWithVariableProviderMultiple(t *testing.T) {
	provider1 := &mockVariableProvider{name: "time"}
	provider2 := &mockVariableProvider{name: "state"}

	cfg := &config{}
	_ = WithVariableProvider(provider1)(cfg)
	_ = WithVariableProvider(provider2)(cfg)

	assert.Equal(t, 2, len(cfg.variableProviders))
	assert.Equal(t, "time", cfg.variableProviders[0].Name())
	assert.Equal(t, "state", cfg.variableProviders[1].Name())
}

func TestWithTTS(t *testing.T) {
	service := &mockTTSService{name: "openai"}

	opt := WithTTS(service)
	assert.NotNil(t, opt)

	cfg := &config{}
	err := opt(cfg)
	assert.NoError(t, err)
	assert.NotNil(t, cfg.ttsService)
	assert.Equal(t, "openai", cfg.ttsService.Name())
}

func TestWithTurnDetector(t *testing.T) {
	detector := &mockTurnDetector{name: "silence"}

	opt := WithTurnDetector(detector)
	assert.NotNil(t, opt)

	cfg := &config{}
	err := opt(cfg)
	assert.NoError(t, err)
	assert.NotNil(t, cfg.turnDetector)
	assert.Equal(t, "silence", cfg.turnDetector.Name())
}

// Mock types for testing

type mockVariableProvider struct {
	name string
}

func (m *mockVariableProvider) Name() string {
	return m.name
}

func (m *mockVariableProvider) Provide(_ context.Context) (map[string]string, error) {
	return nil, nil
}

type mockTTSService struct {
	name string
}

func (m *mockTTSService) Name() string {
	return m.name
}

func (m *mockTTSService) Synthesize(_ context.Context, _ string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
	return nil, nil
}

func (m *mockTTSService) SupportedVoices() []tts.Voice {
	return nil
}

func (m *mockTTSService) SupportedFormats() []tts.AudioFormat {
	return nil
}

type mockTurnDetector struct {
	name string
}

func (m *mockTurnDetector) Name() string {
	return m.name
}

func (m *mockTurnDetector) ProcessAudio(_ context.Context, _ []byte) (bool, error) {
	return false, nil
}

func (m *mockTurnDetector) ProcessVADState(_ context.Context, _ audio.VADState) (bool, error) {
	return false, nil
}

func (m *mockTurnDetector) IsUserSpeaking() bool {
	return false
}

func (m *mockTurnDetector) Reset() {}

func TestWithImagePreprocessing(t *testing.T) {
	t.Run("with custom config", func(t *testing.T) {
		cfg := &stage.ImagePreprocessConfig{
			Resize: stage.ImageResizeStageConfig{
				MaxWidth:  1024,
				MaxHeight: 768,
				Quality:   90,
			},
			EnableResize: true,
		}

		opt := WithImagePreprocessing(cfg)
		assert.NotNil(t, opt)

		c := &config{}
		err := opt(c)
		assert.NoError(t, err)
		assert.NotNil(t, c.imagePreprocessConfig)
		assert.Equal(t, 1024, c.imagePreprocessConfig.Resize.MaxWidth)
		assert.Equal(t, 768, c.imagePreprocessConfig.Resize.MaxHeight)
		assert.Equal(t, 90, c.imagePreprocessConfig.Resize.Quality)
	})

	t.Run("with nil config uses defaults", func(t *testing.T) {
		opt := WithImagePreprocessing(nil)
		assert.NotNil(t, opt)

		c := &config{}
		err := opt(c)
		assert.NoError(t, err)
		assert.NotNil(t, c.imagePreprocessConfig)
	})
}

func TestWithAutoResize(t *testing.T) {
	opt := WithAutoResize(1280, 720)
	assert.NotNil(t, opt)

	c := &config{}
	err := opt(c)
	assert.NoError(t, err)
	assert.NotNil(t, c.imagePreprocessConfig)
	assert.Equal(t, 1280, c.imagePreprocessConfig.Resize.MaxWidth)
	assert.Equal(t, 720, c.imagePreprocessConfig.Resize.MaxHeight)
}

func TestDefaultVideoStreamConfig(t *testing.T) {
	cfg := DefaultVideoStreamConfig()
	assert.NotNil(t, cfg)
	assert.Equal(t, 1.0, cfg.TargetFPS)
	assert.Equal(t, 0, cfg.MaxWidth)
	assert.Equal(t, 0, cfg.MaxHeight)
	assert.Equal(t, 85, cfg.Quality)
	assert.True(t, cfg.EnableResize)
}

func TestWithStreamingVideo(t *testing.T) {
	t.Run("with custom config", func(t *testing.T) {
		cfg := &VideoStreamConfig{
			TargetFPS:    2.5,
			MaxWidth:     1920,
			MaxHeight:    1080,
			Quality:      75,
			EnableResize: false,
		}

		opt := WithStreamingVideo(cfg)
		assert.NotNil(t, opt)

		c := &config{}
		err := opt(c)
		assert.NoError(t, err)
		assert.NotNil(t, c.videoStreamConfig)
		assert.Equal(t, 2.5, c.videoStreamConfig.TargetFPS)
		assert.Equal(t, 1920, c.videoStreamConfig.MaxWidth)
		assert.Equal(t, 1080, c.videoStreamConfig.MaxHeight)
		assert.Equal(t, 75, c.videoStreamConfig.Quality)
		assert.False(t, c.videoStreamConfig.EnableResize)
	})

	t.Run("with nil config uses defaults", func(t *testing.T) {
		opt := WithStreamingVideo(nil)
		assert.NotNil(t, opt)

		c := &config{}
		err := opt(c)
		assert.NoError(t, err)
		assert.NotNil(t, c.videoStreamConfig)
		assert.Equal(t, 1.0, c.videoStreamConfig.TargetFPS)
		assert.Equal(t, 85, c.videoStreamConfig.Quality)
		assert.True(t, c.videoStreamConfig.EnableResize)
	})
}

func TestWithEventStore(t *testing.T) {
	opt := WithEventStore(nil)
	assert.NotNil(t, opt)

	cfg := &config{}
	err := opt(cfg)
	assert.NoError(t, err)
}

func TestWithVariables(t *testing.T) {
	vars := map[string]string{"key1": "value1", "key2": "value2"}
	opt := WithVariables(vars)
	assert.NotNil(t, opt)

	cfg := &config{}
	err := opt(cfg)
	assert.NoError(t, err)
	assert.Equal(t, "value1", cfg.initialVariables["key1"])
	assert.Equal(t, "value2", cfg.initialVariables["key2"])
}

func TestWithStreamingConfig(t *testing.T) {
	opt := WithStreamingConfig(nil)
	assert.NotNil(t, opt)

	cfg := &config{}
	err := opt(cfg)
	assert.NoError(t, err)
}

func TestMCPServerBuilderWithArgs(t *testing.T) {
	builder := NewMCPServer("test", "cmd", "arg1")
	builder.WithArgs("arg2", "arg3")
	config := builder.Build()

	assert.Equal(t, "test", config.Name)
	assert.Equal(t, "cmd", config.Command)
	assert.Contains(t, config.Args, "arg1")
	assert.Contains(t, config.Args, "arg2")
	assert.Contains(t, config.Args, "arg3")
}

func TestDefaultVADModeConfig(t *testing.T) {
	cfg := DefaultVADModeConfig()
	assert.NotNil(t, cfg)
	assert.Equal(t, 800000000, int(cfg.SilenceDuration)) // 800ms in nanoseconds
	assert.Equal(t, 16000, cfg.SampleRate)
	assert.Equal(t, "en", cfg.Language)
	assert.Equal(t, "alloy", cfg.Voice)
	assert.Equal(t, 1.0, cfg.Speed)
}

func TestWithVADMode(t *testing.T) {
	opt := WithVADMode(nil, nil, nil)
	assert.NotNil(t, opt)

	cfg := &config{}
	err := opt(cfg)
	assert.NoError(t, err)
	assert.NotNil(t, cfg.vadModeConfig)
}

func TestVADModeConfigConversions(t *testing.T) {
	cfg := DefaultVADModeConfig()

	// Test toAudioTurnConfig
	turnCfg := cfg.toAudioTurnConfig(nil)
	assert.Equal(t, cfg.SilenceDuration, turnCfg.SilenceDuration)
	assert.Equal(t, cfg.SampleRate, turnCfg.SampleRate)

	// Test toSTTStageConfig
	sttCfg := cfg.toSTTStageConfig()
	assert.Equal(t, cfg.Language, sttCfg.Language)

	// Test toTTSStageConfig
	ttsCfg := cfg.toTTSStageConfig(nil)
	assert.Equal(t, cfg.Voice, ttsCfg.Voice)
	assert.Equal(t, cfg.Speed, ttsCfg.Speed)
}

// Credential option tests

func TestWithCredentialAPIKey(t *testing.T) {
	opt := WithCredentialAPIKey("sk-test-key")
	assert.NotNil(t, opt)

	cfg := &credentialConfig{}
	opt.applyCredential(cfg)
	assert.Equal(t, "sk-test-key", cfg.apiKey)
}

func TestWithCredentialFile(t *testing.T) {
	opt := WithCredentialFile("/path/to/credential.txt")
	assert.NotNil(t, opt)

	cfg := &credentialConfig{}
	opt.applyCredential(cfg)
	assert.Equal(t, "/path/to/credential.txt", cfg.credentialFile)
}

func TestWithCredentialEnv(t *testing.T) {
	opt := WithCredentialEnv("MY_API_KEY_VAR")
	assert.NotNil(t, opt)

	cfg := &credentialConfig{}
	opt.applyCredential(cfg)
	assert.Equal(t, "MY_API_KEY_VAR", cfg.credentialEnv)
}

// Platform option tests

func TestWithPlatformRegion(t *testing.T) {
	opt := WithPlatformRegion("us-west-2")
	assert.NotNil(t, opt)

	cfg := &platformConfig{}
	opt.applyPlatform(cfg)
	assert.Equal(t, "us-west-2", cfg.region)
}

func TestWithPlatformProject(t *testing.T) {
	opt := WithPlatformProject("my-gcp-project")
	assert.NotNil(t, opt)

	cfg := &platformConfig{}
	opt.applyPlatform(cfg)
	assert.Equal(t, "my-gcp-project", cfg.project)
}

func TestWithPlatformEndpoint(t *testing.T) {
	opt := WithPlatformEndpoint("https://custom.endpoint.com")
	assert.NotNil(t, opt)

	cfg := &platformConfig{}
	opt.applyPlatform(cfg)
	assert.Equal(t, "https://custom.endpoint.com", cfg.endpoint)
}

// Platform provider option tests

func TestWithBedrock(t *testing.T) {
	t.Run("basic usage", func(t *testing.T) {
		opt := WithBedrock("us-west-2", "claude", "claude-sonnet-4-20250514")
		assert.NotNil(t, opt)

		cfg := &config{}
		err := opt(cfg)
		assert.NoError(t, err)
		require.NotNil(t, cfg.platform)
		assert.Equal(t, "bedrock", cfg.platform.platformType)
		assert.Equal(t, "claude", cfg.platform.providerType)
		assert.Equal(t, "claude-sonnet-4-20250514", cfg.platform.model)
		assert.Equal(t, "us-west-2", cfg.platform.region)
	})

	t.Run("with additional options", func(t *testing.T) {
		opt := WithBedrock("us-east-1", "claude", "claude-sonnet-4-20250514",
			WithPlatformEndpoint("https://custom.bedrock.aws"))
		assert.NotNil(t, opt)

		cfg := &config{}
		err := opt(cfg)
		assert.NoError(t, err)
		require.NotNil(t, cfg.platform)
		assert.Equal(t, "bedrock", cfg.platform.platformType)
		assert.Equal(t, "us-east-1", cfg.platform.region)
		assert.Equal(t, "https://custom.bedrock.aws", cfg.platform.endpoint)
	})
}

func TestWithVertex(t *testing.T) {
	t.Run("basic usage", func(t *testing.T) {
		opt := WithVertex("us-central1", "my-project", "gemini", "gemini-2.0-flash")
		assert.NotNil(t, opt)

		cfg := &config{}
		err := opt(cfg)
		assert.NoError(t, err)
		require.NotNil(t, cfg.platform)
		assert.Equal(t, "vertex", cfg.platform.platformType)
		assert.Equal(t, "gemini", cfg.platform.providerType)
		assert.Equal(t, "gemini-2.0-flash", cfg.platform.model)
		assert.Equal(t, "us-central1", cfg.platform.region)
		assert.Equal(t, "my-project", cfg.platform.project)
	})

	t.Run("with additional options", func(t *testing.T) {
		opt := WithVertex("europe-west1", "my-project", "claude", "claude-sonnet-4-20250514",
			WithPlatformEndpoint("https://custom.vertex.googleapis.com"))
		assert.NotNil(t, opt)

		cfg := &config{}
		err := opt(cfg)
		assert.NoError(t, err)
		require.NotNil(t, cfg.platform)
		assert.Equal(t, "vertex", cfg.platform.platformType)
		assert.Equal(t, "europe-west1", cfg.platform.region)
		assert.Equal(t, "https://custom.vertex.googleapis.com", cfg.platform.endpoint)
	})
}

func TestWithAzure(t *testing.T) {
	t.Run("basic usage", func(t *testing.T) {
		opt := WithAzure("https://my-resource.openai.azure.com", "openai", "gpt-4o")
		assert.NotNil(t, opt)

		cfg := &config{}
		err := opt(cfg)
		assert.NoError(t, err)
		require.NotNil(t, cfg.platform)
		assert.Equal(t, "azure", cfg.platform.platformType)
		assert.Equal(t, "openai", cfg.platform.providerType)
		assert.Equal(t, "gpt-4o", cfg.platform.model)
		assert.Equal(t, "https://my-resource.openai.azure.com", cfg.platform.endpoint)
	})

	t.Run("with additional options", func(t *testing.T) {
		opt := WithAzure("https://my-resource.openai.azure.com", "openai", "gpt-4o",
			WithPlatformRegion("eastus"))
		assert.NotNil(t, opt)

		cfg := &config{}
		err := opt(cfg)
		assert.NoError(t, err)
		require.NotNil(t, cfg.platform)
		assert.Equal(t, "azure", cfg.platform.platformType)
		assert.Equal(t, "eastus", cfg.platform.region)
		assert.Equal(t, "https://my-resource.openai.azure.com", cfg.platform.endpoint)
	})
}

// MCP option tests (additional tests, main tests in mcp_test.go)

func TestNewMCPServerWithEnvMultiple(t *testing.T) {
	builder := NewMCPServer("test", "cmd").
		WithEnv("KEY1", "value1").
		WithEnv("KEY2", "value2")

	cfg := builder.Build()
	assert.Equal(t, "test", cfg.Name)
	assert.Equal(t, "cmd", cfg.Command)
	assert.Equal(t, "value1", cfg.Env["KEY1"])
	assert.Equal(t, "value2", cfg.Env["KEY2"])
}

// Validation option tests

func TestWithSkipSchemaValidation(t *testing.T) {
	opt := WithSkipSchemaValidation()
	assert.NotNil(t, opt)

	cfg := &config{}
	err := opt(cfg)
	assert.NoError(t, err)
	assert.True(t, cfg.skipSchemaValidation)
}

// Response format option tests

func TestWithResponseFormat(t *testing.T) {
	t.Run("JSON format", func(t *testing.T) {
		opt := WithResponseFormat(&providers.ResponseFormat{
			Type: providers.ResponseFormatJSON,
		})
		assert.NotNil(t, opt)

		cfg := &config{}
		err := opt(cfg)
		assert.NoError(t, err)
		assert.NotNil(t, cfg.responseFormat)
		assert.Equal(t, providers.ResponseFormatJSON, cfg.responseFormat.Type)
	})

	t.Run("JSON Schema format", func(t *testing.T) {
		opt := WithResponseFormat(&providers.ResponseFormat{
			Type:       providers.ResponseFormatJSONSchema,
			SchemaName: "person",
			Strict:     true,
		})
		assert.NotNil(t, opt)

		cfg := &config{}
		err := opt(cfg)
		assert.NoError(t, err)
		assert.NotNil(t, cfg.responseFormat)
		assert.Equal(t, providers.ResponseFormatJSONSchema, cfg.responseFormat.Type)
		assert.Equal(t, "person", cfg.responseFormat.SchemaName)
		assert.True(t, cfg.responseFormat.Strict)
	})
}

func TestWithJSONMode(t *testing.T) {
	opt := WithJSONMode()
	assert.NotNil(t, opt)

	cfg := &config{}
	err := opt(cfg)
	assert.NoError(t, err)
	assert.NotNil(t, cfg.responseFormat)
	assert.Equal(t, providers.ResponseFormatJSON, cfg.responseFormat.Type)
}

func TestWithContextWindow(t *testing.T) {
	opt := WithContextWindow(20)
	assert.NotNil(t, opt)

	cfg := &config{}
	err := opt(cfg)
	assert.NoError(t, err)
	assert.Equal(t, 20, cfg.contextWindow)
}

func TestWithContextRetrieval(t *testing.T) {
	embProvider := &mockEmbeddingProvider{}

	opt := WithContextRetrieval(embProvider, 5)
	assert.NotNil(t, opt)

	cfg := &config{}
	err := opt(cfg)
	assert.NoError(t, err)
	assert.Equal(t, embProvider, cfg.retrievalProvider)
	assert.Equal(t, 5, cfg.retrievalTopK)
}

func TestWithAutoSummarize(t *testing.T) {
	sumProvider := &mockSummarizeProvider{}

	opt := WithAutoSummarize(sumProvider, 100, 50)
	assert.NotNil(t, opt)

	cfg := &config{}
	err := opt(cfg)
	assert.NoError(t, err)
	assert.Equal(t, sumProvider, cfg.summarizeProvider)
	assert.Equal(t, 100, cfg.summarizeThreshold)
	assert.Equal(t, 50, cfg.summarizeBatchSize)
}

// Mock types for context management option tests

type mockEmbeddingProvider struct{}

func (m *mockEmbeddingProvider) Embed(_ context.Context, _ providers.EmbeddingRequest) (providers.EmbeddingResponse, error) {
	return providers.EmbeddingResponse{}, nil
}

func (m *mockEmbeddingProvider) EmbeddingDimensions() int { return 1536 }
func (m *mockEmbeddingProvider) MaxBatchSize() int        { return 100 }
func (m *mockEmbeddingProvider) ID() string               { return "mock-embedding" }

type mockSummarizeProvider struct{}

func (m *mockSummarizeProvider) ID() string    { return "mock-summarize" }
func (m *mockSummarizeProvider) Model() string { return "mock-summarize-model" }
func (m *mockSummarizeProvider) Predict(_ context.Context, _ providers.PredictionRequest) (providers.PredictionResponse, error) {
	return providers.PredictionResponse{}, nil
}
func (m *mockSummarizeProvider) PredictStream(_ context.Context, _ providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	return nil, nil
}
func (m *mockSummarizeProvider) SupportsStreaming() bool      { return false }
func (m *mockSummarizeProvider) ShouldIncludeRawOutput() bool { return false }
func (m *mockSummarizeProvider) Close() error                 { return nil }
func (m *mockSummarizeProvider) CalculateCost(_, _, _ int) types.CostInfo {
	return types.CostInfo{}
}

// Test hook types for hook option tests

type testProviderHook struct {
	name string
}

func (h *testProviderHook) Name() string { return h.name }
func (h *testProviderHook) BeforeCall(_ context.Context, _ *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}
func (h *testProviderHook) AfterCall(_ context.Context, _ *hooks.ProviderRequest, _ *hooks.ProviderResponse) hooks.Decision {
	return hooks.Allow
}

type testToolHook struct {
	name string
}

func (h *testToolHook) Name() string { return h.name }
func (h *testToolHook) BeforeExecution(_ context.Context, _ hooks.ToolRequest) hooks.Decision {
	return hooks.Allow
}
func (h *testToolHook) AfterExecution(_ context.Context, _ hooks.ToolRequest, _ hooks.ToolResponse) hooks.Decision {
	return hooks.Allow
}

type testSessionHook struct {
	name string
}

func (h *testSessionHook) Name() string                                          { return h.name }
func (h *testSessionHook) OnSessionStart(_ context.Context, _ hooks.SessionEvent) error { return nil }
func (h *testSessionHook) OnSessionUpdate(_ context.Context, _ hooks.SessionEvent) error {
	return nil
}
func (h *testSessionHook) OnSessionEnd(_ context.Context, _ hooks.SessionEvent) error { return nil }
