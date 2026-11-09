package selection

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/sandbox"
)

// fakeSandbox captures the Request it receives and returns a
// canned Response. Lets tests assert on the JSON we send and control
// what the subprocess "produces."
type fakeSandbox struct {
	captured sandbox.Request
	resp     sandbox.Response
	spawnErr error
}

func (f *fakeSandbox) Name() string { return "fake" }

func (f *fakeSandbox) Spawn(_ context.Context, req sandbox.Request) (sandbox.Response, error) {
	f.captured = req
	return f.resp, f.spawnErr
}

func TestExecClient_Name(t *testing.T) {
	c := NewExecClient(ExecClientConfig{Name: "rerank", Command: "/bin/true"})
	if got := c.Name(); got != "rerank" {
		t.Errorf("Name() = %q, want %q", got, "rerank")
	}
}

func TestExecClient_Select_HappyPath(t *testing.T) {
	fake := &fakeSandbox{}
	fake.resp.Stdout = []byte(`{"selected":["b","a"]}`)

	c := NewExecClient(ExecClientConfig{
		Name:      "rerank",
		Command:   "/path/to/rerank",
		Args:      []string{"--k", "2"},
		Env:       []string{"RERANK_KEY"},
		TimeoutMs: 1500,
		Sandbox:   fake,
	})

	ids, err := c.Select(context.Background(),
		Query{Text: "what's the weather", Kind: "skill", K: 2},
		[]Candidate{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}, {ID: "c", Name: "C"}},
	)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if len(ids) != 2 || ids[0] != "b" || ids[1] != "a" {
		t.Errorf("Selected = %v, want [b a]", ids)
	}

	// Verify the subprocess saw the request we intended to send.
	if fake.captured.Command != "/path/to/rerank" {
		t.Errorf("Command = %q", fake.captured.Command)
	}
	if fake.captured.Timeout != 1500*time.Millisecond {
		t.Errorf("Timeout = %v, want 1.5s", fake.captured.Timeout)
	}
	var sent execRequest
	if err := json.Unmarshal(fake.captured.Stdin, &sent); err != nil {
		t.Fatalf("captured stdin not valid JSON: %v", err)
	}
	if sent.Query.Text != "what's the weather" || sent.Query.Kind != "skill" {
		t.Errorf("request query = %+v", sent.Query)
	}
	if len(sent.Candidates) != 3 {
		t.Errorf("sent %d candidates, want 3", len(sent.Candidates))
	}
}

func TestExecClient_Select_SpawnError(t *testing.T) {
	fake := &fakeSandbox{spawnErr: errors.New("boom")}
	c := NewExecClient(ExecClientConfig{Name: "x", Command: "/bin/false", Sandbox: fake})
	_, err := c.Select(context.Background(), Query{}, nil)
	if err == nil {
		t.Fatal("expected error on spawn failure")
	}
}

func TestExecClient_Select_ProcessError(t *testing.T) {
	fake := &fakeSandbox{}
	fake.resp.Err = errors.New("exit status 2")
	fake.resp.Stderr = []byte("bad request")

	c := NewExecClient(ExecClientConfig{Name: "x", Command: "/bin/false", Sandbox: fake})
	_, err := c.Select(context.Background(), Query{}, nil)
	if err == nil {
		t.Fatal("expected error when process errored")
	}
}

func TestExecClient_Select_InvalidJSON(t *testing.T) {
	fake := &fakeSandbox{}
	fake.resp.Stdout = []byte("not json")

	c := NewExecClient(ExecClientConfig{Name: "x", Command: "/bin/true", Sandbox: fake})
	_, err := c.Select(context.Background(), Query{}, nil)
	if err == nil {
		t.Fatal("expected error on invalid JSON")
	}
}

func TestExecClient_Timeout_UsesConfig(t *testing.T) {
	fake := &fakeSandbox{}
	fake.resp.Stdout = []byte(`{"selected":[]}`)
	c := NewExecClient(ExecClientConfig{Name: "x", Command: "/bin/true", TimeoutMs: 250, Sandbox: fake})
	_, _ = c.Select(context.Background(), Query{}, nil)
	if fake.captured.Timeout != 250*time.Millisecond {
		t.Errorf("Timeout = %v, want 250ms", fake.captured.Timeout)
	}
}

