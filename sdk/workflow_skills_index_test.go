package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/skills"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/runtime/v2/workflow"
	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pack"
)

// These cover #2326. A workflow state's `skills` filter restricts which skills
// can activate (#2314), but the skill__activate description — the index the
// model discovers skills from — kept listing every skill, so the model was
// steered toward skills activation would refuse. Every assertion here is on
// the tools the provider was handed for a round, not on executor state.

// activateOffer is what one provider round was told about skill__activate.
type activateOffer struct {
	offered     bool
	description string
}

// indexRecordingProvider plays a script of one tool call per round, then
// answers with text, and records the skill__activate descriptor each round
// received.
type indexRecordingProvider struct {
	*mock.ToolProvider

	mu     sync.Mutex
	script []types.MessageToolCall
	rounds []activateOffer
}

func newIndexRecordingProvider(script ...types.MessageToolCall) *indexRecordingProvider {
	return &indexRecordingProvider{
		ToolProvider: mock.NewToolProvider("index", "index-model", false, nil),
		script:       script,
	}
}

// Pin to the unary tool loop for determinism.
func (p *indexRecordingProvider) SupportsStreaming() bool { return false }

func (p *indexRecordingProvider) PredictWithTools(
	_ context.Context, _ providers.PredictionRequest, tools providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	offer := activateOffer{}
	descriptors, _ := tools.([]*providers.ToolDescriptor)
	for _, d := range descriptors {
		if d.Name == skills.SkillActivateTool {
			offer = activateOffer{offered: true, description: d.Description}
		}
	}

	p.mu.Lock()
	p.rounds = append(p.rounds, offer)
	round := len(p.rounds)
	p.mu.Unlock()

	if round <= len(p.script) {
		calls := []types.MessageToolCall{p.script[round-1]}
		return providers.PredictionResponse{ToolCalls: calls}, calls, nil
	}
	return providers.PredictionResponse{Content: "done"}, nil, nil
}

func (p *indexRecordingProvider) offers() []activateOffer {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]activateOffer(nil), p.rounds...)
}

// assertIndexLists checks a round offered skill__activate and that its index
// lists exactly the named skills of x and y.
func assertIndexLists(t *testing.T, offer activateOffer, round string, listed ...string) {
	t.Helper()
	require.True(t, offer.offered, "%s: skill__activate should be offered", round)
	want := map[string]bool{}
	for _, name := range listed {
		want[name] = true
	}
	for _, name := range []string{"x", "y"} {
		line := fmt.Sprintf("- %s: desc-%s", name, name)
		if want[name] {
			assert.Contains(t, offer.description, line, "%s: the index should list %s", round, name)
		} else {
			assert.NotContains(t, offer.description, line, "%s: the index should not list %s", round, name)
		}
	}
}

// writeSkillIndexPack writes a workflow pack over skills x and y:
//
//	allow_x  skills: <dir>/billing  (x only)
//	locked   skills: none           (nothing)
//	open     (no skills field)      (everything)
//
// With sharedPrompt every state runs the same prompt, so a mid-turn transition
// changes nothing about the round except what the skills filter admits.
func writeSkillIndexPack(t *testing.T, sharedPrompt bool) string {
	t.Helper()
	skillsDir := t.TempDir()
	writeSkillFile(t, filepath.Join(skillsDir, "billing"), "x")
	writeSkillFile(t, skillsDir, "y")

	quote := func(s string) string {
		b, err := json.Marshal(s)
		require.NoError(t, err)
		return string(b)
	}
	prompt := func(id string) string {
		return fmt.Sprintf(`%q: {"id": %q, "name": %q, "description": "d", "version": "1.0.0",
			"system_template": "You are %s."}`, id, id, id, id)
	}
	pa, pb, pc := "pa", "pb", "pc"
	if sharedPrompt {
		pb, pc = pa, pa
	}
	content := fmt.Sprintf(`{
		"$schema": "https://promptpack.org/schema/2025.1/promptpack.schema.json",
		"schema_version": "2025.1",
		"id": "workflow-skills-index",
		"version": "1.0.0",
		"template_engine": {"version": "v1", "syntax": "handlebars", "features": []},
		"skills": [%s],
		"prompts": {%s, %s, %s},
		"workflow": {
			"version": 1,
			"entry": "allow_x",
			"states": {
				"allow_x": {"prompt_task": %q, "skills": %s, "on_event": {"Lock": "locked", "Open": "open"}},
				"locked":  {"prompt_task": %q, "skills": "none", "on_event": {"Back": "allow_x"}},
				"open":    {"prompt_task": %q, "on_event": {"Back": "allow_x"}}
			}
		}
	}`, quote(skillsDir), prompt("pa"), prompt("pb"), prompt("pc"),
		pa, quote(filepath.Join(skillsDir, "billing")), pb, pc)
	return writeWorkflowTestPack(t, content)
}

func openIndexWorkflow(t *testing.T, packPath string, prov *indexRecordingProvider) *WorkflowConversation {
	t.Helper()
	wc, err := OpenWorkflow(packPath, WithSkipSchemaValidation(), WithProvider(prov))
	require.NoError(t, err)
	t.Cleanup(func() { _ = wc.Close() })
	return wc
}

