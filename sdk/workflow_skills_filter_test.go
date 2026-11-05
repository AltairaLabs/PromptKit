package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/skills"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/runtime/v2/workflow"
)

// writeSkillsFilterPack writes a workflow pack over two skills, x and y, whose
// states narrow them differently:
//
//	allow_x  skills: <dir>/billing  (x only; y sits outside it)
//	locked   skills: none      (nothing)
//	open     (no skills field) (everything)
func writeSkillsFilterPack(t *testing.T) string {
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
	content := fmt.Sprintf(`{
		"$schema": "https://promptpack.org/schema/2025.1/promptpack.schema.json",
		"schema_version": "2025.1",
		"id": "workflow-skills-filter",
		"version": "1.0.0",
		"template_engine": {"version": "v1", "syntax": "handlebars", "features": []},
		"skills": [%s],
		"prompts": {%s, %s, %s},
		"workflow": {
			"version": 1,
			"entry": "allow_x",
			"states": {
				"allow_x": {"prompt_task": "pa", "skills": %s, "on_event": {"Lock": "locked", "Open": "open"}},
				"locked":  {"prompt_task": "pb", "skills": "none", "on_event": {"Back": "allow_x"}},
				"open":    {"prompt_task": "pc", "on_event": {"Back": "allow_x"}}
			}
		}
	}`, quote(skillsDir), prompt("pa"), prompt("pb"), prompt("pc"), quote(filepath.Join(skillsDir, "billing")))
	return writeWorkflowTestPack(t, content)
}

// activateSkill calls skill__activate the way the tool loop does: through the
// conversation's registry, carrying the conversation's own ActiveSet.
func activateSkill(t *testing.T, conv *Conversation, name string) error {
	t.Helper()
	res, err := conv.ToolRegistry().Execute(conv.withConversationState(context.Background()),
		skills.SkillActivateTool, json.RawMessage(`{"name":`+fmt.Sprintf("%q", name)+`}`))
	require.NoError(t, err)
	if res.Error != "" {
		return fmt.Errorf("%s", res.Error)
	}
	return nil
}

func openSkillsFilterWorkflow(t *testing.T, packPath string, opts ...Option) *WorkflowConversation {
	t.Helper()
	opts = append([]Option{
		WithSkipSchemaValidation(),
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
	}, opts...)
	wc, err := OpenWorkflow(packPath, opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = wc.Close() })
	return wc
}

// A workflow state's `skills` restricts which skills can activate in it, from
// the state the workflow opens in through every state it transitions to
// (#2314). Before the fix the field was parsed and never read: every skill
// activated in every state.
func TestWorkflowSkills_StateFilterAppliesOnOpenAndTransition(t *testing.T) {
	wc := openSkillsFilterWorkflow(t, writeSkillsFilterPack(t))
	require.Equal(t, "allow_x", wc.CurrentState())

	err := activateSkill(t, wc.ActiveConversation(), "y")
	require.ErrorContains(t, err, "not available in the current state",
		"y is outside allow_x's skills path")
	require.NoError(t, activateSkill(t, wc.ActiveConversation(), "x"))

	state, err := wc.Transition("Lock")
	require.NoError(t, err)
	require.Equal(t, "locked", state)

	err = activateSkill(t, wc.ActiveConversation(), "x")
	require.ErrorContains(t, err, "not available in the current state",
		`"none" admits no skill, x included`)
}

// A state with no `skills` field does not restrict activation. Entering one
// mid-turn, in the conversation that ran allow_x, lifts allow_x's narrowing:
// y, refused a moment earlier, activates.
func TestWorkflowSkills_StateWithoutFilterAdmitsEverySkill(t *testing.T) {
	prov := newScriptedSkillsProvider(
		types.MessageToolCall{ID: "y1", Name: skills.SkillActivateTool, Args: []byte(`{"name":"y"}`)},
		types.MessageToolCall{ID: "t1", Name: "workflow__transition", Args: []byte(`{"event":"Open","context":"c"}`)},
		types.MessageToolCall{ID: "y2", Name: skills.SkillActivateTool, Args: []byte(`{"name":"y"}`)},
	)
	wc, err := OpenWorkflow(writeSkillsFilterPack(t), WithSkipSchemaValidation(), WithProvider(prov))
	require.NoError(t, err)
	t.Cleanup(func() { _ = wc.Close() })
	turnConv := wc.ActiveConversation()

	_, err = wc.Send(context.Background(), "go")
	require.NoError(t, err)
	require.Equal(t, "open", wc.CurrentState())

	results := prov.resultsByID()
	assert.Contains(t, results["y1"].Error, "not available in the current state", "allow_x refuses y")
	assert.Empty(t, results["y2"].Error, "open declares no skills, so y activates")
	assert.Equal(t, []string{"y"}, turnConv.skillSet.Names())
	assert.Empty(t, turnConv.skillSet.Filter(), "entering open cleared allow_x's filter")

	// The conversation opened for open after the turn is unrestricted too.
	require.NoError(t, activateSkill(t, wc.ActiveConversation(), "x"))
	require.NoError(t, activateSkill(t, wc.ActiveConversation(), "y"))
	assert.Equal(t, []string{"x", "y"}, wc.ActiveConversation().skillSet.Names())
}

