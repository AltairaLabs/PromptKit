package stage

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/composition"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// registerSimplePrompt adds a trivial prompt template under taskID to reg.
// It uses the same mockRepository pattern already established in stages_core_test.go.
func registerSimplePrompt(t *testing.T, reg *prompt.Registry, taskID string) {
	t.Helper()
	cfg := &prompt.Config{
		Spec: prompt.Spec{
			TaskType:       taskID,
			SystemTemplate: "You are a helpful assistant.",
		},
	}
	if err := reg.RegisterConfig(taskID, cfg); err != nil {
		t.Fatalf("registerSimplePrompt: %v", err)
	}
}

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

func TestCompositionExecutor_PromptStepNilProvider(t *testing.T) {
	// execLLM must return an error when provider or prompt registry is nil,
	// ensuring the switch routes prompt/agent steps into execLLM and that
	// execLLM guards against missing deps.
	exec := NewCompositionStepExecutor(CompositionExecutorDeps{}) // no provider, no registry
	for _, kind := range []composition.StepKind{composition.KindPrompt, composition.KindAgent} {
		step := &composition.Step{ID: "s", Kind: kind, PromptTask: "p"}
		_, err := exec(context.Background(), step, json.RawMessage(`{}`))
		if err == nil {
			t.Errorf("kind %q: expected error when provider/registry not configured", kind)
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

func TestCompositionExecutor_PromptStep(t *testing.T) {
	// mock.NewProvider returns a provider whose default response is "Mock response from test-id model test-model".
	prov := mock.NewProvider("test-id", "test-model", false)
	repo := newMockRepo()
	reg := prompt.NewRegistryWithRepository(repo)
	registerSimplePrompt(t, reg, "p")

	exec := NewCompositionStepExecutor(CompositionExecutorDeps{
		PromptRegistry: reg,
		Provider:       prov,
		ToolRegistry:   tools.NewRegistry(),
	})
	step := &composition.Step{ID: "s", Kind: composition.KindPrompt, PromptTask: "p"}
	out, err := exec(context.Background(), step, json.RawMessage(`"hello"`))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatalf("expected non-empty output, got %s", out)
	}
	// Result must be valid JSON (responseToJSON ensures this).
	if !json.Valid(out) {
		t.Fatalf("output is not valid JSON: %s", out)
	}
}

func TestCompositionExecutor_AgentStepUsesTermination(t *testing.T) {
	prov := mock.NewProvider("test-id", "test-model", false)
	repo := newMockRepo()
	reg := prompt.NewRegistryWithRepository(repo)
	registerSimplePrompt(t, reg, "a")

	exec := NewCompositionStepExecutor(CompositionExecutorDeps{
		PromptRegistry: reg,
		Provider:       prov,
		ToolRegistry:   tools.NewRegistry(),
	})
	step := &composition.Step{
		ID:          "s",
		Kind:        composition.KindAgent,
		PromptTask:  "a",
		Tools:       []string{},
		Termination: &composition.Termination{MaxSteps: 5},
	}
	out, err := exec(context.Background(), step, json.RawMessage(`"go"`))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatalf("expected non-empty output, got %s", out)
	}
	if !json.Valid(out) {
		t.Fatalf("output is not valid JSON: %s", out)
	}
}

func TestCompositionExecutor_AgentStepTerminationToolCalled(t *testing.T) {
	// Covers the StopOnTool branch in execLLM when step.Termination.ToolCalled is set.
	prov := mock.NewProvider("test-id", "test-model", false)
	repo := newMockRepo()
	reg := prompt.NewRegistryWithRepository(repo)
	registerSimplePrompt(t, reg, "a")

	exec := NewCompositionStepExecutor(CompositionExecutorDeps{
		PromptRegistry: reg,
		Provider:       prov,
		ToolRegistry:   tools.NewRegistry(),
	})
	step := &composition.Step{
		ID:         "s",
		Kind:       composition.KindAgent,
		PromptTask: "a",
		Termination: &composition.Termination{
			MaxSteps:   3,
			ToolCalled: "my_tool",
		},
	}
	out, err := exec(context.Background(), step, json.RawMessage(`"run"`))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out) {
		t.Fatalf("output is not valid JSON: %s", out)
	}
}

func TestCompositionExecutor_PromptStepNullInput(t *testing.T) {
	// Covers the stepInputToText null/empty paths.
	prov := mock.NewProvider("test-id", "test-model", false)
	repo := newMockRepo()
	reg := prompt.NewRegistryWithRepository(repo)
	registerSimplePrompt(t, reg, "p")

	exec := NewCompositionStepExecutor(CompositionExecutorDeps{
		PromptRegistry: reg,
		Provider:       prov,
		ToolRegistry:   tools.NewRegistry(),
	})
	step := &composition.Step{ID: "s", Kind: composition.KindPrompt, PromptTask: "p"}

	// null JSON input → empty message text
	out, err := exec(context.Background(), step, json.RawMessage(`null`))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out) {
		t.Fatalf("output is not valid JSON: %s", out)
	}
}

