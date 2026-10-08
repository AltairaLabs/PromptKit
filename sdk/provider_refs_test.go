package sdk

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/composition"
	rtprompt "github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pack"
)

// RFC 0017: a prompt, or a composition prompt/agent step, names the
// requires.providers key that runs it.

// refProvider is a mock LLM that answers with its own id and counts calls, so
// a test can tell which provider ran a call.
type refProvider struct {
	providers.Provider
	mu    sync.Mutex
	calls int
}

func newRefProvider(id string) *refProvider {
	return &refProvider{Provider: mock.NewProviderWithRepository(id, "mock-model", false,
		mock.NewInMemoryMockRepository("answer from "+id))}
}

func (p *refProvider) Predict(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	return p.Provider.Predict(ctx, req)
}

func (p *refProvider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	return p.Provider.PredictStream(ctx, req)
}

// refProvider supports tools, as a workflow state's provider must: the
// transition tool is offered to every state's prompt.
func (p *refProvider) BuildTooling(descriptors []*providers.ToolDescriptor) (providers.ProviderTools, error) {
	return descriptors, nil
}

func (p *refProvider) PredictWithTools(
	ctx context.Context, req providers.PredictionRequest, _ providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	resp, err := p.Predict(ctx, req)
	return resp, nil, err
}

func (p *refProvider) PredictStreamWithTools(
	ctx context.Context, req providers.PredictionRequest, _ providers.ProviderTools, _ string,
) (<-chan providers.StreamChunk, error) {
	return p.PredictStream(ctx, req)
}

