package sdk

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgconfig "github.com/AltairaLabs/PromptKit/pkg/v2/config"
	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/selection"
	"github.com/AltairaLabs/PromptKit/runtime/v2/streaming"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func TestWithIdleTimeout(t *testing.T) {
	t.Run("rejects a negative timeout", func(t *testing.T) {
		cfg := &config{}
		err := WithIdleTimeout(-time.Second)(cfg)
		require.Error(t, err)
		assert.Nil(t, cfg.idleTimeout)
	})
}

func TestWithRelevanceTruncation(t *testing.T) {
	t.Run("carries CacheEmbeddings to the stage config", func(t *testing.T) {
		for _, want := range []bool{true, false} {
			conv := &Conversation{config: &config{}}
			require.NoError(t, WithRelevanceTruncation(&RelevanceConfig{CacheEmbeddings: want})(conv.config))
			got := conv.buildRelevanceConfig()
			require.NotNil(t, got)
			assert.Equal(t, want, got.CacheEmbeddings)
		}
	})

}

func TestBuildHookRegistry(t *testing.T) {
	t.Run("returns nil when no hooks", func(t *testing.T) {
		cfg := &config{}
		assert.Nil(t, cfg.buildHookRegistry())
	})

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
	t.Run("with nil config uses defaults", func(t *testing.T) {
		c := &config{}
		require.NoError(t, WithImagePreprocessing(nil)(c))
		require.NotNil(t, c.imagePreprocessConfig)
		assert.Equal(t, stage.DefaultImagePreprocessConfig(), *c.imagePreprocessConfig)
	})
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

// The resize fields of VideoStreamConfig reach the pipeline as an image resize
// config; a config without dimensions, or with EnableResize off, adds none.
func TestBuildVideoFrameResize(t *testing.T) {
	build := func(vc *VideoStreamConfig) *stage.ImageResizeStageConfig {
		return (&Conversation{config: &config{videoStreamConfig: vc}}).buildVideoFrameResize()
	}

	got := build(&VideoStreamConfig{TargetFPS: 2, MaxWidth: 640, MaxHeight: 480, Quality: 70, EnableResize: true})
	require.NotNil(t, got, "MaxWidth/MaxHeight with EnableResize must configure a resize")
	assert.Equal(t, 640, got.MaxWidth)
	assert.Equal(t, 480, got.MaxHeight)
	assert.Equal(t, 70, got.Quality)
	assert.True(t, got.PreserveAspectRatio)
	assert.True(t, got.SkipIfSmaller, "frames within the limits must not be re-encoded")

	got = build(&VideoStreamConfig{MaxWidth: 640, EnableResize: true})
	require.NotNil(t, got)
	assert.Equal(t, 0, got.MaxHeight, "an unset dimension stays unlimited")
	assert.Equal(t, defaultVideoStreamQuality, got.Quality, "Quality 0 uses the default")

	assert.Nil(t, build(nil))
	assert.Nil(t, build(DefaultVideoStreamConfig()), "defaults carry no dimensions, so no resize")
	assert.Nil(t, build(&VideoStreamConfig{MaxWidth: 640, MaxHeight: 480}), "EnableResize off means no resize")
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

// A nil service passed to WithVADMode leaves an earlier WithTTS / WithSTT
// setting in place instead of clearing it; a non-nil one replaces it.
func TestWithVADMode_NilServicesKeepEarlierSettings(t *testing.T) {
	earlierTTS := newConvMockTTSService()
	earlierSTT := &convMockSTTService{}
	cfg := &config{ttsService: earlierTTS, sttService: earlierSTT}

	require.NoError(t, WithVADMode(nil, nil, nil)(cfg))
	assert.Same(t, earlierTTS, cfg.ttsService, "nil TTS must not clobber WithTTS")
	assert.Same(t, earlierSTT, cfg.sttService, "nil STT must not clobber an earlier STT service")

	laterTTS := newConvMockTTSService()
	laterSTT := &convMockSTTService{}
	require.NoError(t, WithVADMode(laterSTT, laterTTS, nil)(cfg))
	assert.Same(t, laterTTS, cfg.ttsService)
	assert.Same(t, laterSTT, cfg.sttService)
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
	assert.Zero(t, ttsCfg.OutputSampleRate, "no output rate by default: the service's own rate")

	cfg.OutputSampleRate = 16000
	assert.Equal(t, 16000, cfg.toTTSStageConfig(nil).OutputSampleRate)
}

// Credential option tests

func TestResolveCredentialAPIKey(t *testing.T) {
	t.Run("direct API key", func(t *testing.T) {
		cc := &credentialConfig{apiKey: "sk-direct"}
		key, err := resolveCredentialAPIKey(cc)
		assert.NoError(t, err)
		assert.Equal(t, "sk-direct", key)
	})

	t.Run("env var", func(t *testing.T) {
		t.Setenv("TEST_CREDENTIAL_KEY", "sk-from-env")
		cc := &credentialConfig{credentialEnv: "TEST_CREDENTIAL_KEY"}
		key, err := resolveCredentialAPIKey(cc)
		assert.NoError(t, err)
		assert.Equal(t, "sk-from-env", key)
	})

	t.Run("env var not set", func(t *testing.T) {
		cc := &credentialConfig{credentialEnv: "TEST_CREDENTIAL_KEY_NONEXISTENT"}
		_, err := resolveCredentialAPIKey(cc)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not set or empty")
	})

	t.Run("file", func(t *testing.T) {
		tmpFile := t.TempDir() + "/api-key.txt"
		err := os.WriteFile(tmpFile, []byte("sk-from-file\n"), 0o600)
		require.NoError(t, err)

		cc := &credentialConfig{credentialFile: tmpFile}
		key, err := resolveCredentialAPIKey(cc)
		assert.NoError(t, err)
		assert.Equal(t, "sk-from-file", key)
	})

	t.Run("file not found", func(t *testing.T) {
		cc := &credentialConfig{credentialFile: "/nonexistent/path/key.txt"}
		_, err := resolveCredentialAPIKey(cc)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read credential file")
	})

	t.Run("priority: api key over env", func(t *testing.T) {
		t.Setenv("TEST_CREDENTIAL_KEY_PRIORITY", "sk-from-env")
		cc := &credentialConfig{
			apiKey:        "sk-direct",
			credentialEnv: "TEST_CREDENTIAL_KEY_PRIORITY",
		}
		key, err := resolveCredentialAPIKey(cc)
		assert.NoError(t, err)
		assert.Equal(t, "sk-direct", key)
	})

	t.Run("priority: env over file", func(t *testing.T) {
		t.Setenv("TEST_CREDENTIAL_KEY_PRIORITY2", "sk-from-env")
		tmpFile := t.TempDir() + "/api-key.txt"
		err := os.WriteFile(tmpFile, []byte("sk-from-file"), 0o600)
		require.NoError(t, err)

		cc := &credentialConfig{
			credentialEnv:  "TEST_CREDENTIAL_KEY_PRIORITY2",
			credentialFile: tmpFile,
		}
		key, err := resolveCredentialAPIKey(cc)
		assert.NoError(t, err)
		assert.Equal(t, "sk-from-env", key)
	})

	t.Run("empty config returns empty", func(t *testing.T) {
		cc := &credentialConfig{}
		key, err := resolveCredentialAPIKey(cc)
		assert.NoError(t, err)
		assert.Equal(t, "", key)
	})
}

// Platform option tests

// Platform provider option tests

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

// Response format option tests

// Mock types for context management option tests

type mockEmbeddingProvider struct{}

func (m *mockEmbeddingProvider) Embed(_ context.Context, _ providers.EmbeddingRequest) (providers.EmbeddingResponse, error) {
	return providers.EmbeddingResponse{}, nil
}

func (m *mockEmbeddingProvider) EmbeddingDimensions() int { return 1536 }
func (m *mockEmbeddingProvider) MaxBatchSize() int        { return 100 }
func (m *mockEmbeddingProvider) ID() string               { return "mock-embedding" }

type mockSummarizeProvider struct {
	base.Implementation
}

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

func (h *testSessionHook) Name() string                                                 { return h.name }
func (h *testSessionHook) OnSessionStart(_ context.Context, _ hooks.SessionEvent) error { return nil }
func (h *testSessionHook) OnSessionUpdate(_ context.Context, _ hooks.SessionEvent) error {
	return nil
}
func (h *testSessionHook) OnSessionEnd(_ context.Context, _ hooks.SessionEvent) error { return nil }

func TestDefaultRecordingConfig(t *testing.T) {
	cfg := DefaultRecordingConfig()
	assert.True(t, cfg.IncludeAudio)
	assert.False(t, cfg.IncludeVideo)
	assert.True(t, cfg.IncludeImages)
}

// Platform credential override option tests

func TestWithMaxMessageSize(t *testing.T) {
	t.Run("rejects zero", func(t *testing.T) {
		opt := WithMaxMessageSize(0)
		cfg := &config{}
		err := opt(cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must be positive")
	})

	t.Run("rejects negative", func(t *testing.T) {
		opt := WithMaxMessageSize(-1)
		cfg := &config{}
		err := opt(cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must be positive")
	})
}

type optTestSelector struct{ n string }

func (s *optTestSelector) Name() string                         { return s.n }
func (s *optTestSelector) Init(selection.SelectorContext) error { return nil }
func (s *optTestSelector) Select(_ context.Context, _ selection.Query,
	_ []selection.Candidate,
) ([]string, error) {
	return nil, nil
}

func TestWithSelector(t *testing.T) {
	t.Run("rejects empty name", func(t *testing.T) {
		cfg := &config{}
		err := WithSelector("", &optTestSelector{})(cfg)
		require.Error(t, err)
	})
	t.Run("rejects nil impl", func(t *testing.T) {
		cfg := &config{}
		err := WithSelector("x", nil)(cfg)
		require.Error(t, err)
	})
	t.Run("rejects duplicate", func(t *testing.T) {
		cfg := &config{}
		require.NoError(t, WithSelector("r", &optTestSelector{})(cfg))
		err := WithSelector("r", &optTestSelector{})(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already registered")
	})
}

// stubText is a minimal inference provider for WithClassifier tests. The id field
// exists so two stubs compare unequal — an empty struct makes every value
// identical, which would let a "did the right one win?" assertion pass by
// accident.
type stubText struct{ id string }

func (s stubText) Infer(context.Context, inference.Request) (inference.Response, error) {
	return inference.Response{Raw: s.id}, nil
}

// rawOf calls p and returns its Raw answer, identifying which stub it is.
func rawOf(t *testing.T, p inference.Provider) string {
	t.Helper()
	resp, err := p.Infer(context.Background(), inference.Request{})
	require.NoError(t, err)
	return resp.Raw
}

func TestWithClassifier_RegistersBackend(t *testing.T) {
	c := &config{}
	require.NoError(t, WithClassifier("stub", stubText{id: "stub"})(c))
	p, err := c.inferenceRegistry.Get("stub")
	require.NoError(t, err, "stub provider should resolve")
	assert.Equal(t, "stub", rawOf(t, p))
}

func TestWithClassifier_RejectsAValueThatIsNotAnInferenceProvider(t *testing.T) {
	c := &config{}
	err := WithClassifier("old-style", struct{}{})(c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an inference.Provider")
	assert.Nil(t, c.inferenceRegistry, "nothing may be registered for a rejected provider")
}

func TestWithClassifier_RejectsNilProvider(t *testing.T) {
	c := &config{}
	err := WithClassifier("nope", nil)(c)
	require.Error(t, err, "a nil provider must be rejected, not registered")
	assert.Nil(t, c.inferenceRegistry, "nothing may be registered for a rejected provider")
}

func TestWithClassifier_ClaimsDefaultTask(t *testing.T) {
	c := &config{}
	require.NoError(t, WithClassifier("first", stubText{id: "first"})(c))
	require.NoError(t, WithClassifier("second", stubText{id: "second"})(c))

	// The empty id must resolve, and resolve to the FIRST registration —
	// a backend registered through an option has to claim the unset default,
	// and a later one must not steal it.
	byDefault, err := c.inferenceRegistry.Get("")
	require.NoError(t, err,
		"a provider registered through an option must claim the unset default, "+
			"otherwise every handler lookup that doesn't name an id fails")
	assert.Equal(t, "first", rawOf(t, byDefault), "the default should be the first registered")
}

func TestWithInferenceProvider_ClaimsDefaultTasks(t *testing.T) {
	c := &config{}
	err := WithInferenceProvider(ProviderSpec{
		ID:         "hf",
		Type:       "huggingface",
		Credential: &pkgconfig.CredentialConfig{APIKey: "tok"},
	})(c)
	require.NoError(t, err)

	// The first inference provider registered is the default.
	byID, err := c.inferenceRegistry.Get("hf")
	require.NoError(t, err)
	byDefault, err := c.inferenceRegistry.Get("")
	require.NoError(t, err, "role: inference must set the default inference provider")
	require.Equal(t, byID, byDefault)
}

func TestWithClassifier_DuplicateIDRejected(t *testing.T) {
	c := &config{}
	require.NoError(t, WithClassifier("dup", stubText{})(c))
	err := WithClassifier("dup", stubText{})(c)
	require.Error(t, err, "registering the same classify id twice must be rejected")
	assert.Contains(t, err.Error(), "duplicate")
}

func TestWithInferenceProvider_DuplicateIDAcrossSpellings(t *testing.T) {
	c := &config{}
	require.NoError(t, WithClassifier("dup", stubText{})(c))
	err := WithInferenceProvider(ProviderSpec{
		ID:         "dup",
		Type:       "huggingface",
		Credential: &pkgconfig.CredentialConfig{APIKey: "tok"},
	})(c)
	require.Error(t, err,
		"a duplicate classify id must be rejected across every registration path, "+
			"not just within a single config block")
}

func TestApplyInferenceProviders_DuplicateIDAgainstProgrammatic(t *testing.T) {
	c := &config{}
	require.NoError(t, WithClassifier("dup", stubText{})(c))
	err := applyInferenceProviders(c, []pkgconfig.InferenceProviderConfig{
		{ID: "dup", Type: "huggingface", Credential: &pkgconfig.CredentialConfig{APIKey: "tok"}},
	})
	require.Error(t, err,
		"inference_providers: must see ids already registered programmatically; "+
			"a block-local duplicate check silently overwrites them")
}

func TestWithInferenceProvider_DuplicateIDRejected(t *testing.T) {
	spec := ProviderSpec{ID: "hf", Type: "huggingface", Credential: &pkgconfig.CredentialConfig{APIKey: "tok"}}
	c := &config{}
	require.NoError(t, WithInferenceProvider(spec)(c))

	err := WithInferenceProvider(spec)(c)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
	assert.Equal(t, []string{"hf"}, c.inferenceProviderIDs, "the duplicate must not be recorded")
}

// A default key env var belongs to one vendor. With a base_url pointing at a
// different host (a gateway, vLLM), sending OPENAI_API_KEY would leak it.
func TestWithInferenceProvider_EnvKeyNeverGoesToAnotherHost(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-must-not-leak")
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"choices":[{"logprobs":{"content":[{"token":"on","logprob":0,` +
			`"top_logprobs":[{"token":"on","logprob":0}]}]}}]}`))
	}))
	defer srv.Close()
	c := &config{}
	require.NoError(t, WithInferenceProvider(ProviderSpec{ID: "gw", Type: "openai", Model: "m", BaseURL: srv.URL})(c))
	p, err := c.inferenceRegistry.Get("gw")
	require.NoError(t, err)

	_, err = p.Infer(context.Background(), inference.Request{Labels: []string{"on-topic", "off-topic"}})

	require.NoError(t, err)
	assert.Empty(t, gotAuth, "the OpenAI key must not be sent to a non-OpenAI base_url")
}

func TestEnvKeyAllowedFor(t *testing.T) {
	assert.True(t, envKeyAllowedFor("openai", ""), "the vendor's default endpoint")
	assert.True(t, envKeyAllowedFor("openai", "https://api.openai.com/v1"))
	assert.False(t, envKeyAllowedFor("openai", "https://ai-gateway.example.com/v1"))
	assert.False(t, envKeyAllowedFor("openai", "http://localhost:8000/v1"))
	assert.True(t, envKeyAllowedFor("huggingface", "https://x.us-east-1.aws.endpoints.huggingface.cloud"),
		"a dedicated HF endpoint is still HF")
	assert.False(t, envKeyAllowedFor("openai", "https://api.openai.com.evil.example/v1"))
	assert.True(t, envKeyAllowedFor("systemone", "https://ai-gateway.vercel.sh/typesafe/v1"),
		"types with no vendor key are unaffected (they have no env default)")
}

func TestWithStreamingVideo_UnsetQualityUsesDefault(t *testing.T) {
	cfg := &VideoStreamConfig{TargetFPS: 2}

	c := &config{}
	require.NoError(t, WithStreamingVideo(cfg)(c))
	require.NotNil(t, c.videoStreamConfig)
	assert.Equal(t, streaming.DefaultImageQuality, c.videoStreamConfig.Quality)
	assert.Equal(t, 2.0, c.videoStreamConfig.TargetFPS)
	assert.Equal(t, 0, cfg.Quality, "the caller's config must not be mutated")
}
