package sdk

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgconfig "github.com/AltairaLabs/PromptKit/pkg/v2/config"
)

// A provider declared in a runtime config is what the conversation calls: its
// base URL, model, credential, headers and defaults all reach the request.
func TestConv_RuntimeConfigProviderReachesTheWire(t *testing.T) {
	convClearProviderEnv(t)
	w := newConvWire(t, convWireReply{Text: "from yaml"})
	rc := writeRuntimeConfig(t, `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
metadata:
  name: test
spec:
  providers:
    - id: agent
      type: openai
      model: gpt-4o-mini
      base_url: `+w.URL+`
      credential:
        api_key: yaml-key
      headers:
        X-Conv-Tenant: acme
      defaults:
        temperature: 0.25
`)
	conv, err := Open(createTestPackFile(t, convChatPack), "chat", WithSkipSchemaValidation(), WithRuntimeConfig(rc))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	resp, err := conv.Send(context.Background(), "hi")
	require.NoError(t, err)
	assert.Equal(t, "from yaml", resp.Text())
	r := w.lastChat(t)
	assert.Equal(t, "Bearer yaml-key", r.Header.Get("Authorization"))
	assert.Equal(t, "acme", r.Header.Get("X-Conv-Tenant"))
	assert.Equal(t, "gpt-4o-mini", r.Body["model"])
	assert.InDelta(t, 0.25, r.Body["temperature"], 1e-9)
}

// A state store declared in a runtime config holds the conversation: a
// conversation resumed with the same config continues its history. Run for
// every store type a runtime config can name.
func TestConv_RuntimeConfigStateStoreHoldsTheConversation(t *testing.T) {
	mr := miniredis.RunT(t)
	stores := map[string]string{
		"memory": "    type: memory\n",
		"redis":  "    type: redis\n    redis:\n      address: " + mr.Addr() + "\n",
		"file":   "    type: file\n    file:\n      root: " + t.TempDir() + "\n",
	}
	for name, storeYAML := range stores {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			rc := writeRuntimeConfig(t, "apiVersion: promptkit.altairalabs.ai/v1alpha1\nkind: RuntimeConfig\n"+
				"metadata:\n  name: test\nspec:\n  state_store:\n"+storeYAML)
			w := newConvWire(t, convWireReply{Echo: true})
			pack := createTestPackFile(t, convChatPack)
			first, err := Open(pack, "chat", WithSkipSchemaValidation(), WithLLMProvider(convWireSpec(w)),
				WithRuntimeConfig(rc), WithConversationID("rc-"+name))
			require.NoError(t, err)
			_, err = first.Send(ctx, "one")
			require.NoError(t, err)
			require.NoError(t, first.Close())

			if name == "memory" {
				// A memory store lives in its Open: another Open gets a new one.
				_, err = Resume("rc-"+name, pack, "chat", WithSkipSchemaValidation(),
					WithLLMProvider(convWireSpec(w)), WithRuntimeConfig(rc))
				assert.ErrorIs(t, err, ErrConversationNotFound)
				return
			}
			conv, err := Resume("rc-"+name, pack, "chat", WithSkipSchemaValidation(),
				WithLLMProvider(convWireSpec(w)), WithRuntimeConfig(rc))
			require.NoError(t, err)
			t.Cleanup(func() { _ = conv.Close() })
			_, err = conv.Send(ctx, "two")
			require.NoError(t, err)
			assert.Equal(t, []string{"You help nobody.", "one", "echo:one", "two"}, convTexts(t, w.lastChat(t)))
		})
	}
}

// A runtime config's logging block decides the log's format, level and
// common fields.
func TestConv_RuntimeConfigLoggingFormatLevelAndFields(t *testing.T) {
	r, wPipe, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stderr
	os.Stderr = wPipe
	l := buildLoggerFromSpec(&pkgconfig.LoggingConfigSpec{
		DefaultLevel: "warn", Format: "json", CommonFields: map[string]string{"env": "test"},
	})
	os.Stderr = orig

	l.Info("dropped: below the level")
	l.Warn("kept", "k", "v")
	require.NoError(t, wPipe.Close())

	var lines []map[string]any
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		var m map[string]any
		require.NoError(t, json.Unmarshal(sc.Bytes(), &m), "a JSON-format line: %s", sc.Text())
		lines = append(lines, m)
	}
	require.Len(t, lines, 1, "info is below the warn level")
	assert.Equal(t, "kept", lines[0]["msg"])
	assert.Equal(t, "WARN", lines[0]["level"])
	assert.Equal(t, "test", lines[0]["env"])
	assert.Equal(t, "v", lines[0]["k"])

	assert.False(t, buildLoggerFromSpec(&pkgconfig.LoggingConfigSpec{}).Enabled(context.Background(), -4),
		"the default level is info")
	assert.True(t, buildLoggerFromSpec(&pkgconfig.LoggingConfigSpec{DefaultLevel: "debug"}).
		Enabled(context.Background(), -4))
}