func (p *refProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// withPooledProvider binds an LLM instance to its id without making it the
// agent — the instance form of WithNamedProvider.
func withPooledProvider(p providers.Provider) Option {
	return func(c *config) error {
		ensureProviderPool(c)
		c.providers.Register(p)
		return nil
	}
}

// providerRefsPack declares two named LLM requirements, a prompt per routing
// case, a workflow whose second state's prompt names a key, and a composition
// with one step per resolution rule.
const providerRefsPack = `{
	"$schema": "https://promptpack.org/schema/latest/promptpack.schema.json",
	"id": "provider-refs",
	"name": "provider-refs",
	"version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "{{variable}}"},
	"requires": {
		"providers": [
			{"key": "default", "role": "llm", "required": true},
			{"key": "drafter", "role": "llm", "required": true},
			{"key": "reviewer", "role": "llm", "required": true}
		]
	},
	"prompts": {
		"chat":   {"id": "chat",   "name": "chat",   "version": "1.0.0", "system_template": "chat"},
		"draft":  {"id": "draft",  "name": "draft",  "version": "1.0.0", "system_template": "draft", "provider": "drafter"},
		"review": {"id": "review", "name": "review", "version": "1.0.0", "system_template": "review"}
	},
	"workflow": {
		"version": 1,
		"entry": "intake",
		"states": {
			"intake":   {"prompt_task": "chat", "on_event": {"Next": "drafting"}},
			"drafting": {"prompt_task": "draft", "terminal": true}
		}
	},
	"compositions": {
		"flow": {
			"version": 1,
			"steps": [
				{"id": "step_override", "kind": "prompt", "prompt_task": "draft", "provider": "reviewer", "input": "${input}"},
				{"id": "prompt_level",  "kind": "prompt", "prompt_task": "draft", "input": "${input}"},
				{"id": "neither",       "kind": "prompt", "prompt_task": "chat", "input": "${input}"}
			],
			"output": "neither"
		}
	}
}`

type refProviders struct{ agent, drafter, reviewer *refProvider }

func newRefProviders() refProviders {
	return refProviders{newRefProvider("agent"), newRefProvider("drafter"), newRefProvider("reviewer")}
}

func (r refProviders) options() []Option {
	return []Option{WithProvider(r.agent), withPooledProvider(r.drafter), withPooledProvider(r.reviewer)}
}

func TestProviderRefs_PromptRunsOnItsBoundProvider(t *testing.T) {
	packPath := createTestPackFile(t, providerRefsPack)
	ctx := context.Background()

	t.Run("a prompt naming drafter runs on drafter", func(t *testing.T) {
		r := newRefProviders()
		conv, err := Open(packPath, "draft", r.options()...)
		require.NoError(t, err)
		defer conv.Close()

		resp, err := conv.Send(ctx, "hello")
		require.NoError(t, err)
		assert.Equal(t, "answer from drafter", resp.Text())
		assert.Equal(t, 1, r.drafter.callCount())
		assert.Zero(t, r.agent.callCount())
	})
	t.Run("a prompt naming nothing runs on the agent", func(t *testing.T) {
		r := newRefProviders()
		conv, err := Open(packPath, "chat", r.options()...)
		require.NoError(t, err)
		defer conv.Close()

		resp, err := conv.Send(ctx, "hello")
		require.NoError(t, err)
		assert.Equal(t, "answer from agent", resp.Text())
		assert.Zero(t, r.drafter.callCount())
	})
}

func TestProviderRefs_WorkflowStateRunsOnItsPromptsProvider(t *testing.T) {
	packPath := createTestPackFile(t, providerRefsPack)
	r := newRefProviders()
	wc, err := OpenWorkflow(packPath, r.options()...)
	require.NoError(t, err)
	defer wc.Close()
	ctx := context.Background()

	resp, err := wc.Send(ctx, "first")
	require.NoError(t, err)
	assert.Equal(t, "answer from agent", resp.Text(), "intake's prompt names no key")

	_, err = wc.Transition("Next")
	require.NoError(t, err)
	resp, err = wc.Send(ctx, "second")
	require.NoError(t, err)
	assert.Equal(t, "answer from drafter", resp.Text(), "drafting's prompt names drafter")
}

func TestProviderRefs_CompositionStepsResolvePerStep(t *testing.T) {
	packPath := createTestPackFile(t, providerRefsPack)
	r := newRefProviders()
	conv, err := Open(packPath, "chat", r.options()...)
	require.NoError(t, err)
	defer conv.Close()

	comp := conv.pack.Compositions["flow"]
	require.NotNil(t, comp)
	resolve := stepProviderResolver(conv.pack, conv.config)
	want := map[string]string{"step_override": "reviewer", "prompt_level": "drafter", "neither": "agent"}
	for _, step := range comp.Steps {
		prov, err := resolve(step)
		require.NoError(t, err, step.ID)
		assert.Equal(t, want[step.ID], prov.ID(), "step %s", step.ID)
	}
}

// The composition runs through Send, and each step calls the provider its
// rule picks: the step's key, its prompt's key, or the agent.
func TestProviderRefs_CompositionRunsEachStepOnItsProvider(t *testing.T) {
	pack := strings.Replace(providerRefsPack, `"entry": "intake",`, `"entry": "compose",`, 1)
	pack = strings.Replace(pack, `"intake":   {`,
		`"compose": {"orchestration": "composition", "composition": "flow", "terminal": true},
			"intake":   {`, 1)
	packPath := createTestPackFile(t, pack)
	r := newRefProviders()
	wc, err := OpenWorkflow(packPath, r.options()...)
	require.NoError(t, err)
	defer wc.Close()
	require.Equal(t, "compose", wc.CurrentState())

	_, err = wc.Send(context.Background(), "run the flow")
	require.NoError(t, err)
	assert.Equal(t, 1, r.reviewer.callCount(), "step_override names reviewer")
	assert.Equal(t, 1, r.drafter.callCount(), "prompt_level inherits draft's drafter")
	assert.Equal(t, 1, r.agent.callCount(), "neither falls back to the agent")
}

// providerRefsBrokenPack opens "chat", which names no key, so every fault below
// sits in a prompt or step other than the one opened: validation covers the
// whole pack.
func providerRefsBrokenPack(requires, prompts, compositions string) string {
	return `{
	"$schema": "https://promptpack.org/schema/latest/promptpack.schema.json",
	"id": "provider-refs-broken",
	"name": "provider-refs-broken",
	"version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "{{variable}}"},
	"requires": {"providers": [` + requires + `]},
	"prompts": {
		"chat": {"id": "chat", "name": "chat", "version": "1.0.0", "system_template": "chat"}` + prompts + `
	}` + compositions + `
}`
}

// noToolsProvider is an LLM without tool support.
type noToolsProvider struct{ providers.Provider }

func TestProviderRefs_OpenRejectsBadReferences(t *testing.T) {
	const draftNamesDrafter = `,
		"draft": {"id": "draft", "name": "draft", "version": "1.0.0", "system_template": "d", "provider": "drafter"}`

	cases := []struct {
		name    string
		pack    string
		opts    func() []Option
		wantMsg string
	}{
		{
			name:    "undeclared key",
			pack:    providerRefsBrokenPack(`{"key": "default", "role": "llm"}`, draftNamesDrafter, ""),
			opts:    func() []Option { return []Option{WithProvider(newRefProvider("agent"))} },
			wantMsg: `prompt "draft" names provider "drafter", which the pack does not declare in requires`,
		},
		{
			name:    "declared but unbound",
			pack:    providerRefsBrokenPack(`{"key": "drafter", "role": "llm", "required": true}`, draftNamesDrafter, ""),
			opts:    func() []Option { return []Option{WithProvider(newRefProvider("agent"))} },
			wantMsg: `prompt "draft" names provider "drafter", which the pack declares and the host bound nothing to`,
		},
		{
			name: "bound to an inference provider",
			pack: providerRefsBrokenPack(`{"key": "drafter", "role": "llm", "required": true}`, draftNamesDrafter, ""),
			opts: func() []Option {
				return []Option{WithProvider(newRefProvider("agent")), func(c *config) error {
					return c.registerInferenceProvider("drafter", textClassifierStub{})
				}}
			},
			wantMsg: `prompt "draft" names provider "drafter", and the host bound an inference provider to it`,
		},
		{
			name: "agent step bound to a provider without tool support",
			pack: providerRefsBrokenPack(`{"key": "drafter", "role": "llm", "required": true}`, "", `,
	"compositions": {"flow": {"version": 1, "output": "ag",
		"steps": [{"id": "ag", "kind": "agent", "prompt_task": "chat", "provider": "drafter", "input": "${input}",
			"termination": {"max_steps": 3}}]}}`),
			opts: func() []Option {
				return []Option{WithProvider(newRefProvider("agent")),
					withPooledProvider(noToolsProvider{newRefProvider("drafter")})}
			},
			wantMsg: `composition "flow" step "ag" uses tools and names provider "drafter"`,
		},
		{
			name: "prompt with tools bound to a provider without tool support",
			pack: providerRefsBrokenPack(`{"key": "drafter", "role": "llm", "required": true}`, `,
		"draft": {"id": "draft", "name": "draft", "version": "1.0.0", "system_template": "d",
			"provider": "drafter", "tools": ["lookup"]}`, `,
	"tools": {"lookup": {"name": "lookup", "description": "look up", "parameters": {"type": "object", "properties": {}}}}`),
			opts: func() []Option {
				return []Option{WithProvider(newRefProvider("agent")),
					withPooledProvider(noToolsProvider{newRefProvider("drafter")})}
			},
			wantMsg: `prompt "draft" uses tools and names provider "drafter"`,
		},
		{
			name: "key declared for another role",
			pack: providerRefsBrokenPack(`{"key": "drafter", "role": "embedding", "required": true}`,
				draftNamesDrafter, ""),
			opts:    func() []Option { return []Option{WithProvider(newRefProvider("agent"))} },
			wantMsg: `prompt "draft" names provider "drafter", which the pack declares with role "embedding"`,
		},
		{
			name:    "optional requirement a call site names is unbound",
			pack:    providerRefsBrokenPack(`{"key": "drafter", "role": "llm", "required": false}`, draftNamesDrafter, ""),
			opts:    func() []Option { return []Option{WithProvider(newRefProvider("agent"))} },
			wantMsg: `prompt "draft" names provider "drafter", which the pack declares and the host bound nothing to`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			packPath := createTestPackFile(t, tc.pack)
			_, err := Open(packPath, "chat", tc.opts()...)
			require.Error(t, err)
			assert.ErrorIs(t, err, errProviderKeys)
			assert.Contains(t, err.Error(), tc.wantMsg)
		})
	}
}

