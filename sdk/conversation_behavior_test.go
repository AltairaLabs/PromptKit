package sdk

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// convRedirect sends every request to target, keeping its path, so a provider
// built for a public endpoint can be observed on a local server.
type convRedirect struct{ target *url.URL }

func (r convRedirect) RoundTrip(req *http.Request) (*http.Response, error) {
	out := req.Clone(req.Context())
	out.URL.Scheme = r.target.Scheme
	out.URL.Host = r.target.Host
	out.Host = r.target.Host
	return http.DefaultTransport.RoundTrip(out)
}

// convRedirectAgent points the conversation's own agent provider at w.
func convRedirectAgent(t *testing.T, conv *Conversation, w *convWire) {
	t.Helper()
	target, err := url.Parse(w.URL)
	require.NoError(t, err)
	p, ok := conv.config.getAgentProvider().(interface{ SetHTTPTransport(http.RoundTripper) })
	require.True(t, ok, "agent provider %T cannot take a transport", conv.config.getAgentProvider())
	p.SetHTTPTransport(convRedirect{target: target})
}

// The send path: the system prompt rendered from the pack, the user's text and
// the reply all reach the caller exactly.
func TestConv_SendRendersPackPromptAndReturnsReply(t *testing.T) {
	w := newConvWire(t, convWireReply{Text: "hi Ada"})
	conv := openConv(t, w)
	conv.SetVar("name", "Ada")

	resp, err := conv.Send(context.Background(), "who am I?")
	require.NoError(t, err)

	assert.Equal(t, "hi Ada", resp.Text())
	assert.Equal(t, []convWireMessage{
		{Role: "system", Text: "You help Ada."},
		{Role: "user", Text: "who am I?"},
	}, w.lastChat(t).messages(t))
	assert.Equal(t, "gpt-4o-mini", w.lastChat(t).Body["model"])
}

// WithAPIKey is documented as "an explicit API key instead of reading from
// environment": the key must be the one the provider authenticates with.
func TestConv_WithAPIKeyAuthenticatesTheRequest(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	w := newConvWire(t)
	conv, err := Open(createTestPackFile(t, convChatPack), "chat",
		WithSkipSchemaValidation(), WithAPIKey("explicit-key"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	convRedirectAgent(t, conv, w)

	_, _ = conv.Send(context.Background(), "hi") // the endpoint's answer is not under test
	assert.Equal(t, "Bearer explicit-key", w.last(t).Header.Get("Authorization"))
}

// WithAPIKey wins over the environment variable it replaces.
func TestConv_WithAPIKeyOverridesEnvironmentKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "env-key")
	w := newConvWire(t)
	conv, err := Open(createTestPackFile(t, convChatPack), "chat",
		WithSkipSchemaValidation(), WithAPIKey("explicit-key"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	convRedirectAgent(t, conv, w)

	_, _ = conv.Send(context.Background(), "hi") // the endpoint's answer is not under test
	assert.Equal(t, "Bearer explicit-key", w.last(t).Header.Get("Authorization"))
}

// WithModel selects the model the request names.
func TestConv_WithModelNamesTheRequestModel(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "env-key")
	w := newConvWire(t)
	conv, err := Open(createTestPackFile(t, convChatPack), "chat",
		WithSkipSchemaValidation(), WithModel("gpt-4o"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	convRedirectAgent(t, conv, w)

	_, _ = conv.Send(context.Background(), "hi") // the endpoint's answer is not under test
	assert.Equal(t, "gpt-4o", w.last(t).Body["model"])
	assert.Equal(t, "Bearer env-key", w.last(t).Header.Get("Authorization"))
}

// convClearProviderEnv removes every key auto-detection reads.
func convClearProviderEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"OPENAI_API_KEY", "OPENAI_TOKEN", "ANTHROPIC_API_KEY", "CLAUDE_API_KEY",
		"GOOGLE_API_KEY", "GEMINI_API_KEY"} {
		t.Setenv(k, "")
	}
}

// A model name picks the vendor, and WithAPIKey authenticates with that
// vendor's own scheme, even when no environment key for it exists.
func TestConv_WithAPIKeyAndModelPickTheVendorAndItsAuthScheme(t *testing.T) {
	cases := []struct {
		model  string
		path   string
		header string
		want   string
	}{
		{model: "gpt-4o", path: "/v1/", header: "Authorization", want: "Bearer explicit-key"},
		{model: "claude-sonnet-5", path: "/v1/messages", header: "X-Api-Key", want: "explicit-key"},
		{model: "gemini-3.6-flash", path: "/v1beta/models/gemini-3.6-flash", header: "X-Goog-Api-Key", want: "explicit-key"},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			convClearProviderEnv(t)
			w := newConvWire(t)
			conv, err := Open(createTestPackFile(t, convChatPack), "chat",
				WithSkipSchemaValidation(), WithAPIKey("explicit-key"), WithModel(tc.model))
			require.NoError(t, err)
			t.Cleanup(func() { _ = conv.Close() })
			convRedirectAgent(t, conv, w)

			_, _ = conv.Send(context.Background(), "hi") // the endpoint's answer is not under test
			got := w.last(t)
			assert.Contains(t, got.Path, tc.path, "the request went to the wrong vendor's API")
			assert.Equal(t, tc.want, got.Header.Get(tc.header))
		})
	}
}

