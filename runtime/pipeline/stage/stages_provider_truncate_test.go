package stage

import (
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/stretchr/testify/assert"
)

func TestEnforceResultSizeLimit(t *testing.T) {
	t.Run("no truncation when within limit", func(t *testing.T) {
		reg := tools.NewRegistry(tools.WithMaxToolResultSize(100))
		s := &ProviderStage{toolRegistry: reg}

		content := "short result"
		result := s.enforceResultSizeLimit("test_tool", content)
		assert.Equal(t, content, result)
	})

	t.Run("truncates when exceeding limit", func(t *testing.T) {
		reg := tools.NewRegistry(tools.WithMaxToolResultSize(10))
		s := &ProviderStage{toolRegistry: reg}

		content := "this is a long result that exceeds the limit"
		result := s.enforceResultSizeLimit("test_tool", content)

		assert.True(t, strings.HasPrefix(result, content[:10]))
		assert.Contains(t, result, "[truncated,")
		assert.Contains(t, result, "bytes exceeded limit of 10 bytes]")
	})

	t.Run("no truncation when limit is zero", func(t *testing.T) {
		reg := tools.NewRegistry(tools.WithMaxToolResultSize(0))
		s := &ProviderStage{toolRegistry: reg}

		content := strings.Repeat("x", 10000)
		result := s.enforceResultSizeLimit("test_tool", content)
		assert.Equal(t, content, result)
	})

	t.Run("no truncation when no registry", func(t *testing.T) {
		s := &ProviderStage{toolRegistry: nil}

		content := "some result"
		result := s.enforceResultSizeLimit("test_tool", content)
		assert.Equal(t, content, result)
	})

	t.Run("exact limit is not truncated", func(t *testing.T) {
		reg := tools.NewRegistry(tools.WithMaxToolResultSize(5))
		s := &ProviderStage{toolRegistry: reg}

		content := "12345"
		result := s.enforceResultSizeLimit("test_tool", content)
		assert.Equal(t, content, result)
	})

	t.Run("one byte over limit is truncated", func(t *testing.T) {
		reg := tools.NewRegistry(tools.WithMaxToolResultSize(5))
		s := &ProviderStage{toolRegistry: reg}

		content := "123456"
		result := s.enforceResultSizeLimit("test_tool", content)
		assert.True(t, strings.HasPrefix(result, "12345"))
		assert.Contains(t, result, "[truncated,")
	})
}