// A pack with no provider references opens and runs exactly as before.
func TestProviderRefs_PackWithoutReferencesIsUnchanged(t *testing.T) {
	pack := strings.Replace(providerRefsPack, `, "provider": "drafter"`, "", 1)
	pack = strings.Replace(pack, `"provider": "reviewer", `, "", 1)
	packPath := createTestPackFile(t, pack)
	r := newRefProviders()

	conv, err := Open(packPath, "draft", r.options()...)
	require.NoError(t, err)
	defer conv.Close()
	assert.Same(t, r.agent, conv.callProvider())

	resp, err := conv.Send(context.Background(), "hello")
	require.NoError(t, err)
	assert.Equal(t, "answer from agent", resp.Text())
}

// A PackTemplate opens prompts through the same resolution and validation.
func TestProviderRefs_TemplateOpen(t *testing.T) {
	packPath := createTestPackFile(t, providerRefsPack)
	tmpl, err := LoadTemplate(packPath)
	require.NoError(t, err)

	r := newRefProviders()
	conv, err := tmpl.Open("draft", r.options()...)
	require.NoError(t, err)
	defer conv.Close()
	resp, err := conv.Send(context.Background(), "hello")
	require.NoError(t, err)
	assert.Equal(t, "answer from drafter", resp.Text())

	broken, err := LoadTemplate(createTestPackFile(t, providerRefsBrokenPack(
		`{"key": "default", "role": "llm"}`, `,
		"draft": {"id": "draft", "name": "draft", "version": "1.0.0", "system_template": "d", "provider": "drafter"}`, "")))
	require.NoError(t, err)
	_, err = broken.Open("chat", WithProvider(newRefProvider("agent")))
	require.ErrorIs(t, err, errProviderKeys)
	assert.Contains(t, err.Error(), `prompt "draft" names provider "drafter"`)
}