// convToolPack is convChatPack with one local tool the prompt may call.
const convToolPack = `{
	"id": "conv-tool-pack",
	"version": "1.0.0",
	"prompts": {
		"chat": {"id": "chat", "name": "Chat", "version": "1.0.0",
			"system_template": "You look things up.", "tools": ["lookup"]}
	},
	"tools": {
		"lookup": {"name": "lookup", "description": "Look up a key", "mode": "local",
			"parameters": {"type": "object", "properties": {"key": {"type": "string"}}}}
	}
}`

// openConvPack opens packJSON's chat prompt against w.
func openConvPack(t *testing.T, w *convWire, packJSON string, opts ...Option) *Conversation {
	t.Helper()
	base := []Option{WithSkipSchemaValidation(), WithLLMProvider(convWireSpec(w))}
	conv, err := Open(createTestPackFile(t, packJSON), "chat", append(base, opts...)...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv
}

// convToolResult returns the text of the request's tool-result message.
func convToolResult(t *testing.T, r convWireRequest) string {
	t.Helper()
	for _, m := range r.messages(t) {
		if m.Role == "tool" {
			return m.Text
		}
	}
	t.Fatalf("the request carries no tool result: %+v", r.messages(t))
	return ""
}

// Variables set by SetVar, SetVars and SetVarsFromEnv render into the system
// prompt the provider receives; the last write wins.
func TestConv_VariablesRenderIntoTheSystemPrompt(t *testing.T) {
	ctx := context.Background()
	t.Run("default when unset", func(t *testing.T) {
		w := newConvWire(t)
		conv := openConv(t, w)
		_, err := conv.Send(ctx, "hi")
		require.NoError(t, err)
		assert.Equal(t, "You help nobody.", w.lastChat(t).system(t))
	})
	t.Run("SetVar then overwrite", func(t *testing.T) {
		w := newConvWire(t)
		conv := openConv(t, w)
		conv.SetVar("name", "Alice")
		conv.SetVar("name", "Bob")
		got, ok := conv.GetVar("name")
		assert.True(t, ok)
		assert.Equal(t, "Bob", got)
		_, err := conv.Send(ctx, "hi")
		require.NoError(t, err)
		assert.Equal(t, "You help Bob.", w.lastChat(t).system(t))
	})
	t.Run("SetVars stringifies values", func(t *testing.T) {
		w := newConvWire(t)
		conv := openConv(t, w)
		conv.SetVars(map[string]any{"name": 42})
		_, err := conv.Send(ctx, "hi")
		require.NoError(t, err)
		assert.Equal(t, "You help 42.", w.lastChat(t).system(t))
	})
	t.Run("SetVarsFromEnv strips the prefix and lowercases", func(t *testing.T) {
		w := newConvWire(t)
		conv := openConv(t, w)
		t.Setenv("CONVTEST_NAME", "Eve")
		conv.SetVarsFromEnv("CONVTEST_")
		_, err := conv.Send(ctx, "hi")
		require.NoError(t, err)
		assert.Equal(t, "You help Eve.", w.lastChat(t).system(t))
	})
}

// convStaticVars is a variables.Provider returning fixed values.
type convStaticVars map[string]string

func (convStaticVars) Name() string { return "conv-static" }
func (v convStaticVars) Provide(context.Context) (map[string]string, error) {
	return v, nil
}

// A variable provider supplies names the session does not hold; it cannot
// override a pack default (docs: sdk/explanation/variables.md, precedence).
func TestConv_VariableProviderRendersIntoTheSystemPrompt(t *testing.T) {
	const pack = `{"id": "p", "version": "1.0.0", "prompts": {"chat": {"id": "chat", "name": "Chat",
		"version": "1.0.0", "system_template": "You help {{name}} in {{city}}.",
		"variables": [{"name": "name", "type": "string", "required": false, "default": "nobody"},
			{"name": "city", "type": "string", "required": false}]}}}`
	w := newConvWire(t)
	conv := openConvPack(t, w, pack, WithVariableProvider(convStaticVars{"name": "Grace", "city": "Oslo"}))
	_, err := conv.Send(context.Background(), "hi")
	require.NoError(t, err)
	assert.Equal(t, "You help nobody in Oslo.", w.lastChat(t).system(t))
}

// OnTool, OnToolCtx and OnTools handlers run when the model calls the tool:
// they get the model's arguments, and their result is what the model reads.
func TestConv_ToolHandlersAnswerTheModelsToolCall(t *testing.T) {
	register := map[string]func(c *Conversation, got *map[string]any){
		"OnTool": func(c *Conversation, got *map[string]any) {
			c.OnTool("lookup", func(args map[string]any) (any, error) {
				*got = args
				return map[string]any{"value": "v-" + args["key"].(string)}, nil
			})
		},
		"OnToolCtx": func(c *Conversation, got *map[string]any) {
			c.OnToolCtx("lookup", func(ctx context.Context, args map[string]any) (any, error) {
				require.NotNil(t, ctx)
				*got = args
				return map[string]any{"value": "v-" + args["key"].(string)}, nil
			})
		},
		"OnTools": func(c *Conversation, got *map[string]any) {
			c.OnTools(map[string]ToolHandler{"lookup": func(args map[string]any) (any, error) {
				*got = args
				return map[string]any{"value": "v-" + args["key"].(string)}, nil
			}})
		},
	}
	for name, reg := range register {
		t.Run(name, func(t *testing.T) {
			w := newConvWire(t,
				convWireReply{ToolName: "lookup", ToolArgs: `{"key":"k1"}`},
				convWireReply{Text: "found it"})
			conv := openConvPack(t, w, convToolPack)
			var got map[string]any
			reg(conv, &got)

			resp, err := conv.Send(context.Background(), "look up k1")
			require.NoError(t, err)

			assert.Equal(t, map[string]any{"key": "k1"}, got)
			assert.Equal(t, "found it", resp.Text())
			reqs := w.chatRequests()
			require.Len(t, reqs, 2, "one request for the call, one with its result")
			assert.JSONEq(t, `{"value":"v-k1"}`, convToolResult(t, reqs[1]))
		})
	}
}

// A handler's error reaches the model as the tool result, not the caller.
func TestConv_ToolHandlerErrorIsReportedToTheModel(t *testing.T) {
	w := newConvWire(t,
		convWireReply{ToolName: "lookup", ToolArgs: `{"key":"k1"}`},
		convWireReply{Text: "sorry"})
	conv := openConvPack(t, w, convToolPack)
	conv.OnTool("lookup", func(map[string]any) (any, error) { return nil, assert.AnError })

	resp, err := conv.Send(context.Background(), "look up k1")
	require.NoError(t, err)
	assert.Equal(t, "sorry", resp.Text())
	assert.Contains(t, convToolResult(t, w.lastChat(t)), assert.AnError.Error())
}

// Send accepts a string or a *types.Message, and rejects anything else.
func TestConv_SendMessageTypes(t *testing.T) {
	ctx := context.Background()
	w := newConvWire(t)
	conv := openConv(t, w)

	_, err := conv.Send(ctx, "as string")
	require.NoError(t, err)
	assert.Equal(t, convWireMessage{Role: "user", Text: "as string"}, w.lastChat(t).messages(t)[1])

	msg := &types.Message{Role: "user"}
	msg.AddTextPart("as message")
	_, err = conv.Send(ctx, msg)
	require.NoError(t, err)
	got := w.lastChat(t).messages(t)
	assert.Equal(t, convWireMessage{Role: "user", Text: "as message"}, got[len(got)-1])

	before := len(w.chatRequests())
	_, err = conv.Send(ctx, 123)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be string or *types.Message")
	assert.Len(t, w.chatRequests(), before, "a rejected message must not reach the provider")
}

// History: each Send carries the earlier turns; Messages returns them as a
// copy; Clear forgets them.
func TestConv_HistoryMessagesAndClear(t *testing.T) {
	ctx := context.Background()
	w := newConvWire(t, convWireReply{Text: "one"}, convWireReply{Text: "two"}, convWireReply{Text: "three"})
	conv := openConv(t, w)

	assert.Empty(t, conv.Messages(ctx), "a new conversation has no history")
	require.NoError(t, conv.Clear(), "clearing an empty conversation is not an error")

	_, err := conv.Send(ctx, "first")
	require.NoError(t, err)
	_, err = conv.Send(ctx, "second")
	require.NoError(t, err)
	assert.Equal(t, []convWireMessage{
		{Role: "system", Text: "You help nobody."},
		{Role: "user", Text: "first"},
		{Role: "assistant", Text: "one"},
		{Role: "user", Text: "second"},
	}, w.lastChat(t).messages(t))

	msgs := conv.Messages(ctx)
	require.Len(t, msgs, 4)
	assert.Equal(t, []string{"user", "assistant", "user", "assistant"},
		[]string{msgs[0].Role, msgs[1].Role, msgs[2].Role, msgs[3].Role})
	assert.Equal(t, "two", msgs[3].GetContent())
	msgs[0].Role = "modified"
	assert.Equal(t, "user", conv.Messages(ctx)[0].Role, "Messages must return a copy")

	require.NoError(t, conv.Clear())
	assert.Empty(t, conv.Messages(ctx))
	_, err = conv.Send(ctx, "third")
	require.NoError(t, err)
	assert.Equal(t, []convWireMessage{
		{Role: "system", Text: "You help nobody."},
		{Role: "user", Text: "third"},
	}, w.lastChat(t).messages(t), "a cleared conversation must not resend old turns")
}

// Fork copies history, variables and handlers; afterwards the two diverge.
func TestConv_ForkCopiesThenDiverges(t *testing.T) {
	ctx := context.Background()
	w := newConvWire(t,
		convWireReply{Text: "orig"},
		convWireReply{ToolName: "lookup", ToolArgs: `{"key":"k"}`},
		convWireReply{Text: "fork reply"},
		convWireReply{Text: "orig again"})
	conv := openConvPack(t, w, convToolPack)
	conv.SetVar("name", "Alice")
	conv.OnToolCtx("lookup", func(context.Context, map[string]any) (any, error) { return "from-orig", nil })
	_, err := conv.Send(ctx, "before fork")
	require.NoError(t, err)

	fork, err := conv.Fork()
	require.NoError(t, err)
	t.Cleanup(func() { _ = fork.Close() })
	assert.NotEqual(t, conv.ID(), fork.ID())
	got, _ := fork.GetVar("name")
	assert.Equal(t, "Alice", got)
	fork.SetVar("name", "Bob")
	got, _ = conv.GetVar("name")
	assert.Equal(t, "Alice", got, "a fork's variables are its own")

	_, err = fork.Send(ctx, "in fork")
	require.NoError(t, err)
	assert.Equal(t, "from-orig", convToolResult(t, w.lastChat(t)), "the fork keeps OnToolCtx handlers")
	assert.Len(t, fork.Messages(ctx), 6)
	assert.Len(t, conv.Messages(ctx), 2, "the fork's turns do not reach the original")

	_, err = conv.Send(ctx, "orig turn")
	require.NoError(t, err)
	assert.Equal(t, []convWireMessage{
		{Role: "system", Text: "You look things up."},
		{Role: "user", Text: "before fork"},
		{Role: "assistant", Text: "orig"},
		{Role: "user", Text: "orig turn"},
	}, w.lastChat(t).messages(t))
}

// IDs: each Open gets its own, and WithConversationID names it.
func TestConv_ConversationIDs(t *testing.T) {
	w := newConvWire(t)
	a, b := openConv(t, w), openConv(t, w)
	assert.NotEmpty(t, a.ID())
	assert.NotEqual(t, a.ID(), b.ID())
	assert.Equal(t, "my-id", openConv(t, w, WithConversationID("my-id")).ID())
}

// A conversation persists its turns to the caller's state store under its ID.
func TestConv_StateStoreHoldsTheTurns(t *testing.T) {
	ctx := context.Background()
	store := statestore.NewMemoryStore()
	w := newConvWire(t, convWireReply{Text: "stored"})
	conv := openConv(t, w, WithStateStore(store), WithConversationID("persisted"))
	_, err := conv.Send(ctx, "remember me")
	require.NoError(t, err)

	state, err := store.Load(ctx, "persisted")
	require.NoError(t, err)
	require.Len(t, state.Messages, 2)
	assert.Equal(t, "remember me", state.Messages[0].GetContent())
	assert.Equal(t, "stored", state.Messages[1].GetContent())
}

// WithEventBus: the conversation publishes its events on the caller's bus.
func TestConv_EventBusCarriesTheConversationsEvents(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)
	created := make(chan string, 8)
	bus.Subscribe(events.EventMessageCreated, func(e *events.Event) {
		if d, ok := e.Data.(*events.MessageCreatedData); ok {
			created <- d.Role + ":" + d.Content
		}
	})
	w := newConvWire(t, convWireReply{Text: "pong"})
	conv := openConv(t, w, WithEventBus(bus))
	assert.Same(t, bus, conv.EventBus())

	_, err := conv.Send(context.Background(), "ping")
	require.NoError(t, err)
	var seen []string
	for len(seen) < 2 {
		select {
		case s := <-created:
			seen = append(seen, s)
		case <-time.After(2 * time.Second):
			t.Fatalf("message.created events missing, got %v", seen)
		}
	}
	assert.Equal(t, "assistant:pong", seen[1], "the reply is published on the caller's bus")
	assert.Equal(t, "user:", seen[0][:5], "the user's message is published first")
}

