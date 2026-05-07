package base_test

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/stretchr/testify/assert"
)

func TestProviderType_String(t *testing.T) {
	cases := []struct {
		typ      base.ProviderType
		expected string
	}{
		{base.ProviderTypeInference, "inference"},
		{base.ProviderTypeTTS, "tts"},
		{base.ProviderTypeSTT, "stt"},
		{base.ProviderTypeEmbedding, "embedding"},
		{base.ProviderTypeImage, "image"},
	}
	for _, c := range cases {
		t.Run(c.expected, func(t *testing.T) {
			assert.Equal(t, c.expected, string(c.typ))
			parsed, err := base.ParseProviderType(c.expected)
			assert.NoError(t, err)
			assert.Equal(t, c.typ, parsed)
		})
	}
}

func TestProviderType_ParseInvalid(t *testing.T) {
	_, err := base.ParseProviderType("nonsense")
	assert.Error(t, err)
}
