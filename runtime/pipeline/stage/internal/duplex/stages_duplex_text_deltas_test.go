package duplex

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	providersmock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// A reply whose text arrives as deltas (Delta without Content), then once more
// whole on Content (OpenAI Realtime's text-only output), streams delta by
// delta and is recorded once. The next turn starts afresh: text on Content
// alone is its text, not a repeat.
func TestDuplexStage_TextDeltasStreamAndAreRecordedOnce(t *testing.T) {
	stop := "stop"
	provider := providersmock.NewStreamingProvider("test", "test-model", false)
	stage := NewDuplexProviderStage(provider, baseConfig())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	input := make(chan core.StreamElement, 10)
	output := make(chan core.StreamElement, 40)
	input <- elementWithSystemPrompt("sys")
	go func() { stage.Process(ctx, input, output) }()

	session := awaitMockSession(t, provider)
	for _, c := range []providers.StreamChunk{
		{Delta: "Hel"}, {Delta: "lo "}, {Delta: "there"},
		{Content: "Hello there", Metadata: map[string]interface{}{"text_done": true}},
		{FinishReason: &stop},
		{Content: "Bye!"},
		{FinishReason: &stop},
	} {
		session.EmitChunk(&c)
	}
	_ = session.Close()

	var texts, replies []string
	for _, elem := range drainUntilClosed(t, output) {
		if elem.Text != nil {
			texts = append(texts, *elem.Text)
		}
		if elem.Message != nil && elem.Message.Role == "assistant" {
			replies = append(replies, elem.Message.Content)
		}
	}
	close(input)

	assert.Equal(t, []string{"Hel", "lo ", "there", "Bye!"}, texts)
	assert.Equal(t, []string{"Hello there", "Bye!"}, replies)
}