// ToolRegistry exposes the pack's tools the conversation offers the model.
func TestConv_ToolRegistryHoldsThePacksTools(t *testing.T) {
	w := newConvWire(t)
	conv := openConvPack(t, w, convToolPack)
	desc := conv.ToolRegistry().Get("lookup")
	require.NotNil(t, desc)
	assert.Equal(t, "Look up a key", desc.Description)

	_, err := conv.Send(context.Background(), "hi")
	require.NoError(t, err)
	offered, _ := w.lastChat(t).Body["tools"].([]any)
	require.Len(t, offered, 1)
	assert.Equal(t, "lookup", offered[0].(map[string]any)["function"].(map[string]any)["name"])
}

// Stream yields the reply's text in chunks, then a Done chunk carrying the
// whole message, and records the turn like Send.
func TestConv_StreamYieldsTextThenDone(t *testing.T) {
	ctx := context.Background()
	w := newConvWire(t, convWireReply{Text: "streamed reply"})
	conv := openConv(t, w)

	var text string
	var done *StreamChunk
	for chunk := range conv.Stream(ctx, "go") {
		require.NoError(t, chunk.Error)
		switch chunk.Type {
		case ChunkText:
			text += chunk.Text
		case ChunkDone:
			c := chunk
			done = &c
		}
	}
	assert.Equal(t, "streamed reply", text)
	require.NotNil(t, done, "the stream must end with a Done chunk")
	require.NotNil(t, done.Message)
	assert.Equal(t, "streamed reply", done.Message.Text())
	assert.Equal(t, true, w.lastChat(t).Body["stream"])
	msgs := conv.Messages(ctx)
	require.Len(t, msgs, 2)
	assert.Equal(t, "streamed reply", msgs[1].GetContent())
}