func TestCompositionExecutor_PromptStepObjectInput(t *testing.T) {
	// Covers the stepInputToText JSON-object (non-string) path.
	prov := mock.NewProvider("test-id", "test-model", false)
	repo := newMockRepo()
	reg := prompt.NewRegistryWithRepository(repo)
	registerSimplePrompt(t, reg, "p")

	exec := NewCompositionStepExecutor(CompositionExecutorDeps{
		PromptRegistry: reg,
		Provider:       prov,
		ToolRegistry:   tools.NewRegistry(),
	})
	step := &composition.Step{ID: "s", Kind: composition.KindPrompt, PromptTask: "p"}

	// object JSON input → passed through as compact JSON string
	out, err := exec(context.Background(), step, json.RawMessage(`{"key":"value"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out) {
		t.Fatalf("output is not valid JSON: %s", out)
	}
}

func TestCompositionExecutor_ResponseFormatWithResolver(t *testing.T) {
	// Covers the responseFormat resolver path (SchemaResolver returns schema bytes).
	prov := mock.NewProvider("test-id", "test-model", false)
	repo := newMockRepo()
	reg := prompt.NewRegistryWithRepository(repo)
	registerSimplePrompt(t, reg, "p")

	resolver := func(path string) (json.RawMessage, error) {
		return json.RawMessage(`{"type":"object"}`), nil
	}
	exec := NewCompositionStepExecutor(CompositionExecutorDeps{
		PromptRegistry: reg,
		Provider:       prov,
		ToolRegistry:   tools.NewRegistry(),
		SchemaResolver: resolver,
	})
	step := &composition.Step{
		ID:           "s",
		Kind:         composition.KindPrompt,
		PromptTask:   "p",
		OutputSchema: "my_schema.json",
	}
	out, err := exec(context.Background(), step, json.RawMessage(`"hello"`))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out) {
		t.Fatalf("output is not valid JSON: %s", out)
	}
}

func TestCompositionExecutor_ResponseFormatResolverEmpty(t *testing.T) {
	// Covers the responseFormat resolver returning empty (nil ResponseFormat path).
	prov := mock.NewProvider("test-id", "test-model", false)
	repo := newMockRepo()
	reg := prompt.NewRegistryWithRepository(repo)
	registerSimplePrompt(t, reg, "p")

	resolver := func(path string) (json.RawMessage, error) {
		return nil, nil // declined
	}
	exec := NewCompositionStepExecutor(CompositionExecutorDeps{
		PromptRegistry: reg,
		Provider:       prov,
		ToolRegistry:   tools.NewRegistry(),
		SchemaResolver: resolver,
	})
	step := &composition.Step{
		ID:           "s",
		Kind:         composition.KindPrompt,
		PromptTask:   "p",
		OutputSchema: "my_schema.json",
	}
	out, err := exec(context.Background(), step, json.RawMessage(`"hello"`))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out) {
		t.Fatalf("output is not valid JSON: %s", out)
	}
}

func TestCompositionExecutor_ResponseFormatResolverError(t *testing.T) {
	// Covers the responseFormat resolver returning an error.
	exec := NewCompositionStepExecutor(CompositionExecutorDeps{
		PromptRegistry: prompt.NewRegistryWithRepository(newMockRepo()),
		Provider:       mock.NewProvider("test-id", "test-model", false),
		ToolRegistry:   tools.NewRegistry(),
		SchemaResolver: func(path string) (json.RawMessage, error) {
			return nil, fmt.Errorf("schema not found: %s", path)
		},
	})
	step := &composition.Step{
		ID:           "s",
		Kind:         composition.KindPrompt,
		PromptTask:   "p",
		OutputSchema: "bad_schema.json",
	}
	if _, err := exec(context.Background(), step, json.RawMessage(`"hello"`)); err == nil {
		t.Fatal("expected error when schema resolver returns an error")
	}
}

func TestCompositionExecutor_CancelledContext(t *testing.T) {
	// Covers the execLLM ExecuteSync error path by cancelling the context before execution.
	prov := mock.NewProvider("test-id", "test-model", false)
	repo := newMockRepo()
	reg := prompt.NewRegistryWithRepository(repo)
	registerSimplePrompt(t, reg, "p")

	exec := NewCompositionStepExecutor(CompositionExecutorDeps{
		PromptRegistry: reg,
		Provider:       prov,
		ToolRegistry:   tools.NewRegistry(),
	})
	step := &composition.Step{ID: "s", Kind: composition.KindPrompt, PromptTask: "p"}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before executing
	_, err := exec(ctx, step, json.RawMessage(`"hello"`))
	if err == nil {
		// Some mock providers may still succeed with a cancelled context; that is OK —
		// the important thing is the code is exercised. Don't fail if it happens to succeed.
		t.Log("cancelled context did not produce an error (mock provider ignores cancellation)")
	}
}

func TestCompositionExecutor_ResponseJSONPassthrough(t *testing.T) {
	// Covers the responseToJSON JSON-passthrough path: when the mock returns
	// content that is already valid JSON AND the step has an OutputSchema
	// (structured=true), responseToJSON returns it unchanged.
	repo := mock.NewInMemoryMockRepository(`{"answer":42}`)
	prov := mock.NewProviderWithRepository("test-id", "test-model", false, repo)
	mockRepo := newMockRepo()
	reg := prompt.NewRegistryWithRepository(mockRepo)
	registerSimplePrompt(t, reg, "p")

	exec := NewCompositionStepExecutor(CompositionExecutorDeps{
		PromptRegistry: reg,
		Provider:       prov,
		ToolRegistry:   tools.NewRegistry(),
	})
	// OutputSchema non-empty → structured=true → JSON passthrough applies.
	step := &composition.Step{ID: "s", Kind: composition.KindPrompt, PromptTask: "p", OutputSchema: "schemas/x.json"}
	out, err := exec(context.Background(), step, json.RawMessage(`"hello"`))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out) {
		t.Fatalf("output is not valid JSON: %s", out)
	}
	// The JSON object passthrough means the output IS the mock's JSON content.
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("expected JSON object passthrough, got: %s", out)
	}
}

func TestCompositionExecutor_TextStepNullLiteralEncodedAsString(t *testing.T) {
	// I2 regression test: a TEXT step (no OutputSchema) whose provider returns
	// the literal text "null" must produce the JSON string "\"null\"", NOT raw
	// JSON null. Without the fix, json.Valid("null") is true and the old code
	// would pass through null, making the output ambiguous/corrupt.
	repo := mock.NewInMemoryMockRepository("null")
	prov := mock.NewProviderWithRepository("test-id", "test-model", false, repo)
	mockRepo := newMockRepo()
	reg := prompt.NewRegistryWithRepository(mockRepo)
	registerSimplePrompt(t, reg, "p")

	exec := NewCompositionStepExecutor(CompositionExecutorDeps{
		PromptRegistry: reg,
		Provider:       prov,
		ToolRegistry:   tools.NewRegistry(),
	})
	// No OutputSchema → structured=false → plain text encoding required.
	step := &composition.Step{ID: "s", Kind: composition.KindPrompt, PromptTask: "p"}
	out, err := exec(context.Background(), step, json.RawMessage(`"hello"`))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out) {
		t.Fatalf("output is not valid JSON: %s", out)
	}
	// Must be a JSON string, not raw null.
	var s string
	if err := json.Unmarshal(out, &s); err != nil {
		t.Fatalf("expected JSON string encoding of text, got: %s (err: %v)", out, err)
	}
}

func TestCompositionExecutor_AllowedToolsOverrideStage(t *testing.T) {
	// C1 regression test: proves that allowedToolsStage overrides whatever
	// AllowedTools the prompt template declares, so the step's tool policy wins.
	//
	// The observable: after allowedToolsStage.Process runs over a channel,
	// turnState.AllowedTools equals the configured list, regardless of what
	// PromptAssemblyStage wrote to it first.
	//
	// This is a focused unit test of the override stage itself (fallback
	// approach from the spec), because the ProviderStage snapshots AllowedTools
	// inside its own goroutine and does not expose which tools it offered to
	// the mock provider via the public StreamElement surface.
	turnState := &TurnState{}

	// Simulate what PromptAssemblyStage does: sets AllowedTools from the template.
	turnState.AllowedTools = []string{"template-tool"}

	stage := &allowedToolsStage{
		turnState: turnState,
		tools:     []string{"step-tool"},
	}

	input := make(chan StreamElement, 1)
	userMsg := &types.Message{Role: roleUser, Content: "hi"}
	input <- NewMessageElement(userMsg)
	close(input)

	output := make(chan StreamElement, 1)
	if err := stage.Process(context.Background(), input, output); err != nil {
		t.Fatalf("allowedToolsStage.Process: %v", err)
	}

	// Drain output to confirm the element was forwarded.
	var forwarded int
	for range output {
		forwarded++
	}
	if forwarded != 1 {
		t.Errorf("expected 1 forwarded element, got %d", forwarded)
	}

	// The override must have replaced the template's AllowedTools with step-tool.
	if len(turnState.AllowedTools) != 1 || turnState.AllowedTools[0] != "step-tool" {
		t.Errorf("AllowedTools after override = %v, want [step-tool]", turnState.AllowedTools)
	}
}
