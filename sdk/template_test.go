package sdk

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testPackJSON is a minimal valid pack for template tests.
const testPackJSON = `{
	"name": "template-test-pack",
	"version": "v1",
	"prompts": {
		"chat": {
			"system_template": "You are a helpful assistant."
		},
		"summarize": {
			"system_template": "Summarize the following text."
		}
	}
}`

// writeTestPack writes a test pack file and returns its path.
func writeTestPack(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	packFile := filepath.Join(dir, "test.pack.json")
	err := os.WriteFile(packFile, []byte(testPackJSON), 0600)
	require.NoError(t, err)
	return packFile
}

func TestLoadTemplate(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		packFile := writeTestPack(t)

		tmpl, err := LoadTemplate(packFile, WithSkipSchemaValidation())
		require.NoError(t, err)
		require.NotNil(t, tmpl)

		// Verify cached pack
		assert.Equal(t, "template-test-pack", tmpl.Pack().Name)
		assert.Contains(t, tmpl.Pack().Prompts, "chat")
		assert.Contains(t, tmpl.Pack().Prompts, "summarize")

		// Verify shared registries are populated
		assert.NotNil(t, tmpl.promptRegistry)
		assert.NotNil(t, tmpl.toolRepository)
	})

	t.Run("non-existent path", func(t *testing.T) {
		_, err := LoadTemplate("/nonexistent/path/pack.json")
		assert.Error(t, err)
	})

	t.Run("option error", func(t *testing.T) {
		packFile := writeTestPack(t)
		_, err := LoadTemplate(packFile, func(c *config) error {
			return assert.AnError
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to apply option")
	})
}

func TestPackTemplateOpen(t *testing.T) {
	packFile := writeTestPack(t)
	tmpl, err := LoadTemplate(packFile, WithSkipSchemaValidation())
	require.NoError(t, err)

	t.Run("success with mock provider", func(t *testing.T) {
		provider := mock.NewProvider("mock", "mock-model", false)
		conv, err := tmpl.Open("chat", WithProvider(provider))
		require.NoError(t, err)
		require.NotNil(t, conv)

		assert.Equal(t, "chat", conv.promptName)
		assert.NotNil(t, conv.toolRegistry)
		defer conv.Close()
	})

	t.Run("prompt not found", func(t *testing.T) {
		provider := mock.NewProvider("mock", "mock-model", false)
		_, err := tmpl.Open("nonexistent", WithProvider(provider))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("no provider configured", func(t *testing.T) {
		// Clear all API keys
		os.Unsetenv("OPENAI_API_KEY")
		os.Unsetenv("ANTHROPIC_API_KEY")
		os.Unsetenv("GOOGLE_API_KEY")
		os.Unsetenv("GEMINI_API_KEY")

		_, err := tmpl.Open("chat")
		assert.Error(t, err)
	})

	t.Run("option error", func(t *testing.T) {
		_, err := tmpl.Open("chat", func(c *config) error {
			return assert.AnError
		})
		assert.Error(t, err)
	})

	t.Run("multiple prompts same template", func(t *testing.T) {
		provider := mock.NewProvider("mock", "mock-model", false)

		conv1, err := tmpl.Open("chat", WithProvider(provider))
		require.NoError(t, err)
		defer conv1.Close()

		conv2, err := tmpl.Open("summarize", WithProvider(provider))
		require.NoError(t, err)
		defer conv2.Close()

		// Each conversation has its own state
		assert.NotEqual(t, conv1.ID(), conv2.ID())
		assert.Equal(t, "chat", conv1.promptName)
		assert.Equal(t, "summarize", conv2.promptName)
	})

	t.Run("conversations share prompt registry", func(t *testing.T) {
		provider := mock.NewProvider("mock", "mock-model", false)

		conv1, err := tmpl.Open("chat", WithProvider(provider))
		require.NoError(t, err)
		defer conv1.Close()

		conv2, err := tmpl.Open("chat", WithProvider(provider))
		require.NoError(t, err)
		defer conv2.Close()

		// Both conversations reference the same prompt registry instance
		assert.Same(t, conv1.promptRegistry, conv2.promptRegistry)
	})

	t.Run("conversations have isolated tool registries", func(t *testing.T) {
		provider := mock.NewProvider("mock", "mock-model", false)

		conv1, err := tmpl.Open("chat", WithProvider(provider))
		require.NoError(t, err)
		defer conv1.Close()

		conv2, err := tmpl.Open("chat", WithProvider(provider))
		require.NoError(t, err)
		defer conv2.Close()

		// Each conversation has its own tool registry
		assert.NotSame(t, conv1.toolRegistry, conv2.toolRegistry)
	})
}

func TestPackTemplateOpenConcurrent(t *testing.T) {
	packFile := writeTestPack(t)
	tmpl, err := LoadTemplate(packFile, WithSkipSchemaValidation())
	require.NoError(t, err)

	const goroutines = 20
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	convs := make(chan *Conversation, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			provider := mock.NewProvider("mock", "mock-model", false)
			conv, err := tmpl.Open("chat", WithProvider(provider))
			if err != nil {
				errs <- err
				return
			}
			convs <- conv
		}()
	}

	wg.Wait()
	close(errs)
	close(convs)

	// No errors
	for err := range errs {
		t.Errorf("concurrent Open failed: %v", err)
	}

	// All conversations created successfully
	var created []*Conversation
	for conv := range convs {
		created = append(created, conv)
		defer conv.Close()
	}
	assert.Len(t, created, goroutines)

	// All conversations have unique IDs
	ids := make(map[string]bool, goroutines)
	for _, conv := range created {
		ids[conv.ID()] = true
	}
	assert.Len(t, ids, goroutines, "all conversation IDs should be unique")
}

func TestPackTemplateOpenSendReceive(t *testing.T) {
	packFile := writeTestPack(t)
	tmpl, err := LoadTemplate(packFile, WithSkipSchemaValidation())
	require.NoError(t, err)

	provider := mock.NewProvider("mock", "mock-model", false)
	conv, err := tmpl.Open("chat", WithProvider(provider))
	require.NoError(t, err)
	defer conv.Close()

	// Verify conversation is functional by sending a message
	resp, err := conv.Send(context.Background(), "Hello")
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.NotEmpty(t, resp.Text())
}

func BenchmarkTemplateVsDirectOpen(b *testing.B) {
	dir := b.TempDir()
	packFile := filepath.Join(dir, "bench.pack.json")
	err := os.WriteFile(packFile, []byte(testPackJSON), 0600)
	require.NoError(b, err)

	b.Run("Direct_Open", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			provider := mock.NewProvider("mock", "mock-model", false)
			conv, err := Open(packFile, "chat",
				WithSkipSchemaValidation(),
				WithProvider(provider),
			)
			if err != nil {
				b.Fatal(err)
			}
			conv.Close()
		}
	})

	b.Run("Template_Open", func(b *testing.B) {
		tmpl, err := LoadTemplate(packFile, WithSkipSchemaValidation())
		if err != nil {
			b.Fatal(err)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			provider := mock.NewProvider("mock", "mock-model", false)
			conv, err := tmpl.Open("chat", WithProvider(provider))
			if err != nil {
				b.Fatal(err)
			}
			conv.Close()
		}
	})
}