// convUserParts returns the content parts of the request's last user message.
func convUserParts(t *testing.T, r convWireRequest) []map[string]any {
	t.Helper()
	msgs, _ := r.Body["messages"].([]any)
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i].(map[string]any)
		if m["role"] != "user" {
			continue
		}
		var parts []map[string]any
		for _, p := range m["content"].([]any) {
			parts = append(parts, p.(map[string]any))
		}
		return parts
	}
	t.Fatal("the request carries no user message")
	return nil
}

// Send options that attach media put that media in the user message the
// provider receives, after the text.
func TestConv_SendMediaOptionsReachTheProvider(t *testing.T) {
	ctx := context.Background()
	w := newConvWire(t)
	conv := openConv(t, w, WithUnsafePrivateNetworkMedia())

	_, err := conv.Send(ctx, "What's in this image?", WithImageURL("https://example.com/image.jpg"))
	require.NoError(t, err)
	parts := convUserParts(t, w.lastChat(t))
	require.Len(t, parts, 2)
	assert.Equal(t, "What's in this image?", parts[0]["text"])
	assert.Equal(t, "https://example.com/image.jpg", parts[1]["image_url"].(map[string]any)["url"])

	png := []byte{0x89, 0x50, 0x4E, 0x47}
	_, err = conv.Send(ctx, "Describe this", WithImageData(png, "image/png"))
	require.NoError(t, err)
	parts = convUserParts(t, w.lastChat(t))
	require.Len(t, parts, 2)
	assert.Equal(t, "data:image/png;base64,iVBORw==", parts[1]["image_url"].(map[string]any)["url"])

	_, err = conv.Send(ctx, "Review this file", WithFile("test.txt", []byte("file body")))
	require.NoError(t, err)
	parts = convUserParts(t, w.lastChat(t))
	require.Len(t, parts, 2)
	assert.Equal(t, "[File: test.txt]\nfile body", parts[1]["text"])
}

