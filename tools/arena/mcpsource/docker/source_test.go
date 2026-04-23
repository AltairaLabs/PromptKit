package docker

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCLI struct {
	runCalls  []RunSpec
	runID     string
	runErr    error
	stopCalls []string
	stopErr   error
	execCalls [][]string
	execOut   string
	execErr   error
}

func (f *fakeCLI) Run(_ context.Context, spec RunSpec) (string, error) {
	f.runCalls = append(f.runCalls, spec)
	return f.runID, f.runErr
}
func (f *fakeCLI) Stop(_ context.Context, id string) error {
	f.stopCalls = append(f.stopCalls, id)
	return f.stopErr
}
func (f *fakeCLI) Exec(_ context.Context, _ string, argv []string) (string, error) {
	f.execCalls = append(f.execCalls, argv)
	return f.execOut, f.execErr
}

func TestSource_Open_RequiresImage(t *testing.T) {
	s := &Source{cli: &fakeCLI{}}
	_, _, err := s.Open(context.Background(), map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "image")
}

func TestSource_Open_StartsContainerAndReturnsURL(t *testing.T) {
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer health.Close()

	cli := &fakeCLI{runID: "cid1"}
	s := &Source{cli: cli, urlForContainer: func(_, _ string) string { return health.URL }}

	conn, closer, err := s.Open(context.Background(), map[string]any{"image": "x"})
	require.NoError(t, err)
	assert.Equal(t, health.URL, conn.URL)
	require.NotNil(t, closer)
	require.Len(t, cli.runCalls, 1)
	assert.Equal(t, "x", cli.runCalls[0].Image)

	require.NoError(t, closer.Close())
	assert.Equal(t, []string{"cid1"}, cli.stopCalls)
}

func TestSource_Open_HealthTimeoutClosesContainer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	cli := &fakeCLI{runID: "cid-timeout"}
	s := &Source{
		cli:                   cli,
		urlForContainer:       func(_, _ string) string { return srv.URL },
		healthTimeoutOverride: 50 * time.Millisecond,
	}

	_, _, err := s.Open(context.Background(), map[string]any{"image": "x"})
	require.Error(t, err)
	assert.Equal(t, []string{"cid-timeout"}, cli.stopCalls, "failed Open must stop the container")
}

func TestSource_Open_ClonesRepoWhenSet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cli := &fakeCLI{runID: "cid2"}
	s := &Source{cli: cli, urlForContainer: func(_, _ string) string { return srv.URL }}

	_, closer, err := s.Open(context.Background(), map[string]any{
		"image":  "x",
		"repo":   "https://github.com/foo/bar",
		"branch": "main",
	})
	require.NoError(t, err)
	defer func() { _ = closer.Close() }()

	var cloneCall []string
	for _, c := range cli.execCalls {
		for _, arg := range c {
			if arg == "clone" {
				cloneCall = c
			}
		}
	}
	require.NotEmpty(t, cloneCall, "expected a git clone exec")
	assert.Contains(t, cloneCall, "https://github.com/foo/bar")
	assert.Contains(t, cloneCall, "main")
	assert.Contains(t, cloneCall, "/workspace")
}

func TestSource_Open_ContainerStartFailureSurfaces(t *testing.T) {
	cli := &fakeCLI{runErr: errors.New("boom")}
	s := &Source{cli: cli, urlForContainer: func(_, _ string) string { return "http://x" }}

	_, _, err := s.Open(context.Background(), map[string]any{"image": "x"})
	require.Error(t, err)
	assert.Empty(t, cli.stopCalls, "no container to stop when Run failed")
}

func TestSource_Open_PropagatesMountsAndEnv(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cli := &fakeCLI{runID: "cid3"}
	s := &Source{cli: cli, urlForContainer: func(_, _ string) string { return srv.URL }}

	_, closer, err := s.Open(context.Background(), map[string]any{
		"image": "x",
		"env":   map[string]any{"FOO": "bar"},
		"mounts": []any{
			map[string]any{"source": "/host/skills", "target": "/skills/codegen", "readonly": true},
		},
	})
	require.NoError(t, err)
	defer func() { _ = closer.Close() }()

	require.Len(t, cli.runCalls, 1)
	spec := cli.runCalls[0]
	assert.Equal(t, "bar", spec.Env["FOO"])
	require.Len(t, spec.Mounts, 1)
	assert.Equal(t, "/host/skills", spec.Mounts[0].Source)
	assert.Equal(t, "/skills/codegen", spec.Mounts[0].Target)
	assert.True(t, spec.Mounts[0].ReadOnly)
}

func TestMountsFromArgs_TypedSlice(t *testing.T) {
	// buildSkillMounts in engine returns []map[string]any directly (not []any).
	mounts := mountsFromArgs([]map[string]any{
		{"source": "/a", "target": "/b", "readonly": true},
	})
	require.Len(t, mounts, 1)
	assert.Equal(t, "/a", mounts[0].Source)
	assert.True(t, mounts[0].ReadOnly)
}

func TestMountsFromArgs_Nil(t *testing.T) {
	assert.Nil(t, mountsFromArgs(nil))
}
