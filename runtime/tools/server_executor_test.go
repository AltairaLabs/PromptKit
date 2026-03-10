package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
	// Server that never responds
	script := writeServerScript(t, "server.py", `#!/usr/bin/env python3
import sys, time
for line in sys.stdin:
    time.sleep(60)
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

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
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
