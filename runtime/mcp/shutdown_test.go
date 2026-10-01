package mcp

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	// Fake servers like `sleep` ignore stdin; a short grace keeps the suite
	// fast without changing the shutdown sequence under test.
	stdioShutdownGrace = 200 * time.Millisecond
	os.Exit(m.Run())
}

// startShutdownClient runs script under sh as the client's server process.
func startShutdownClient(t *testing.T, script string) *StdioClient {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell and signals")
	}
	c := NewStdioClientWithOptions(ServerConfig{Name: "shutdown", Command: "sh", Args: []string{"-c", script}},
		DefaultClientOptions())
	require.NoError(t, c.startProcess())
	c.started = true
	return c
}

func TestClose_ServerThatExitsOnEOFIsNotSignalled(t *testing.T) {
	// basic/lifecycle (stdio): the client closes the server's input and lets
	// it exit; a well-behaved server is never signalled.
	marker := filepath.Join(t.TempDir(), "flushed")
	c := startShutdownClient(t, `trap 'exit 3' TERM; cat >/dev/null; echo ok > `+marker)
	require.NoError(t, c.Close())
	assert.FileExists(t, marker, "the server ran its own shutdown after stdin closed")
	assert.Equal(t, 0, c.cmd.ProcessState.ExitCode(), "exited on its own, not by signal")
}

func TestClose_ServerIgnoringEOFGetsSIGTERM(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "terminated")
	c := startShutdownClient(t, `trap 'echo term > `+marker+`; exit 0' TERM; while :; do sleep 0.05; done`)
	require.NoError(t, c.Close())
	assert.FileExists(t, marker, "SIGTERM came before any SIGKILL")
}

func TestClose_ServerIgnoringSIGTERMIsKilled(t *testing.T) {
	c := startShutdownClient(t, `trap '' TERM; while :; do sleep 0.05; done`)
	start := time.Now()
	require.NoError(t, c.Close())
	assert.Less(t, time.Since(start), 5*time.Second)
	require.NotNil(t, c.cmd.ProcessState)
	assert.False(t, c.cmd.ProcessState.Success(), "the process was killed")
}