// OpenDuplex streams on the opened prompt's provider: it is the one that must
// support duplex streaming, and the one the session runs on.
func TestProviderRefs_OpenDuplexUsesThePromptsProvider(t *testing.T) {
	// Without the workflow: a state's prompt would need tool support, which the
	// streaming mock does not have.
	noWorkflow := providerRefsPack[:strings.Index(providerRefsPack, `"workflow"`)] +
		providerRefsPack[strings.Index(providerRefsPack, `"compositions"`):]
	packPath := createTestPackFile(t, noWorkflow)

	t.Run("a streaming drafter serves a prompt naming it, under a non-streaming agent", func(t *testing.T) {
		// ASM mode: the session itself needs a StreamInputSupport provider, so it
		// can only open on drafter.
		drafter := mock.NewStreamingProvider("drafter", "mock-model", false)
		conv, err := OpenDuplex(packPath, "draft", WithProvider(newRefProvider("agent")),
			withPooledProvider(drafter), withPooledProvider(newRefProvider("reviewer")),
			WithStreamingConfig(&providers.StreamingInputConfig{Config: types.StreamingMediaConfig{
				Type: types.ContentTypeAudio, SampleRate: 24000, Channels: 1, Encoding: "pcm16", BitDepth: 16,
			}}))
		require.NoError(t, err)
		defer conv.Close()
		assert.Same(t, drafter, conv.callProvider())
	})
	t.Run("a non-streaming drafter is refused even under a streaming agent", func(t *testing.T) {
		_, err := OpenDuplex(packPath, "draft",
			WithProvider(mock.NewStreamingProvider("agent", "mock-model", false)),
			withPooledProvider(newRefProvider("drafter")), withPooledProvider(newRefProvider("reviewer")))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not support duplex streaming")
		assert.Contains(t, err.Error(), "refProvider")
	})
}

// A fork runs on the same provider as the conversation it was forked from.
func TestProviderRefs_ForkKeepsThePromptsProvider(t *testing.T) {
	packPath := createTestPackFile(t, providerRefsPack)
	r := newRefProviders()
	conv, err := Open(packPath, "draft", r.options()...)
	require.NoError(t, err)
	defer conv.Close()

	fork, err := conv.Fork()
	require.NoError(t, err)
	defer fork.Close()
	resp, err := fork.Send(context.Background(), "hello")
	require.NoError(t, err)
	assert.Equal(t, "answer from drafter", resp.Text())
}

// clearDetectableEnv hides every API key provider detection reads, so a test
// sees what Open does with no agent available.
func clearDetectableEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GOOGLE_API_KEY", "GEMINI_API_KEY"} {
		t.Setenv(k, "")
	}
}

// providerRefsAllNamedPack names a key on every prompt, so no call can run on
// the agent provider.
const providerRefsAllNamedPack = `{
	"$schema": "https://promptpack.org/schema/latest/promptpack.schema.json",
	"id": "provider-refs-all-named",
	"name": "provider-refs-all-named",
	"version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "{{variable}}"},
	"requires": {"providers": [{"key": "drafter", "role": "llm", "required": true}]},
	"prompts": {
		"draft": {"id": "draft", "name": "draft", "version": "1.0.0", "system_template": "draft", "provider": "drafter"}
	}
}`

// When every call names a key the host bound, Open needs no agent provider and
// does not go looking for one.
func TestProviderRefs_NoAgentNeededWhenEveryCallNamesAKey(t *testing.T) {
	clearDetectableEnv(t)
	packPath := createTestPackFile(t, providerRefsAllNamedPack)

	drafter := newRefProvider("drafter")
	conv, err := Open(packPath, "draft", withPooledProvider(drafter))
	require.NoError(t, err)
	defer conv.Close()
	assert.Nil(t, conv.config.getAgentProvider(), "nothing was detected or registered as the agent")

	resp, err := conv.Send(context.Background(), "hello")
	require.NoError(t, err)
	assert.Equal(t, "answer from drafter", resp.Text())
}