// Resuming into a state applies that state's filter, not the entry state's and
// not none at all.
func TestWorkflowSkills_ResumeAppliesTheResumedStatesFilter(t *testing.T) {
	packPath := writeSkillsFilterPack(t)
	store := statestore.NewMemoryStore()
	const id = "wf-skills-resume"
	require.NoError(t, store.Save(context.Background(), &statestore.ConversationState{
		ID:       id,
		Metadata: map[string]any{workflowCurrentKey: &workflow.Context{CurrentState: "locked"}},
	}))

	wc, err := ResumeWorkflow(id, packPath,
		WithStateStore(store),
		WithSkipSchemaValidation(),
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = wc.Close() })
	require.Equal(t, "locked", wc.CurrentState())

	require.Error(t, activateSkill(t, wc.ActiveConversation(), "x"),
		"resumed into locked, whose skills are none")
	require.Error(t, activateSkill(t, wc.ActiveConversation(), "y"))
}

// scriptedSkillsProvider makes one scripted tool call per round, then answers
// with text, recording the tool results it was handed. A workflow__transition
// in the script is handed off mid-turn, so the rounds after it run as the
// destination state in the same conversation.
type scriptedSkillsProvider struct {
	*toolGrantProvider
	script  []types.MessageToolCall
	results []types.MessageToolResult // tool results seen by the final round
}

func newScriptedSkillsProvider(script ...types.MessageToolCall) *scriptedSkillsProvider {
	return &scriptedSkillsProvider{toolGrantProvider: newToolGrantProvider(), script: script}
}

func (p *scriptedSkillsProvider) PredictWithTools(
	_ context.Context, req providers.PredictionRequest, _ providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.mu.Lock()
	p.rounds++
	round := p.rounds
	p.mu.Unlock()

	if round <= len(p.script) {
		calls := []types.MessageToolCall{p.script[round-1]}
		return providers.PredictionResponse{ToolCalls: calls}, calls, nil
	}
	for i := range req.Messages {
		if tr := req.Messages[i].ToolResult; tr != nil {
			p.results = append(p.results, *tr)
		}
	}
	return providers.PredictionResponse{Content: "done"}, nil, nil
}

func (p *scriptedSkillsProvider) resultsByID() map[string]types.MessageToolResult {
	byID := make(map[string]types.MessageToolResult, len(p.results))
	for _, r := range p.results {
		byID[r.ID] = r
	}
	return byID
}

// A transition the model makes mid-turn narrows the same conversation's skills
// before the destination state's next round: x, active in allow_x, is
// deactivated on entering locked and cannot be activated again there.
func TestWorkflowSkills_MidTurnTransitionNarrowsTheActiveSet(t *testing.T) {
	prov := newScriptedSkillsProvider(
		types.MessageToolCall{ID: "a1", Name: skills.SkillActivateTool, Args: []byte(`{"name":"x"}`)},
		types.MessageToolCall{ID: "t1", Name: "workflow__transition", Args: []byte(`{"event":"Lock","context":"c"}`)},
		types.MessageToolCall{ID: "a2", Name: skills.SkillActivateTool, Args: []byte(`{"name":"x"}`)},
	)
	wc, err := OpenWorkflow(writeSkillsFilterPack(t), WithSkipSchemaValidation(), WithProvider(prov))
	require.NoError(t, err)
	t.Cleanup(func() { _ = wc.Close() })
	turnConv := wc.ActiveConversation()

	_, err = wc.Send(context.Background(), "go")
	require.NoError(t, err)
	require.Equal(t, "locked", wc.CurrentState())

	byID := prov.resultsByID()
	require.Contains(t, byID, "a1")
	require.Contains(t, byID, "a2")
	assert.Empty(t, byID["a1"].Error, "x activates in allow_x")
	assert.Contains(t, byID["a2"].Error, "not available in the current state",
		"after the mid-turn handoff to locked, x is refused")
	assert.Empty(t, turnConv.skillSet.Names(),
		"entering locked deactivated x in the conversation that ran the turn")
}
