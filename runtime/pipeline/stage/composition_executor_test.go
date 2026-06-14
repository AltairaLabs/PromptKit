package stage

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/composition"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// echoExecutor is a local tools.Executor that returns its args back as the result.
type echoExecutor struct{}

func (e *echoExecutor) Name() string { return "echo-exec" }

func (e *echoExecutor) Execute(_ context.Context, _ *tools.ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
	return args, nil
}

// failExecutor is a local tools.Executor whose result carries a non-empty Error string.
type failExecutor struct{ msg string }

func (f *failExecutor) Name() string { return "fail-exec" }

func (f *failExecutor) Execute(_ context.Context, _ *tools.ToolDescriptor, _ json.RawMessage) (json.RawMessage, error) {
	// Return valid JSON with an empty result; the caller must inspect ToolResult.Error.
	// Registry.Execute wraps executor errors as ToolResult.Error, but for a failure
	// we need the ToolResult.Error path, not the Go-error path.
	// Easiest: panic so we know if this is ever called wrong; but to get ToolResult.Error
	// we rely on the registry's error wrapping: return a Go error which the registry
	// turns into ToolResult{Error: <msg>}.
	return nil, &toolExecError{f.msg}
}

// toolExecError satisfies the error interface for failExecutor.
type toolExecError struct{ msg string }

func (e *toolExecError) Error() string { return e.msg }

// registerEchoTool registers a tool named toolName backed by echoExecutor.
func registerEchoTool(t *testing.T, reg *tools.Registry, toolName string) {
	t.Helper()
	exec := &echoExecutor{}
	reg.RegisterExecutor(exec)
	if err := reg.Register(&tools.ToolDescriptor{
		Name:        toolName,
		Description: "echo tool",
		Mode:        exec.Name(),
		InputSchema: []byte(`{"type":"object"}`),
	}); err != nil {
		t.Fatalf("registerEchoTool: %v", err)
	}
}

// registerFailingTool registers a tool named toolName whose execution returns an error,
// which the registry surfaces as ToolResult.Error.
func registerFailingTool(t *testing.T, reg *tools.Registry, toolName string) {
	t.Helper()
	exec := &failExecutor{msg: "kaboom"}
	reg.RegisterExecutor(exec)
	if err := reg.Register(&tools.ToolDescriptor{
		Name:        toolName,
		Description: "failing tool",
		Mode:        exec.Name(),
		InputSchema: []byte(`{"type":"object"}`),
	}); err != nil {
		t.Fatalf("registerFailingTool: %v", err)
	}
}

func TestCompositionExecutor_ToolStep(t *testing.T) {
	reg := tools.NewRegistry()
	registerEchoTool(t, reg, "echo")

	exec := NewCompositionStepExecutor(CompositionExecutorDeps{ToolRegistry: reg})

	step := &composition.Step{ID: "t", Kind: composition.KindTool, Tool: "echo"}
	out, err := exec(context.Background(), step, json.RawMessage(`{"x":1}`))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got["x"] != float64(1) {
		t.Errorf("echo tool output = %#v", got)
	}
}

func TestCompositionExecutor_ToolStepError(t *testing.T) {
	reg := tools.NewRegistry()
	registerFailingTool(t, reg, "boom")

	exec := NewCompositionStepExecutor(CompositionExecutorDeps{ToolRegistry: reg})
	step := &composition.Step{ID: "t", Kind: composition.KindTool, Tool: "boom"}
	if _, err := exec(context.Background(), step, json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected error when tool reports an error")
	}
}

func TestCompositionExecutor_ToolStepNotFound(t *testing.T) {
	// Registry.Execute returns a Go error (not ToolResult.Error) when the tool
	// is not registered, exercising the err != nil branch in executeTool.
	exec := NewCompositionStepExecutor(CompositionExecutorDeps{ToolRegistry: tools.NewRegistry()})
	step := &composition.Step{ID: "t", Kind: composition.KindTool, Tool: "no-such-tool"}
	if _, err := exec(context.Background(), step, json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected error for unregistered tool")
	}
}

func TestCompositionExecutor_ToolStepNilRegistry(t *testing.T) {
	exec := NewCompositionStepExecutor(CompositionExecutorDeps{}) // no ToolRegistry
	step := &composition.Step{ID: "t", Kind: composition.KindTool, Tool: "echo"}
	if _, err := exec(context.Background(), step, json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected error when tool registry is nil")
	}
}

func TestCompositionExecutor_PromptStepStub(t *testing.T) {
	// Until Task 3 wires the real sub-pipeline, prompt/agent steps return a
	// "not yet implemented" error. This test documents that contract and ensures
	// the switch routes correctly.
	exec := NewCompositionStepExecutor(CompositionExecutorDeps{})
	for _, kind := range []composition.StepKind{composition.KindPrompt, composition.KindAgent} {
		step := &composition.Step{ID: "s", Kind: kind, PromptTask: "p"}
		_, err := exec(context.Background(), step, json.RawMessage(`{}`))
		if err == nil {
			t.Errorf("kind %q: expected error from stub execLLM", kind)
		}
	}
}

func TestCompositionExecutor_UnknownKind(t *testing.T) {
	exec := NewCompositionStepExecutor(CompositionExecutorDeps{})
	step := &composition.Step{ID: "s", Kind: "bogus"}
	if _, err := exec(context.Background(), step, json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected error for unknown step kind")
	}
}