// A pack in which some call runs on the agent still requires one.
func TestProviderRefs_AgentStillRequiredWhenACallUsesIt(t *testing.T) {
	clearDetectableEnv(t)
	packPath := createTestPackFile(t, providerRefsPack)

	_, err := Open(packPath, "draft", withPooledProvider(newRefProvider("drafter")),
		withPooledProvider(newRefProvider("reviewer")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to detect provider")
}

// With nothing wired at all, the error says so rather than blaming one key.
func TestProviderRefs_NothingWiredAtAll(t *testing.T) {
	clearDetectableEnv(t)
	packPath := createTestPackFile(t, providerRefsAllNamedPack)

	_, err := Open(packPath, "draft")
	require.ErrorIs(t, err, errProviderKeys)
	assert.Contains(t, err.Error(), `prompt "draft" names provider "drafter" and this conversation has no providers wired at all`)
}

// A workflow's first Open checks every call site; only its transitions, which
// re-open the same pack with the same bindings, skip the check.
func TestProviderRefs_OpenWorkflowChecksAtEntry(t *testing.T) {
	packPath := createTestPackFile(t, providerRefsPack)
	_, err := OpenWorkflow(packPath, WithProvider(newRefProvider("agent")),
		withPooledProvider(newRefProvider("reviewer")))
	require.ErrorIs(t, err, errProviderKeys)
	assert.Contains(t, err.Error(), `prompt "draft" names provider "drafter"`,
		"the entry state's prompt names no key; the drafting state's does")
}

func TestPackNeedsAgent(t *testing.T) {
	named := func(key string) *pack.Prompt { return &pack.Prompt{Provider: key} }
	step := func(kind, task, key string, branches ...*composition.Step) *composition.Step {
		return &composition.Step{ID: task, Kind: kind, PromptTask: task, Provider: key, Branches: branches}
	}
	withComp := func(steps ...*composition.Step) *pack.Pack {
		p := &pack.Pack{}
		p.Prompts = map[string]*pack.Prompt{"a": named("drafter")}
		p.Compositions = map[string]*composition.Composition{"c": {Steps: steps}}
		return p
	}

	cases := []struct {
		name string
		pack *pack.Pack
		want bool
	}{
		{"no pack", nil, true},
		{"a prompt names no key", func() *pack.Pack {
			p := &pack.Pack{}
			p.Prompts = map[string]*pack.Prompt{"a": named("drafter"), "b": named("")}
			return p
		}(), true},
		{"a prompt names default", func() *pack.Pack {
			p := &pack.Pack{}
			p.Prompts = map[string]*pack.Prompt{"a": named("default")}
			return p
		}(), true},
		{"every prompt and step named", withComp(step("prompt", "a", "")), false},
		{"a step's prompt is not in the pack", withComp(step("prompt", "missing", "")), true},
		{"a nested agent step resolves to default",
			withComp(step("parallel", "", "", step("agent", "missing", ""))), true},
		{"a nested step names a key", withComp(step("parallel", "", "", step("agent", "missing", "reviewer"))), false},
		{"tool steps never use the agent", withComp(step("tool", "", "")), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, rtprompt.NeedsDefaultProvider(tc.pack))
		})
	}
}

// A malformed requires block is reported by the call-site check, which builds
// it once and returns the same error on every run.
func TestCallProviderCheck_MalformedRequires(t *testing.T) {
	p := &pack.Pack{}
	p.Requires = &rtprompt.Requires{Providers: []*rtprompt.ProviderRequirement{
		{Key: "dup", Role: "llm"}, {Key: "dup", Role: "llm"},
	}}
	p.Prompts = map[string]*pack.Prompt{"a": {Provider: "dup"}}

	check := newCallProviderCheck(p)
	err := check.run(&config{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pack requires")
	assert.Equal(t, err, check.run(&config{}))
}

// A workflow state's prompt that names a key needs a provider with tool
// support even when it declares no tools: the transition tool is offered to
// it, and a mid-turn handoff reaches it with the transition call in history.
func TestProviderRefs_WorkflowStatePromptNeedsToolSupport(t *testing.T) {
	packPath := createTestPackFile(t, providerRefsPack)
	_, err := OpenWorkflow(packPath, WithProvider(newRefProvider("agent")),
		withPooledProvider(noToolsProvider{newRefProvider("drafter")}),
		withPooledProvider(newRefProvider("reviewer")))
	require.ErrorIs(t, err, errProviderKeys)
	assert.Contains(t, err.Error(), `prompt "draft" uses tools and names provider "drafter"`)
}
