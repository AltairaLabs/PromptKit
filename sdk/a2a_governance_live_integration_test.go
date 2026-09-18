//go:build integration

package sdk_test

// Live end-to-end for the pair #2018 + #2019, in the shape an embedder runs:
//
//	HTTP middleware puts an identity on the request context
//	  → A2A message/send
//	    → a conversation opened internally by A2AOpener
//	      → a real model decides to call a tool
//	        → the embedder's executor runs, and can read that identity
//
// Neither fix is sufficient alone. Without #2019 the executor is never in the
// path; without #2018 it runs but sees no identity, which reads as
// "unauthenticated" rather than as a failure.
//
// Run:
//
//	ANTHROPIC_API_KEY=... go test -tags integration ./sdk/ -run TestLive_A2AToolGovernance -v

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/claude"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	sdk "github.com/AltairaLabs/PromptKit/sdk/v2"
	a2aserver "github.com/AltairaLabs/PromptKit/server/a2a/v2"
)

const governancePackJSON = `{
	"id": "live-a2a-governance",
	"version": "1.0.0",
	"description": "Live pack with one tool, served over A2A",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You look up account balances. When asked about a balance, call get_balance with the account id, then state the balance in one short sentence.",
			"tools": ["get_balance"]
		}
	},
	"tools": {
		"get_balance": {
			"name": "get_balance",
			"description": "Look up the balance for an account",
			"mode": "local",
			"parameters": {
				"type": "object",
				"properties": {
					"account_id": {"type": "string", "description": "Account identifier"}
				},
				"required": ["account_id"]
			}
		}
	}
}`

type identityKey struct{}

// governedExecutor is the embedder's tool path: it records the caller identity
// it found on the context, the way a real one would use it to authorize.
type governedExecutor struct {
	mu       sync.Mutex
	calls    int
	identity any
}

func (e *governedExecutor) Name() string { return "governed" }

func (e *governedExecutor) Execute(
	ctx context.Context, _ *tools.ToolDescriptor, _ json.RawMessage,
) (json.RawMessage, error) {
	e.mu.Lock()
	e.calls++
	e.identity = ctx.Value(identityKey{})
	e.mu.Unlock()
	return json.RawMessage(`{"balance":"1420.55","currency":"USD"}`), nil
}

func (e *governedExecutor) observed() (int, any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls, e.identity
}

func TestLive_A2AToolGovernance(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	provider, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID:       "claude-live",
		Type:     "claude",
		Model:    envOrLive("A2A_GOVERNANCE_MODEL", "claude-haiku-4-5"),
		BaseURL:  "https://api.anthropic.com/v1",
		Defaults: providers.ProviderDefaults{MaxTokens: 512},
	})
	require.NoError(t, err)

	dir := t.TempDir()
	packPath := dir + "/governance.pack.json"
	require.NoError(t, os.WriteFile(packPath, []byte(governancePackJSON), 0o644))

	executor := &governedExecutor{}

	// #2019: the executor can only arrive as an option — A2AOpener opens the
	// conversation itself and hands back an adapter.
	opener := sdk.A2AOpener(packPath, "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
		sdk.WithToolExecutor("get_balance", executor),
	)

	srv := a2aserver.NewServer(a2aserver.ConversationOpener(opener))

	// The embedder's authenticating middleware.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), identityKey{}, "user-42")
		srv.Handler().ServeHTTP(w, r.WithContext(ctx))
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	task := liveA2ASend(t, ts, "ctx-governance-1", "What is the balance on account ACC-7781?")

	calls, identity := executor.observed()
	require.Equal(t, 1, calls, "the embedder's executor was not in the A2A tool path (#2019)")
	assert.Equal(t, "user-42", identity,
		"the executor ran but saw no caller identity; message/send dropped the context values (#2018)")

	require.Equal(t, a2a.TaskStateCompleted, task.Status.State)
	require.NotEmpty(t, task.Artifacts)
	require.NotEmpty(t, task.Artifacts[0].Parts)
	require.NotNil(t, task.Artifacts[0].Parts[0].Text)
	answer := *task.Artifacts[0].Parts[0].Text
	// Models format the figure freely ("$1,420.55"), so compare without separators.
	assert.Contains(t, strings.ReplaceAll(answer, ",", ""), "1420.55",
		"the tool result never reached the model")
	t.Logf("identity seen by the executor: %v; answer: %q", identity, strings.TrimSpace(answer))
}

func liveA2ASend(t *testing.T, ts *httptest.Server, contextID, text string) *a2a.Task {
	t.Helper()

	params, err := json.Marshal(a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: contextID,
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: &text}},
		},
		Configuration: &a2a.SendMessageConfiguration{Blocking: true},
	})
	require.NoError(t, err)

	body, err := json.Marshal(a2a.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  a2a.MethodSendMessage,
		Params:  params,
	})
	require.NoError(t, err)

	resp, err := http.Post(ts.URL+"/a2a", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	var rpcResp a2a.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rpcResp))
	require.Nil(t, rpcResp.Error, "RPC error from message/send")

	var task a2a.Task
	require.NoError(t, json.Unmarshal(rpcResp.Result, &task))
	return &task
}
