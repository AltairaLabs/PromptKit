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

func writeScript(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755))
	return path
}

func TestExecExecutor_Name(t *testing.T) {
	e := &ExecExecutor{}
	assert.Equal(t, "exec", e.Name())
}

func TestExecExecutor_Execute_Success(t *testing.T) {
	script := writeScript(t, "tool.sh", `#!/bin/sh
cat <<'EOF'
{"result": {"greeting": "hello"}}
EOF
`)

	e := &ExecExecutor{}
	desc := &ToolDescriptor{
		Name: "test-tool",
		ExecConfig: &ExecConfig{
			Command: script,
		},
	}

	result, err := e.Execute(context.Background(), desc, json.RawMessage(`{"name":"world"}`))
	require.NoError(t, err)

	var parsed map[string]string
	require.NoError(t, json.Unmarshal(result, &parsed))
	assert.Equal(t, "hello", parsed["greeting"])
}

func TestExecExecutor_Execute_ErrorResponse(t *testing.T) {
	script := writeScript(t, "tool.sh", `#!/bin/sh
echo '{"error": "not found"}'
`)

	e := &ExecExecutor{}
	desc := &ToolDescriptor{
		Name: "test-tool",
		ExecConfig: &ExecConfig{
			Command: script,
		},
	}

	_, err := e.Execute(context.Background(), desc, json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestExecExecutor_Execute_ProcessFailure(t *testing.T) {
	script := writeScript(t, "tool.sh", `#!/bin/sh
echo "something went wrong" >&2
exit 1
`)

	e := &ExecExecutor{}
	desc := &ToolDescriptor{
		Name: "fail-tool",
		ExecConfig: &ExecConfig{
			Command: script,
		},
	}

	_, err := e.Execute(context.Background(), desc, json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "something went wrong")
}

func TestExecExecutor_Execute_InvalidJSON(t *testing.T) {
	script := writeScript(t, "tool.sh", `#!/bin/sh
echo "not json"
`)

	e := &ExecExecutor{}
	desc := &ToolDescriptor{
		Name: "bad-tool",
		ExecConfig: &ExecConfig{
			Command: script,
		},
	}

	_, err := e.Execute(context.Background(), desc, json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid JSON")
}

func TestExecExecutor_Execute_NoExecConfig(t *testing.T) {
	e := &ExecExecutor{}
	desc := &ToolDescriptor{Name: "no-config"}

	_, err := e.Execute(context.Background(), desc, json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no exec configuration")
}

func TestExecExecutor_Execute_WithArgs(t *testing.T) {
	script := writeScript(t, "tool.sh", `#!/bin/sh
# Echo the first argument as JSON
echo "{\"result\": {\"arg\": \"$1\"}}"
`)

	e := &ExecExecutor{}
	desc := &ToolDescriptor{
		Name: "args-tool",
		ExecConfig: &ExecConfig{
			Command: script,
			Args:    []string{"--verbose"},
		},
	}

	result, err := e.Execute(context.Background(), desc, json.RawMessage(`{}`))
	require.NoError(t, err)

	var parsed map[string]string
	require.NoError(t, json.Unmarshal(result, &parsed))
	assert.Equal(t, "--verbose", parsed["arg"])
}

func TestExecExecutor_Execute_WithEnv(t *testing.T) {
	script := writeScript(t, "tool.sh", `#!/bin/sh
echo "{\"result\": {\"val\": \"$TEST_EXEC_VAR\"}}"
`)

	t.Setenv("TEST_EXEC_VAR", "from_env")

	e := &ExecExecutor{}
	desc := &ToolDescriptor{
		Name: "env-tool",
		ExecConfig: &ExecConfig{
			Command: script,
			Env:     []string{"TEST_EXEC_VAR"},
		},
	}

	result, err := e.Execute(context.Background(), desc, json.RawMessage(`{}`))
	require.NoError(t, err)

	var parsed map[string]string
	require.NoError(t, json.Unmarshal(result, &parsed))
	assert.Equal(t, "from_env", parsed["val"])
}

func TestExecExecutor_Execute_EnvIsolation(t *testing.T) {
	// Set a "secret" env var that is NOT in the envNames list.
	// The subprocess must NOT see it.
	script := writeScript(t, "tool.sh", `#!/bin/sh
# Print the secret env var — should be empty when properly isolated
echo "{\"result\": {\"secret\": \"$EXEC_TEST_SECRET\", \"allowed\": \"$EXEC_TEST_ALLOWED\"}}"
`)

	t.Setenv("EXEC_TEST_SECRET", "s3cr3t")
	t.Setenv("EXEC_TEST_ALLOWED", "visible")

	e := &ExecExecutor{}
	desc := &ToolDescriptor{
		Name: "env-isolation-tool",
		ExecConfig: &ExecConfig{
			Command: script,
			Env:     []string{"EXEC_TEST_ALLOWED"}, // only this one should be passed
		},
	}

	result, err := e.Execute(context.Background(), desc, json.RawMessage(`{}`))
	require.NoError(t, err)

	var parsed map[string]string
	require.NoError(t, json.Unmarshal(result, &parsed))
	assert.Equal(t, "visible", parsed["allowed"], "explicitly allowed env var should be present")
	assert.Empty(t, parsed["secret"], "env var not in envNames must NOT leak to subprocess")
}

func TestExecExecutor_Execute_ContextTimeout(t *testing.T) {
	script := writeScript(t, "tool.sh", `#!/bin/sh
sleep 10
echo '{"result": {}}'
`)

	e := &ExecExecutor{}
	desc := &ToolDescriptor{
		Name: "slow-tool",
		ExecConfig: &ExecConfig{
			Command: script,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := e.Execute(ctx, desc, json.RawMessage(`{}`))
	require.Error(t, err)
}

func TestExecExecutor_Execute_FullResponse(t *testing.T) {
	// When no "result" field, return full stdout
	script := writeScript(t, "tool.sh", `#!/bin/sh
echo '{"status": "ok", "count": 42}'
`)

	e := &ExecExecutor{}
	desc := &ToolDescriptor{
		Name: "raw-tool",
		ExecConfig: &ExecConfig{
			Command: script,
		},
	}

	result, err := e.Execute(context.Background(), desc, json.RawMessage(`{}`))
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(result, &parsed))
	assert.Equal(t, "ok", parsed["status"])
}

func TestExecExecutor_Execute_ReadsStdin(t *testing.T) {
	script := writeScript(t, "tool.sh", `#!/bin/sh
# Read stdin and return it as the result
INPUT=$(cat)
echo "{\"result\": $INPUT}"
`)

	e := &ExecExecutor{}
	desc := &ToolDescriptor{
		Name: "echo-tool",
		ExecConfig: &ExecConfig{
			Command: script,
		},
	}

	args := json.RawMessage(`{"key":"value"}`)
	result, err := e.Execute(context.Background(), desc, args)
	require.NoError(t, err)

	// The script returns the full stdin as result — which is {"args":{"key":"value"}}
	var parsed map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(result, &parsed))
	assert.Contains(t, string(parsed["args"]), "key")
}