// The index lists only what the state admits on open, and each conversation a
// transition opens lists what its state admits: everything in open, nothing —
// not even the tool — in locked.
func TestSkillIndex_ListsWhatEachStateAdmits(t *testing.T) {
	prov := newIndexRecordingProvider()
	wc := openIndexWorkflow(t, writeSkillIndexPack(t, false), prov)

	_, err := wc.Send(context.Background(), "hi")
	require.NoError(t, err)

	_, err = wc.Transition("Open")
	require.NoError(t, err)
	_, err = wc.Send(context.Background(), "hi")
	require.NoError(t, err)

	_, err = wc.Transition("Back")
	require.NoError(t, err)
	_, err = wc.Transition("Lock")
	require.NoError(t, err)
	_, err = wc.Send(context.Background(), "hi")
	require.NoError(t, err)

	offers := prov.offers()
	require.Len(t, offers, 3)
	assertIndexLists(t, offers[0], "allow_x", "x")
	assertIndexLists(t, offers[1], "open", "x", "y")
	assert.False(t, offers[2].offered, "locked admits no skill, so skill__activate is not offered")
}

// A transition the model makes mid-turn re-describes skill__activate for the
// destination state's next round in the same conversation. The states share a
// prompt, so the new skills filter is the only thing that changed: the round
// after the transition must still see a different index.
func TestSkillIndex_MidTurnTransitionChangesTheIndex(t *testing.T) {
	prov := newIndexRecordingProvider(
		types.MessageToolCall{ID: "t1", Name: "workflow__transition", Args: []byte(`{"event":"Open","context":"c"}`)},
		types.MessageToolCall{ID: "t2", Name: "workflow__transition", Args: []byte(`{"event":"Back","context":"c"}`)},
		types.MessageToolCall{ID: "t3", Name: "workflow__transition", Args: []byte(`{"event":"Lock","context":"c"}`)},
	)
	wc := openIndexWorkflow(t, writeSkillIndexPack(t, true), prov)

	_, err := wc.Send(context.Background(), "go")
	require.NoError(t, err)
	require.Equal(t, "locked", wc.CurrentState())

	offers := prov.offers()
	require.Len(t, offers, 4)
	assertIndexLists(t, offers[0], "allow_x", "x")
	assertIndexLists(t, offers[1], "open after a mid-turn transition", "x", "y")
	assertIndexLists(t, offers[2], "allow_x again", "x")
	assert.False(t, offers[3].offered, "entering locked mid-turn withdraws skill__activate")
}

// Resuming into a state describes skill__activate for that state.
func TestSkillIndex_ResumeDescribesTheResumedState(t *testing.T) {
	packPath := writeSkillIndexPack(t, false)
	store := statestore.NewMemoryStore()
	const id = "wf-skills-index-resume"
	require.NoError(t, store.Save(context.Background(), &statestore.ConversationState{
		ID:       id,
		Metadata: map[string]any{workflowCurrentKey: &workflow.Context{CurrentState: "open"}},
	}))

	prov := newIndexRecordingProvider()
	wc, err := ResumeWorkflow(id, packPath, WithStateStore(store), WithSkipSchemaValidation(), WithProvider(prov))
	require.NoError(t, err)
	t.Cleanup(func() { _ = wc.Close() })

	_, err = wc.Send(context.Background(), "hi")
	require.NoError(t, err)
	offers := prov.offers()
	require.Len(t, offers, 1)
	assertIndexLists(t, offers[0], "resumed into open", "x", "y")
}

// A configured skills selector still narrows the index, now within what the
// state admits: it picks from allow_x's skills and the round lists its choice.
func TestSkillIndex_SelectorNarrowsWithinTheStateFilter(t *testing.T) {
	sel := &capTestSelector{name: "s", selected: []string{"x"}}
	prov := newIndexRecordingProvider()
	wc, err := OpenWorkflow(writeSkillIndexPack(t, false),
		WithSkipSchemaValidation(), WithProvider(prov), WithSelector("s", sel),
		func(c *config) error { c.skillsSelectorName = "s"; return nil },
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = wc.Close() })

	_, err = wc.Transition("Open")
	require.NoError(t, err)
	_, err = wc.Send(context.Background(), "about x")
	require.NoError(t, err)

	offers := prov.offers()
	require.Len(t, offers, 1)
	assertIndexLists(t, offers[0], "open, selector chose x", "x")
}

// A host that replaced skill__activate's description keeps it: there is no
// registered index in it to narrow. A filter that admits nothing still
// withholds the tool.
func TestSkillsCapability_DescribeActivateKeepsAnOverriddenDescription(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "alpha")
	cap := NewSkillsCapability([]skills.SkillSource{{Dir: dir}})
	p := &pack.Pack{Pack: packspec.Pack{ID: "t", Prompts: map[string]*pack.Prompt{"chat": {ID: "chat"}}}}
	require.NoError(t, cap.Init(CapabilityContext{Pack: p, PromptName: "chat"}))

	overridden := skills.BuildSkillActivateDescriptor()
	overridden.Description = "Host wording."
	set := skills.NewActiveSet()
	set.SetFilter(filepath.Join(dir, "elsewhere"))
	assert.Nil(t, cap.describeActivate(overridden, set, nil), "nothing admitted, nothing offered")

	set.SetFilter("")
	assert.Same(t, overridden, cap.describeActivate(overridden, set, []string{"alpha"}))
}

// A conversation outside any workflow has no filter: the index lists every
// skill.
func TestSkillIndex_NoWorkflowListsEverySkill(t *testing.T) {
	skillsDir := t.TempDir()
	writeSkillFile(t, filepath.Join(skillsDir, "billing"), "x")
	writeSkillFile(t, skillsDir, "y")

	prov := newIndexRecordingProvider()
	conv, err := Open(writeWorkflowTestPack(t, toolGrantPackJSON), "chat",
		WithProvider(prov), WithSkipSchemaValidation(), WithSkillsDir(skillsDir))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	conv.OnTool("lookup_order", func(map[string]any) (any, error) { return "ok", nil })

	_, err = conv.Send(context.Background(), "hi")
	require.NoError(t, err)
	offers := prov.offers()
	require.Len(t, offers, 1)
	assertIndexLists(t, offers[0], "no workflow", "x", "y")
}