// A provider's error status reaches the caller as a *ProviderError carrying
// the status, and the failed turn is not recorded as a reply.
func TestConv_ProviderFailureIsAProviderError(t *testing.T) {
	w := newConvWire(t, convWireReply{Status: 400})
	conv := openConv(t, w)

	resp, err := conv.Send(context.Background(), "hi")
	assert.Nil(t, resp)
	var pe *ProviderError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, 400, pe.StatusCode)
	assert.Equal(t, "agent", pe.Provider)
	assert.Contains(t, pe.Message, "scripted failure")
	for _, m := range conv.Messages(context.Background()) {
		assert.NotEqual(t, "assistant", m.Role, "a failed turn must not record a reply")
	}
}

// convTexts reduces a request to its message texts.
func convTexts(t *testing.T, r convWireRequest) []string {
	t.Helper()
	var out []string
	for _, m := range r.messages(t) {
		out = append(out, m.Text)
	}
	return out
}

// WithContextWindow sends only the last N messages; WithContextRetrieval adds
// older ones back, each once and in conversation order; WithAutoSummarize
// replaces summarized turns with the summary provider's text.
func TestConv_ContextWindowRetrievalAndSummary(t *testing.T) {
	ctx := context.Background()
	turns := []string{"u0", "u1", "u2", "u3"}
	send := func(t *testing.T, conv *Conversation) {
		t.Helper()
		for _, u := range turns {
			_, err := conv.Send(ctx, u)
			require.NoError(t, err)
		}
	}

	t.Run("window only", func(t *testing.T) {
		w := newConvWire(t, convWireReply{Echo: true})
		conv := openConv(t, w, WithStateStore(statestore.NewMemoryStore()), WithContextWindow(2))
		send(t, conv)
		assert.Equal(t, []string{"You help nobody.", "u2", "echo:u2", "u3"}, convTexts(t, w.lastChat(t)))
	})

	t.Run("window with retrieval", func(t *testing.T) {
		w := newConvWire(t, convWireReply{Echo: true})
		emb, err := openai.NewEmbeddingProvider(openai.WithEmbeddingBaseURL(w.URL), openai.WithEmbeddingAPIKey("k"),
			openai.WithEmbeddingDimensions(2))
		require.NoError(t, err)
		conv := openConv(t, w, WithStateStore(statestore.NewMemoryStore()), WithContextWindow(2),
			WithContextRetrieval(emb, 10))
		send(t, conv)
		assert.Equal(t,
			[]string{"You help nobody.", "u0", "echo:u0", "u1", "echo:u1", "u2", "echo:u2", "u3"},
			convTexts(t, w.lastChat(t)),
			"every older message comes back once, in order, and none duplicates the hot window")
	})

	t.Run("window with summary", func(t *testing.T) {
		w := newConvWire(t, convWireReply{Echo: true})
		sumWire := newConvWire(t, convWireReply{Text: "SUMMARY"})
		sum, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
			ID: "sum", Type: "openai", Model: "gpt-4o-mini", BaseURL: sumWire.URL})
		require.NoError(t, err)
		conv := openConv(t, w, WithStateStore(statestore.NewMemoryStore()), WithContextWindow(2),
			WithAutoSummarize(sum, 4, 2))
		send(t, conv)
		assert.Equal(t,
			[]string{"You help nobody.\n\n[Conversation summary (turns 0-2)]: SUMMARY", "u2", "echo:u2", "u3"},
			convTexts(t, w.lastChat(t)))
		assert.NotEmpty(t, sumWire.chatRequests(), "the summary comes from the summary provider")
	})
}

