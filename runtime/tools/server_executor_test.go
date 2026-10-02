package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeServerScript(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755))
	return path
}

// jsonRPCServer is a minimal JSON-RPC server script that echoes requests.
const jsonRPCEchoServer = `#!/bin/sh
# Read JSON-RPC requests from stdin, respond with the args as result
while IFS= read -r line; do
  id=$(echo "$line" | python3 -c "import sys,json; print(json.loads(sys.stdin.read())['id'])" 2>/dev/null)
  if [ -z "$id" ]; then
    id=0
  fi
  args=$(echo "$line" | python3 -c "import sys,json; d=json.loads(sys.stdin.read()); print(json.dumps(d.get('params',{}).get('args',{})))" 2>/dev/null)
  echo "{\"jsonrpc\":\"2.0\",\"id\":$id,\"result\":$args}"
done
`

// simpleJSONRPCServer uses a Python one-liner for reliable JSON parsing.
const pythonJSONRPCServer = `#!/usr/bin/env python3
import sys, json
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    req = json.loads(line)
    rid = req.get("id", 0)
    args = req.get("params", {}).get("args", {})
    resp = {"jsonrpc": "2.0", "id": rid, "result": args}
    print(json.dumps(resp), flush=True)
`

const pythonJSONRPCErrorServer = `#!/usr/bin/env python3
import sys, json
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    req = json.loads(line)
    rid = req.get("id", 0)
    resp = {"jsonrpc": "2.0", "id": rid, "error": {"code": -32603, "message": "internal error"}}
    print(json.dumps(resp), flush=True)
`

func TestServerExecutor_Name(t *testing.T) {
	e := &ServerExecutor{}
	assert.Equal(t, "server", e.Name())
}

