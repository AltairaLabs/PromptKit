package selection

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/hooks/sandbox"
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