// convSessionLog records each session hook call as "kind:turn:messages".
type convSessionLog struct {
	mu    sync.Mutex
	calls []string
	ids   map[string]bool
}

func (l *convSessionLog) Name() string { return "conv-log" }
func (l *convSessionLog) note(kind string, e hooks.SessionEvent) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, fmt.Sprintf("%s:%d:%d", kind, e.TurnIndex, len(e.Messages)))
	if l.ids == nil {
		l.ids = map[string]bool{}
	}
	l.ids[e.SessionID] = true
	return nil
}
func (l *convSessionLog) OnSessionStart(_ context.Context, e hooks.SessionEvent) error {
	return l.note("start", e)
}
func (l *convSessionLog) OnSessionUpdate(_ context.Context, e hooks.SessionEvent) error {
	return l.note("update", e)
}
func (l *convSessionLog) OnSessionEnd(_ context.Context, e hooks.SessionEvent) error {
	return l.note("end", e)
}
func (l *convSessionLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.calls...)
}

// A session hook sees the conversation's life: start on Open, one update per
// completed turn carrying the turn count and history, and end on Close.
func TestConv_SessionHookSeesTheConversationsLife(t *testing.T) {
	ctx := context.Background()
	log := &convSessionLog{}
	w := newConvWire(t)
	t.Setenv("OPENAI_API_KEY", "test-key")
	conv, err := Open(createTestPackFile(t, convChatPack), "chat",
		WithSkipSchemaValidation(), WithLLMProvider(convWireSpec(w)), WithSessionHook(log),
		WithConversationID("hooked"))
	require.NoError(t, err)
	_, err = conv.Send(ctx, "one")
	require.NoError(t, err)
	_, err = conv.Send(ctx, "two")
	require.NoError(t, err)
	require.NoError(t, conv.Close())
	require.NoError(t, conv.Close(), "a second Close is a no-op")

	assert.Equal(t, []string{"start:0:0", "update:1:2", "update:2:4", "end:2:4"}, log.snapshot())
	assert.Equal(t, map[string]bool{"hooked": true}, log.ids)
}

