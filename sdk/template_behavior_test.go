package sdk

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// convTemplatePack has two prompts, one of which renders {{name}}.
const convTemplatePack = `{
	"id": "conv-template", "version": "1.0.0",
	"prompts": {
		"chat": {"id": "chat", "name": "Chat", "version": "1.0.0", "system_template": "You help {{name}}.",
			"variables": [{"name": "name", "type": "string", "required": false, "default": "nobody"}]},
		"summarize": {"id": "summarize", "name": "Summarize", "version": "1.0.0",
			"system_template": "Summarize the text."}
	}
}`

// Conversations opened from one template share the loaded pack but nothing a
// caller sets: each renders its own prompt and variables and keeps its own
// history, including when they are opened and used concurrently from a cold
// template. Run under -race.
func TestConv_TemplateConversationsAreIndependent(t *testing.T) {
	tmpl, err := LoadTemplate(createTestPackFile(t, convTemplatePack), WithSkipSchemaValidation())
	require.NoError(t, err)
	w := newConvWire(t, convWireReply{Echo: true})

	const n = 4
	start := make(chan struct{})
	var wg sync.WaitGroup
	convs := make([]*Conversation, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			prompt := "chat"
			if i == n-1 {
				prompt = "summarize"
			}
			conv, err := tmpl.Open(prompt, WithLLMProvider(convWireSpec(w)))
			if err != nil {
				errs[i] = err
				return
			}
			convs[i] = conv
			conv.SetVar("name", fmt.Sprintf("user-%d", i))
			_, errs[i] = conv.Send(context.Background(), fmt.Sprintf("msg-%d", i))
		}(i)
	}
	close(start)
	wg.Wait()
	for i := range convs {
		require.NoError(t, errs[i], "conversation %d", i)
		t.Cleanup(func() { _ = convs[i].Close() })
	}

	systems := map[string]string{}
	for _, r := range w.chatRequests() {
		m := r.messages(t)
		systems[m[len(m)-1].Text] = m[0].Text
	}
	for i := 0; i < n-1; i++ {
		assert.Equal(t, fmt.Sprintf("You help user-%d.", i), systems[fmt.Sprintf("msg-%d", i)])
	}
	assert.Equal(t, "Summarize the text.", systems[fmt.Sprintf("msg-%d", n-1)])
	for i, conv := range convs {
		msgs := conv.Messages(context.Background())
		require.Len(t, msgs, 2, "conversation %d sees only its own turn", i)
		assert.Equal(t, fmt.Sprintf("msg-%d", i), msgs[0].GetContent())
	}
}
