package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

type evictionExec struct{ name string }

func (e *evictionExec) Name() string { return e.name }

func (e *evictionExec) Execute(
	_ context.Context, _ *ToolDescriptor, _ json.RawMessage,
) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	logger.SetLogger(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { logger.SetLogger(nil) })
	return &buf
}

// Registering an executor under a name already taken replaces it -- a plain map
// assignment, with the previous executor simply gone. It used to happen without
// a word, which is why every bug in #2011 was invisible: two conversations each
// believed they had installed the executor serving their tool calls, and the
// loser silently began receiving the winner's answers along with the winner's
// per-conversation state.
//
// Nothing in PromptKit re-registers a name on one registry, so this fires only
// on that mistake.
func TestRegisterExecutor_WarnsWhenItReplacesAnother(t *testing.T) {
	buf := captureWarnings(t)
	r := NewRegistry()

	r.RegisterExecutor(&evictionExec{name: "memory"})
	r.RegisterExecutor(&tagExec{name: "memory", tag: "second"})

	out := buf.String()
	require.Contains(t, out, "tool executor replaced",
		"replacing an executor must not be silent")
	assert.Contains(t, out, `"executor":"memory"`, "the warning must name the executor")
	assert.Contains(t, out, "evictionExec", "and the one being displaced")
	assert.Contains(t, out, "tagExec", "and the one displacing it")
}

// Registering distinct names is the normal case and stays quiet.
func TestRegisterExecutor_QuietForDistinctNames(t *testing.T) {
	buf := captureWarnings(t)
	r := NewRegistry()

	r.RegisterExecutor(&evictionExec{name: "memory"})
	r.RegisterExecutor(&evictionExec{name: "skill"})
	r.RegisterExecutor(&evictionExec{name: "local"})

	assert.NotContains(t, buf.String(), "tool executor replaced")
}

// Re-registering the SAME instance is a no-op, not a collision: it means one
// owner ran its registration twice, which displaces nothing.
func TestRegisterExecutor_QuietWhenReRegisteringTheSameInstance(t *testing.T) {
	buf := captureWarnings(t)
	r := NewRegistry()

	exec := &evictionExec{name: "memory"}
	r.RegisterExecutor(exec)
	r.RegisterExecutor(exec)

	assert.NotContains(t, buf.String(), "tool executor replaced")
}

// A child registry shadowing a parent's executor is the supported way to give
// each conversation its own, so it must not warn -- the parent's entry is
// untouched.
func TestRegisterExecutor_QuietWhenAChildShadowsTheParent(t *testing.T) {
	parent := NewRegistry()
	parent.RegisterExecutor(&evictionExec{name: "local"})

	buf := captureWarnings(t)
	parent.Child().RegisterExecutor(&evictionExec{name: "local"})

	assert.NotContains(t, buf.String(), "tool executor replaced",
		"Child() is the prescribed fix and must not report itself as the problem")
}

// A nil executor is ignored rather than panicking on Name(), and registers
// nothing.
func TestRegisterExecutor_NilIsANoOp(t *testing.T) {
	r := NewRegistry()
	r.RegisterExecutor(&evictionExec{name: "local"})

	r.RegisterExecutor(nil)

	got, ok := r.lookupExecutor("local")
	require.True(t, ok, "a nil registration must not clear what is there")
	assert.Equal(t, "local", got.Name())
}