// A fork keeps the parent's session hooks: its turns and Close reach them,
// under the fork's own session ID, with the history it continued from.
func TestConv_ForkKeepsSessionHooks(t *testing.T) {
	ctx := context.Background()
	log := &convSessionLog{}
	w := newConvWire(t)
	conv := openConv(t, w, WithSessionHook(log), WithConversationID("parent"))
	_, err := conv.Send(ctx, "one")
	require.NoError(t, err)
	fork, err := conv.Fork()
	require.NoError(t, err)
	_, err = fork.Send(ctx, "in fork")
	require.NoError(t, err)
	require.NoError(t, fork.Close())

	assert.Equal(t, map[string]bool{"parent": true, "parent-fork": true}, log.ids)
	calls := log.snapshot()
	assert.Regexp(t, `^end:\d+:4$`, calls[len(calls)-1], "the fork's Close reports its history")
}

// convStores returns each state store implementation a caller can persist a
// conversation in, fresh.
func convStores(t *testing.T) map[string]func() statestore.Store {
	return map[string]func() statestore.Store{
		"memory": func() statestore.Store { return statestore.NewMemoryStore() },
		"redis": func() statestore.Store {
			mr := miniredis.RunT(t)
			return statestore.NewRedisStore(redis.NewClient(&redis.Options{Addr: mr.Addr()}))
		},
	}
}

