package sdk

import (
"testing"

"github.com/stretchr/testify/assert"
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

func TestWithValidationMode(t *testing.T) {
	tests := []struct {
		name string
		mode ValidationMode
		want ValidationMode
	}{
		{"error", ValidationModeError, ValidationModeError},
		{"warn", ValidationModeWarn, ValidationModeWarn},
		{"disabled", ValidationModeDisabled, ValidationModeDisabled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
opt := WithValidationMode(tt.mode)
cfg := &config{}
			err := opt(cfg)
			assert.NoError(t, err)
			assert.Equal(t, tt.want, cfg.validationMode)
		})
	}
}

func TestWithDisabledValidators(t *testing.T) {
	opt := WithDisabledValidators("pii", "profanity")
	assert.NotNil(t, opt)

	cfg := &config{}
	err := opt(cfg)
	assert.NoError(t, err)
	assert.Equal(t, []string{"pii", "profanity"}, cfg.disabledValidators)
}

func TestWithStrictValidation(t *testing.T) {
	opt := WithStrictValidation()
	assert.NotNil(t, opt)

	cfg := &config{}
	err := opt(cfg)
	assert.NoError(t, err)
	assert.True(t, cfg.strictValidation)
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

	t.Run("WithFile", func(t *testing.T) {
opt := WithFile("doc.pdf", []byte("pdf content"))
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
