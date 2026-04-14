package selection

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/hooks/sandbox"
	"github.com/AltairaLabs/PromptKit/runtime/hooks/sandbox/direct"
)

const defaultExecSelectorTimeout = 10 * time.Second

// ExecClientConfig configures an exec-backed Selector. The subprocess
// reads a single JSON object on stdin and writes a JSON object on
// stdout (see wire protocol below). When Sandbox is nil, the built-in
// direct backend is used.
type ExecClientConfig struct {
	Name      string
	Command   string
	Args      []string
	Env       []string
	TimeoutMs int
	Sandbox   sandbox.Sandbox
}

// execRequest is the JSON payload sent to the selector subprocess.
type execRequest struct {
	Query      Query       `json:"query"`
	Candidates []Candidate `json:"candidates"`
}

// execResponse is the JSON payload expected from the selector
// subprocess. Additional fields (reason, scores, ...) are ignored;
// only Selected drives behavior.
type execResponse struct {
	Selected []string `json:"selected"`
	Reason   string   `json:"reason,omitempty"`
}

// ExecClient is a Selector that delegates ranking to an external
// process. Spawning goes through a Sandbox so the process can run
// on-host, in a sidecar, or wherever the sandbox dictates.
type ExecClient struct {
	name      string
	command   string
	args      []string
	env       []string
	timeoutMs int
	sandbox   sandbox.Sandbox
}

// NewExecClient constructs an ExecClient from the given config.
// Sandbox defaults to the built-in direct backend when nil.
//
//nolint:gocritic // config is a value-semantics builder; users assemble inline.
func NewExecClient(cfg ExecClientConfig) *ExecClient {
	sb := cfg.Sandbox
	if sb == nil {
		sb = direct.New(direct.ModeName)
	}
	return &ExecClient{
		name:      cfg.Name,
		command:   cfg.Command,
		args:      cfg.Args,
		env:       cfg.Env,
		timeoutMs: cfg.TimeoutMs,
		sandbox:   sb,
	}
}

// Name returns the configured selector name.
func (c *ExecClient) Name() string { return c.name }

// Init is a no-op for ExecClient. The subprocess owns whatever
// resources it needs; PromptKit holds no state on its behalf.
func (c *ExecClient) Init(SelectorContext) error { return nil }

// Select serializes the request, spawns the subprocess via the
// sandbox, and parses selected IDs from stdout. On any failure
// (process error, timeout, invalid JSON) it returns the error
// unchanged; callers are expected to treat a non-nil error as
// "include all eligible" rather than crashing the conversation.
func (c *ExecClient) Select(ctx context.Context, q Query, candidates []Candidate) ([]string, error) {
	reqBytes, err := json.Marshal(execRequest{Query: q, Candidates: candidates})
	if err != nil {
		return nil, fmt.Errorf("selector %q: marshal request: %w", c.name, err)
	}

	resp, spawnErr := c.sandbox.Spawn(ctx, sandbox.Request{
		Command: c.command,
		Args:    c.args,
		Env:     c.env,
		Stdin:   reqBytes,
		Timeout: c.timeout(),
	})
	if spawnErr != nil {
		return nil, fmt.Errorf("selector %q: spawn: %w", c.name, spawnErr)
	}
	if resp.Err != nil {
		return nil, fmt.Errorf("selector %q: process: %w", c.name, resp.Err)
	}

	var out execResponse
	if err := json.Unmarshal(resp.Stdout, &out); err != nil {
		return nil, fmt.Errorf("selector %q: invalid response JSON: %w", c.name, err)
	}
	return out.Selected, nil
}

func (c *ExecClient) timeout() time.Duration {
	if c.timeoutMs > 0 {
		return time.Duration(c.timeoutMs) * time.Millisecond
	}
	return defaultExecSelectorTimeout
}