// Resume continues a persisted conversation: the next turn reaches the
// provider with the earlier turns, and Messages returns them.
func TestConv_ResumeContinuesThePersistedConversation(t *testing.T) {
	for name, newStore := range convStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			store := newStore()
			w := newConvWire(t, convWireReply{Echo: true})
			first := openConv(t, w, WithStateStore(store), WithConversationID("resumable"))
			first.SetVar("name", "Ada")
			_, err := first.Send(ctx, "one")
			require.NoError(t, err)
			require.NoError(t, first.Close())

			conv, err := Resume("resumable", createTestPackFile(t, convChatPack), "chat",
				WithSkipSchemaValidation(), WithLLMProvider(convWireSpec(w)), WithStateStore(store))
			require.NoError(t, err)
			t.Cleanup(func() { _ = conv.Close() })
			assert.Equal(t, "resumable", conv.ID())
			require.Len(t, conv.Messages(ctx), 2)

			_, err = conv.Send(ctx, "two")
			require.NoError(t, err)
			assert.Equal(t, []convWireMessage{
				{Role: "system", Text: "You help nobody."},
				{Role: "user", Text: "one"},
				{Role: "assistant", Text: "echo:one"},
				{Role: "user", Text: "two"},
			}, w.lastChat(t).messages(t))
			assert.Len(t, conv.Messages(ctx), 4)
		})
	}
}

// Resume refuses what it cannot continue, with an error the caller can test.
func TestConv_ResumeErrors(t *testing.T) {
	pack := createTestPackFile(t, convChatPack)
	w := newConvWire(t)
	_, err := Resume("x", pack, "chat", WithSkipSchemaValidation(), WithLLMProvider(convWireSpec(w)))
	assert.ErrorIs(t, err, ErrNoStateStore)
	for name, newStore := range convStores(t) {
		t.Run(name, func(t *testing.T) {
			_, err := Resume("never-saved", pack, "chat", WithSkipSchemaValidation(),
				WithLLMProvider(convWireSpec(w)), WithStateStore(newStore()))
			assert.ErrorIs(t, err, ErrConversationNotFound)
			_, err = ResumeDuplex("never-saved", pack, "chat", WithSkipSchemaValidation(),
				WithProvider(mock.NewStreamingProvider("rt", "rt-model", false)), WithStateStore(newStore()))
			assert.ErrorIs(t, err, ErrConversationNotFound)
		})
	}
}