func TestExecClient_Timeout_Default(t *testing.T) {
	fake := &fakeSandbox{}
	fake.resp.Stdout = []byte(`{"selected":[]}`)
	c := NewExecClient(ExecClientConfig{Name: "x", Command: "/bin/true", Sandbox: fake})
	_, _ = c.Select(context.Background(), Query{}, nil)
	if fake.captured.Timeout != defaultExecSelectorTimeout {
		t.Errorf("Timeout = %v, want default %v", fake.captured.Timeout, defaultExecSelectorTimeout)
	}
}

func TestExecClient_NilSandbox_DefaultsToDirect(t *testing.T) {
	c := NewExecClient(ExecClientConfig{Name: "x", Command: "/bin/true"})
	if c.sandbox == nil {
		t.Fatal("expected default sandbox to be set when nil")
	}
	if c.sandbox.Name() != "direct" {
		t.Errorf("sandbox.Name() = %q, want direct", c.sandbox.Name())
	}
}

func TestExecClient_Init_Noop(t *testing.T) {
	c := NewExecClient(ExecClientConfig{Name: "x", Command: "/bin/true"})
	if err := c.Init(SelectorContext{}); err != nil {
		t.Errorf("Init: unexpected error %v", err)
	}
}

// The exec selector's stdin carries a skill candidate's allowed_tools, and
// leaves the key out for a candidate with none.
func TestExecClient_Select_SendsAllowedToolsOnlyWhenSet(t *testing.T) {
	fake := &fakeSandbox{}
	fake.resp.Stdout = []byte(`{"selected":["refunds"]}`)
	c := NewExecClient(ExecClientConfig{Name: "x", Command: "/bin/true", Sandbox: fake})

	_, err := c.Select(context.Background(), Query{Text: "refund", Kind: "skill"}, []Candidate{
		{ID: "refunds", Name: "refunds", AllowedTools: []string{"refund"}},
		{ID: "lookup", Name: "lookup"},
	})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	var sent struct {
		Candidates []map[string]json.RawMessage `json:"candidates"`
	}
	if err := json.Unmarshal(fake.captured.Stdin, &sent); err != nil {
		t.Fatalf("captured stdin not valid JSON: %v", err)
	}
	if got := string(sent.Candidates[0]["allowed_tools"]); got != `["refund"]` {
		t.Errorf("skill candidate allowed_tools = %s, want [\"refund\"]", got)
	}
	if _, ok := sent.Candidates[1]["allowed_tools"]; ok {
		t.Errorf("candidate with no allowed_tools sent the key: %s", fake.captured.Stdin)
	}
}

// The exec selector's stdin uses the documented lowercase keys, exactly.
func TestExecClient_Select_SendsTheDocumentedLowercaseKeys(t *testing.T) {
	fake := &fakeSandbox{}
	fake.resp.Stdout = []byte(`{"selected":["a"]}`)
	c := NewExecClient(ExecClientConfig{Name: "x", Command: "/bin/true", Sandbox: fake})

	_, err := c.Select(context.Background(),
		Query{Text: "refund", Kind: "skill", PackID: "p", AgentID: "agent", K: 3},
		[]Candidate{{ID: "a", Name: "A", Description: "d", Metadata: map[string]string{"tags": "x"},
			AllowedTools: []string{"refund"}}},
	)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	want := `{"query":{"text":"refund","kind":"skill","pack_id":"p","agent_id":"agent","k":3},` +
		`"candidates":[{"id":"a","name":"A","description":"d","metadata":{"tags":"x"},"allowed_tools":["refund"]}]}`
	if got := string(fake.captured.Stdin); got != want {
		t.Errorf("stdin =\n%s\nwant\n%s", got, want)
	}
}

// A response written with the PascalCase key is read as the lowercase one.
func TestExecClient_Select_ReadsPascalCaseSelected(t *testing.T) {
	fake := &fakeSandbox{}
	fake.resp.Stdout = []byte(`{"Selected":["b"]}`)
	c := NewExecClient(ExecClientConfig{Name: "x", Command: "/bin/true", Sandbox: fake})

	ids, err := c.Select(context.Background(), Query{Text: "q"}, []Candidate{{ID: "a"}, {ID: "b"}})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if len(ids) != 1 || ids[0] != "b" {
		t.Errorf("Selected = %v, want [b]", ids)
	}
}