func TestServerExecutor_NoExecConfig(t *testing.T) {
	e := &ServerExecutor{}
	_, err := e.Execute(context.Background(), &ToolDescriptor{Name: "test"}, json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no exec configuration")
}

func TestServerExecutor_Execute_Success(t *testing.T) {
	script := writeServerScript(t, "server.py", pythonJSONRPCServer)

	e := &ServerExecutor{}
	defer e.Close()

	desc := &ToolDescriptor{
		Name: "echo_tool",
		ExecConfig: &ExecConfig{
			Command: "python3",
			Args:    []string{script},
		},
	}

	result, err := e.Execute(context.Background(), desc, json.RawMessage(`{"city":"NYC"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"city":"NYC"}`, string(result))
}

func TestServerExecutor_Execute_MultipleRequests(t *testing.T) {
	script := writeServerScript(t, "server.py", pythonJSONRPCServer)

	e := &ServerExecutor{}
	defer e.Close()

	desc := &ToolDescriptor{
		Name: "multi_tool",
		ExecConfig: &ExecConfig{
			Command: "python3",
			Args:    []string{script},
		},
	}

	// First request
	r1, err := e.Execute(context.Background(), desc, json.RawMessage(`{"n":1}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"n":1}`, string(r1))

	// Second request (same process)
	r2, err := e.Execute(context.Background(), desc, json.RawMessage(`{"n":2}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"n":2}`, string(r2))
}

func TestServerExecutor_Execute_JSONRPCError(t *testing.T) {
	script := writeServerScript(t, "server.py", pythonJSONRPCErrorServer)

	e := &ServerExecutor{}
	defer e.Close()

	desc := &ToolDescriptor{
		Name: "error_tool",
		ExecConfig: &ExecConfig{
			Command: "python3",
			Args:    []string{script},
		},
	}

	_, err := e.Execute(context.Background(), desc, json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "internal error")
}

func TestServerExecutor_Execute_ProcessStartFailure(t *testing.T) {
	e := &ServerExecutor{}
	defer e.Close()

	desc := &ToolDescriptor{
		Name: "bad_tool",
		ExecConfig: &ExecConfig{
			Command: "/nonexistent/binary",
		},
	}

	_, err := e.Execute(context.Background(), desc, json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "starting process")
}

func TestServerExecutor_Execute_ContextCanceled(t *testing.T) {
	// Server that never responds, but exits as soon as stdin closes so
	// e.Close() doesn't wait out serverShutdownTimeout before killing it.
	script := writeServerScript(t, "server.py", `#!/usr/bin/env python3
import sys
for line in sys.stdin:
    pass
`)

	e := &ServerExecutor{}
	defer e.Close()

	desc := &ToolDescriptor{
		Name: "slow_tool",
		ExecConfig: &ExecConfig{
			Command: "python3",
			Args:    []string{script},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := e.Execute(ctx, desc, json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context")
}

func TestServerExecutor_Close(t *testing.T) {
	script := writeServerScript(t, "server.py", pythonJSONRPCServer)

	e := &ServerExecutor{}

	desc := &ToolDescriptor{
		Name: "close_tool",
		ExecConfig: &ExecConfig{
			Command: "python3",
			Args:    []string{script},
		},
	}

	// Start the process
	_, err := e.Execute(context.Background(), desc, json.RawMessage(`{}`))
	require.NoError(t, err)

	// Close should terminate the process
	require.NoError(t, e.Close())

	// Processes map should be nil after close
	assert.Nil(t, e.processes)
}

func TestServerExecutor_Close_Empty(t *testing.T) {
	e := &ServerExecutor{}
	require.NoError(t, e.Close())
}

func TestServerExecutor_Execute_WithEnv(t *testing.T) {
	script := writeServerScript(t, "server.py", `#!/usr/bin/env python3
import sys, json, os
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    req = json.loads(line)
    rid = req.get("id", 0)
    val = os.environ.get("SERVER_TEST_VAR", "missing")
    resp = {"jsonrpc": "2.0", "id": rid, "result": {"env_val": val}}
    print(json.dumps(resp), flush=True)
`)
	t.Setenv("SERVER_TEST_VAR", "hello_server")

	e := &ServerExecutor{}
	defer e.Close()

	desc := &ToolDescriptor{
		Name: "env_tool",
		ExecConfig: &ExecConfig{
			Command: "python3",
			Args:    []string{script},
			Env:     []string{"SERVER_TEST_VAR"},
		},
	}

	result, err := e.Execute(context.Background(), desc, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"env_val":"hello_server"}`, string(result))
}

func TestServerExecutor_ProcessRestart(t *testing.T) {
	script := writeServerScript(t, "server.py", `#!/usr/bin/env python3
import sys, json
# Only handle one request then exit
line = sys.stdin.readline()
req = json.loads(line)
resp = {"jsonrpc": "2.0", "id": req["id"], "result": {"call": 1}}
print(json.dumps(resp), flush=True)
`)

	e := &ServerExecutor{}
	defer e.Close()

	desc := &ToolDescriptor{
		Name: "restart_tool",
		ExecConfig: &ExecConfig{
			Command: "python3",
			Args:    []string{script},
		},
	}

	// First call succeeds
	r1, err := e.Execute(context.Background(), desc, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"call":1}`, string(r1))
}

func TestParseJSONRPCResponse_Valid(t *testing.T) {
	data := []byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`)
	result, err := parseJSONRPCResponse(data, 1)
	require.NoError(t, err)
	assert.JSONEq(t, `{"ok":true}`, string(result))
}

func TestParseJSONRPCResponse_Error(t *testing.T) {
	data := []byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32603,"message":"boom"}}`)
	_, err := parseJSONRPCResponse(data, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

func TestParseJSONRPCResponse_IDMismatch(t *testing.T) {
	data := []byte(`{"jsonrpc":"2.0","id":99,"result":{}}`)
	_, err := parseJSONRPCResponse(data, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ID mismatch")
}

func TestParseJSONRPCResponse_InvalidJSON(t *testing.T) {
	_, err := parseJSONRPCResponse([]byte(`not json`), 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid JSON-RPC response")
}

func TestParseJSONRPCResponse_NullResult(t *testing.T) {
	data := []byte(`{"jsonrpc":"2.0","id":1}`)
	result, err := parseJSONRPCResponse(data, 1)
	require.NoError(t, err)
	assert.Equal(t, json.RawMessage("null"), result)
}

// pythonServerDesc writes a Python JSON-RPC server script and returns a
// descriptor that runs it.
func pythonServerDesc(t *testing.T, name, script string) *ToolDescriptor {
	t.Helper()
	path := writeServerScript(t, "server.py", script)
	return &ToolDescriptor{
		Name:       name,
		ExecConfig: &ExecConfig{Command: "python3", Args: []string{path}},
	}
}

// pythonSlowFirstServer replies to a request whose args carry "delay" after
// that many seconds, and to any other request at once. A request whose args
// carry "drop" never gets a reply.
const pythonSlowFirstServer = `#!/usr/bin/env python3
import sys, json, time
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    req = json.loads(line)
    args = req.get("params", {}).get("args", {})
    if args.get("drop"):
        continue
    if args.get("delay"):
        time.sleep(args["delay"])
    print(json.dumps({"jsonrpc": "2.0", "id": req["id"], "result": args}), flush=True)
`

func TestServerExecutor_CanceledCallLateReplyDoesNotLeak(t *testing.T) {
	e := &ServerExecutor{}
	defer e.Close()
	desc := pythonServerDesc(t, "late_reply_tool", pythonSlowFirstServer)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := e.Execute(ctx, desc, json.RawMessage(`{"delay":0.3}`))
	require.ErrorIs(t, err, context.DeadlineExceeded)

	// The late reply to the canceled call must not be handed to this call.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	r, err := e.Execute(ctx2, desc, json.RawMessage(`{"n":2}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"n":2}`, string(r))
}

func TestServerExecutor_CanceledCallDoesNotSwallowNextReply(t *testing.T) {
	e := &ServerExecutor{}
	defer e.Close()
	desc := pythonServerDesc(t, "dropped_reply_tool", pythonSlowFirstServer)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := e.Execute(ctx, desc, json.RawMessage(`{"drop":true}`))
	require.ErrorIs(t, err, context.DeadlineExceeded)

	// A reader left behind by the canceled call must not consume this reply.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	r, err := e.Execute(ctx2, desc, json.RawMessage(`{"n":2}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"n":2}`, string(r))
}

func TestServerExecutor_RestartsExitedProcess(t *testing.T) {
	e := &ServerExecutor{}
	defer e.Close()
	// Handles one request, then exits.
	desc := pythonServerDesc(t, "one_shot_tool", `#!/usr/bin/env python3
import sys, json
req = json.loads(sys.stdin.readline())
print(json.dumps({"jsonrpc": "2.0", "id": req["id"], "result": req["params"]["args"]}), flush=True)
`)

	r1, err := e.Execute(context.Background(), desc, json.RawMessage(`{"n":1}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"n":1}`, string(r1))

	e.mu.Lock()
	first := e.processes[desc.Name]
	e.mu.Unlock()
	require.Eventually(t, func() bool { return !first.isRunning() }, 5*time.Second, 10*time.Millisecond,
		"exited server process must be detected")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r2, err := e.Execute(ctx, desc, json.RawMessage(`{"n":2}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"n":2}`, string(r2))

	e.mu.Lock()
	second := e.processes[desc.Name]
	e.mu.Unlock()
	assert.NotSame(t, first, second, "exited process must be replaced")
}

func TestServerExecutor_ConcurrentCallsGetOwnResults(t *testing.T) {
	e := &ServerExecutor{}
	defer e.Close()
	// Reads requests in pairs and answers each pair in reverse order, so a
	// call only completes if another is in flight on the same process and
	// replies are matched by id rather than by arrival order.
	desc := pythonServerDesc(t, "concurrent_tool", `#!/usr/bin/env python3
import sys, json
batch = []
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    batch.append(json.loads(line))
    if len(batch) < 2:
        continue
    for req in reversed(batch):
        print(json.dumps({"jsonrpc": "2.0", "id": req["id"], "result": req["params"]["args"]}), flush=True)
    batch = []
`)

	const calls = 4
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	results := make([]string, calls)
	errs := make([]error, calls)
	var wg sync.WaitGroup
	for i := range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := e.Execute(ctx, desc, json.RawMessage(fmt.Sprintf(`{"n":%d}`, i)))
			results[i], errs[i] = string(r), err
		}()
	}
	wg.Wait()

	for i := range calls {
		require.NoError(t, errs[i], "call %d", i)
		assert.JSONEq(t, fmt.Sprintf(`{"n":%d}`, i), results[i], "call %d", i)
	}
}

func TestServerExecutor_ExitFailsPendingCallWithStderr(t *testing.T) {
	e := &ServerExecutor{}
	defer e.Close()
	desc := pythonServerDesc(t, "crash_tool", `#!/usr/bin/env python3
import sys
sys.stdin.readline()
sys.stderr.write("fatal: boom\n")
sys.exit(3)
`)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := e.Execute(ctx, desc, json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server process exited")
	assert.Contains(t, err.Error(), "exit status 3")
	assert.Contains(t, err.Error(), "fatal: boom")
}

func TestServerExecutor_InvalidResponseFailsPendingCall(t *testing.T) {
	e := &ServerExecutor{}
	defer e.Close()
	desc := pythonServerDesc(t, "garbage_tool", `#!/usr/bin/env python3
import sys
for line in sys.stdin:
    print("not json", flush=True)
`)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := e.Execute(ctx, desc, json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid JSON-RPC response")
}

func TestServerExecutor_ClosedStdoutFailsCall(t *testing.T) {
	e := &ServerExecutor{}
	defer e.Close()
	// Closes stdout but stays alive until stdin closes.
	desc := pythonServerDesc(t, "closed_stdout_tool", `#!/usr/bin/env python3
import os, sys
os.close(1)
for line in sys.stdin:
    pass
`)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := e.Execute(ctx, desc, json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed stdout")
}
