package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/AltairaLabs/PromptKit/tools/arena/mcpsource"
)

const (
	defaultSandboxPort = 8080
	defaultHealthWait  = 20 * time.Second
	healthPollInterval = 200 * time.Millisecond
)

// Source is the Docker-backed MCPSource.
type Source struct {
	cli CLI
	// urlForContainer is overridable for tests; production returns
	// http://localhost:<hostPort>.
	urlForContainer func(containerID, hostPort string) string
	// healthTimeoutOverride shortens the wait in tests. Zero means
	// "use defaultHealthWait".
	healthTimeoutOverride time.Duration
}

// New returns a Docker source using the local docker CLI.
func New() *Source { return &Source{cli: NewCLI()} }

// Open validates args, starts the container, optionally clones a repo
// into /workspace, health-polls, and returns the MCP URL + closer.
func (s *Source) Open(ctx context.Context, args map[string]any) (mcpsource.MCPConn, io.Closer, error) {
	image, ok := args["image"].(string)
	if !ok || image == "" {
		return mcpsource.MCPConn{}, nil, errors.New("docker source: args.image is required")
	}

	hostPort, err := pickFreePort()
	if err != nil {
		return mcpsource.MCPConn{}, nil, fmt.Errorf("docker source: pick free port: %w", err)
	}

	spec := RunSpec{
		Image:    image,
		PortHost: hostPort,
		PortCtr:  defaultSandboxPort,
		Env:      stringMap(args["env"]),
		Mounts:   mountsFromArgs(args["mounts"]),
	}

	cid, err := s.cli.Run(ctx, spec)
	if err != nil {
		return mcpsource.MCPConn{}, nil, fmt.Errorf("docker source: run: %w", err)
	}

	closer := closerFunc(func() error {
		return s.cli.Stop(context.Background(), cid)
	})

	url := s.resolveURL(cid, strconv.Itoa(hostPort))

	if repo, _ := args["repo"].(string); repo != "" {
		branch, _ := args["branch"].(string)
		if err := s.cloneRepo(ctx, cid, repo, branch); err != nil {
			_ = closer.Close()
			return mcpsource.MCPConn{}, nil, err
		}
	}

	timeout := s.healthTimeoutOverride
	if timeout == 0 {
		timeout = defaultHealthWait
	}
	if err := waitForHealth(ctx, url, timeout); err != nil {
		_ = closer.Close()
		return mcpsource.MCPConn{}, nil, fmt.Errorf("docker source: container not healthy: %w", err)
	}

	return mcpsource.MCPConn{URL: url}, closer, nil
}

func (s *Source) resolveURL(cid, port string) string {
	if s.urlForContainer != nil {
		return s.urlForContainer(cid, port)
	}
	return "http://localhost:" + port
}

func (s *Source) cloneRepo(ctx context.Context, cid, repo, branch string) error {
	argv := []string{"git", "clone"}
	if branch != "" {
		argv = append(argv, "--branch", branch)
	}
	argv = append(argv, repo, "/workspace")
	if _, err := s.cli.Exec(ctx, cid, argv); err != nil {
		return fmt.Errorf("docker source: clone %s: %w", repo, err)
	}
	return nil
}

type closerFunc func() error

// Close invokes the underlying function; satisfies io.Closer.
func (f closerFunc) Close() error { return f() }

func stringMap(v any) map[string]string {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, raw := range m {
		if s, ok := raw.(string); ok {
			out[k] = s
		}
	}
	return out
}

func mountsFromArgs(v any) []Mount {
	list, ok := v.([]map[string]any)
	if !ok {
		// Accept the []any shape that YAML unmarshal produces for lists.
		generic, gok := v.([]any)
		if !gok {
			return nil
		}
		list = make([]map[string]any, 0, len(generic))
		for _, item := range generic {
			if m, ok := item.(map[string]any); ok {
				list = append(list, m)
			}
		}
	}
	var out []Mount
	for _, entry := range list {
		src, _ := entry["source"].(string)
		target, _ := entry["target"].(string)
		ro, _ := entry["readonly"].(bool)
		if src != "" && target != "" {
			out = append(out, Mount{Source: src, Target: target, ReadOnly: ro})
		}
	}
	return out
}

func pickFreePort() (int, error) {
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func waitForHealth(ctx context.Context, baseURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", http.NoBody)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(healthPollInterval):
		}
	}
	return fmt.Errorf("health timeout after %v", timeout)
}
