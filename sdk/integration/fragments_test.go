package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	yamlrepo "github.com/AltairaLabs/PromptKit/runtime/v2/persistence/yaml"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// These take a pack's fragments through the SDK the way an application does:
// a pack file, sdk.Open (or Resume, OpenDuplex, OpenWorkflow, a PackTemplate),
// and Send. Every assertion is on the system prompt the provider received.
// The PromptPack spec makes a pack's fragments pack-level content that any of
// its prompts can reference.

const fragTone = "Be warm and brief."

// fragmentPackJSON is a pack with two fragments and three prompts: one uses a
// fragment, one a fragment and a variable, and one neither.
const fragmentPackJSON = `{
	"id": "fragment-test",
	"version": "1.0.0",
	"description": "Pack with fragments",
	"fragments": {
		"tone": "Be warm and brief.",
		"greeting": "Greet {{customer}} by name."
	},
	"prompts": {
		"chat": {
			"id": "chat", "name": "Chat", "version": "1.0.0",
			"system_template": "You are support. {{tone}}"
		},
		"named": {
			"id": "named", "name": "Named", "version": "1.0.0",
			"system_template": "{{tone}} {{greeting}} Topic: {{topic}}.",
			"variables": [
				{"name": "customer", "type": "string", "required": true},
				{"name": "topic", "type": "string", "required": true}
			]
		},
		"plain": {
			"id": "plain", "name": "Plain", "version": "1.0.0",
			"system_template": "You are a plain assistant."
		}
	}
}`

// S1: a prompt that uses a pack fragment.
func TestFragments_PackFragmentReachesSystemPrompt(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)
	ec := newEventCollector(bus)
	rec := newRecordingProvider()
	conv := openTestConvWithPack(t, fragmentPackJSON, "chat", sdk.WithProvider(rec), sdk.WithEventBus(bus))

	_, err := conv.Send(context.Background(), "hi")
	require.NoError(t, err)
	assert.Equal(t, "You are support. "+fragTone, rec.system())

	rendered := ec.waitForType(events.EventTemplateRendered, 2*time.Second)
	require.Len(t, rendered, 1)
	data, ok := rendered[0].Data.(*events.TemplateRenderedData)
	require.True(t, ok)
	assert.Equal(t, rec.system(), data.SystemPrompt)
	assert.Equal(t, []string{"tone"}, data.FragmentsUsed)
}

// S2: a fragment, a variable inside another fragment, and a variable set with
// SetVar, all in one prompt. Both variables are required and set only with
// SetVar: the required-variable check runs once every source is merged.
func TestFragments_FragmentAndSetVar(t *testing.T) {
	rec := newRecordingProvider()
	conv := openTestConvWithPack(t, fragmentPackJSON, "named", sdk.WithProvider(rec))
	conv.SetVar("customer", "Ada")
	conv.SetVar("topic", "billing")

	_, err := conv.Send(context.Background(), "hi")
	require.NoError(t, err)
	assert.Equal(t, fragTone+" Greet Ada by name. Topic: billing.", rec.system())
}

// A prompt that references no fragment renders as it is: making the pack's
// fragments available to every prompt adds nothing to one that doesn't use
// them, and the rendered event doesn't report them as used.
func TestFragments_PromptWithoutFragmentIsUnchanged(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)
	ec := newEventCollector(bus)
	rec := newRecordingProvider()
	conv := openTestConvWithPack(t, fragmentPackJSON, "plain", sdk.WithProvider(rec), sdk.WithEventBus(bus))

	_, err := conv.Send(context.Background(), "hi")
	require.NoError(t, err)
	assert.Equal(t, "You are a plain assistant.", rec.system())

	rendered := ec.waitForType(events.EventTemplateRendered, 2*time.Second)
	require.Len(t, rendered, 1)
	data, ok := rendered[0].Data.(*events.TemplateRenderedData)
	require.True(t, ok)
	assert.Empty(t, data.FragmentsUsed)
}

// A fragment chosen by a variable that declares no values (validation.enum)
// can't be resolved at compile time, so the pack holds only its reference.
// The SDK must fail the turn on it, naming the placeholder, rather than render
// the prompt with the fragment missing.
func TestFragments_DynamicReferenceFailsLoudly(t *testing.T) {
	packJSON := `{
		"id": "dynamic-fragment", "version": "1.0.0", "description": "dynamic",
		"fragments": {"persona_{{region}}": "{{persona_{{region}}}}"},
		"prompts": {"chat": {"id": "chat", "name": "Chat", "version": "1.0.0",
			"system_template": "You are support. {{persona_{{region}}}}",
			"variables": [{"name": "region", "type": "string", "required": false}]}}
	}`
	conv := openTestConvWithPack(t, packJSON, "chat")
	conv.SetVar("region", "us")

	_, err := conv.Send(context.Background(), "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "{{persona_us}}")

	// The application can still supply the runtime-chosen fragment itself.
	rec := newRecordingProvider()
	conv = openTestConvWithPack(t, packJSON, "chat", sdk.WithProvider(rec))
	conv.SetVar("region", "us")
	conv.SetVar("persona_us", "Use US spelling.")
	_, err = conv.Send(context.Background(), "hi")
	require.NoError(t, err)
	assert.Equal(t, "You are support. Use US spelling.", rec.system())
}

// S3: conversations from one pack, each sending concurrently. Half open the
// pack themselves; half share one PackTemplate, and so one prompt registry and
// its fragment cache, which is cold when they start. Run under -race.
func TestFragments_ConcurrentConversations(t *testing.T) {
	const conversations = 16
	packPath := writePackFile(t, fragmentPackJSON)
	tmpl, err := sdk.LoadTemplate(packPath, sdk.WithSkipSchemaValidation())
	require.NoError(t, err)

	recs := make([]*recordingProvider, conversations)
	convs := make([]*sdk.Conversation, conversations)
	for i := range conversations {
		recs[i] = newRecordingProvider()
		opts := []sdk.Option{sdk.WithProvider(recs[i]), sdk.WithSkipSchemaValidation()}
		if i%2 == 0 {
			convs[i], err = tmpl.Open("named", opts...)
		} else {
			convs[i], err = sdk.Open(packPath, "named", opts...)
		}
		require.NoError(t, err)
		t.Cleanup(func() { _ = convs[i].Close() })
		convs[i].SetVar("customer", fmt.Sprintf("customer-%d", i))
		convs[i].SetVar("topic", "billing")
	}

	start := make(chan struct{})
	errs := make([]error, conversations)
	var wg sync.WaitGroup
	for i := range conversations {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = convs[i].Send(context.Background(), "hi")
		}()
	}
	close(start)
	wg.Wait()

	for i := range conversations {
		require.NoError(t, errs[i], "conversation %d", i)
		assert.Equal(t, fmt.Sprintf("%s Greet customer-%d by name. Topic: billing.", fragTone, i), recs[i].system())
	}
}

// Resume rebuilds the conversation from the pack, so a resumed conversation
// must render the pack's fragments as the original did.
func TestFragments_ResumeRendersFragments(t *testing.T) {
	store := statestore.NewMemoryStore()
	packPath := writePackFile(t, fragmentPackJSON)
	ctx := context.Background()

	rec := newRecordingProvider()
	conv, err := sdk.Open(packPath, "chat", sdk.WithProvider(rec), sdk.WithSkipSchemaValidation(),
		sdk.WithStateStore(store), sdk.WithConversationID("frag-resume"))
	require.NoError(t, err)
	_, err = conv.Send(ctx, "hello")
	require.NoError(t, err)
	require.NoError(t, conv.Close())

	rec = newRecordingProvider()
	resumed, err := sdk.Resume("frag-resume", packPath, "chat", sdk.WithProvider(rec),
		sdk.WithSkipSchemaValidation(), sdk.WithStateStore(store))
	require.NoError(t, err)
	t.Cleanup(func() { _ = resumed.Close() })
	_, err = resumed.Send(ctx, "after resume")
	require.NoError(t, err)
	assert.Equal(t, "You are support. "+fragTone, rec.system())
}

// A duplex session is created with the rendered system prompt as its
// instruction, so the fragment must be in it.
func TestFragments_DuplexSessionInstruction(t *testing.T) {
	rec := newRecordingStreamingProvider()
	packPath := writePackFile(t, fragmentPackJSON)
	conv, err := sdk.OpenDuplex(packPath, "chat",
		sdk.WithProvider(rec),
		sdk.WithSkipSchemaValidation(),
		sdk.WithStreamingConfig(&providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{Type: types.ContentTypeAudio},
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	responseCh, err := conv.Response()
	require.NoError(t, err)
	sendDuplexTurn(t, conv, responseCh, "hello")

	instructions := rec.sessionInstructions()
	require.Len(t, instructions, 1)
	assert.Equal(t, "You are support. "+fragTone, instructions[0])
}

// Each workflow state renders its own prompt from the pack, so a state's
// fragment must reach the provider in that state, and after a transition the
// next state's.
func TestFragments_WorkflowStates(t *testing.T) {
	packJSON := `{
		"id": "workflow-fragments", "version": "1.0.0", "description": "workflow with fragments",
		"fragments": {"tone": "Be warm and brief.", "escalation": "Escalate politely."},
		"workflow": {
			"version": 1, "entry": "intake",
			"states": {
				"intake": {"prompt_task": "intake", "on_event": {"Escalate": "specialist"},
					"orchestration": "external"},
				"specialist": {"prompt_task": "specialist"}
			}
		},
		"prompts": {
			"intake": {"id": "intake", "name": "Intake", "version": "1.0.0",
				"system_template": "Intake. {{tone}}"},
			"specialist": {"id": "specialist", "name": "Specialist", "version": "1.0.0",
				"system_template": "Specialist. {{tone}} {{escalation}}"}
		}
	}`
	rec := newRecordingProvider()
	wc, err := sdk.OpenWorkflow(writePackFile(t, packJSON), sdk.WithProvider(rec), sdk.WithSkipSchemaValidation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = wc.Close() })
	ctx := context.Background()

	_, err = wc.Send(ctx, "hello")
	require.NoError(t, err)
	assert.Equal(t, "Intake. "+fragTone, rec.system())

	_, err = wc.Transition("Escalate")
	require.NoError(t, err)
	_, err = wc.Send(ctx, "escalated")
	require.NoError(t, err)
	assert.Equal(t, "Specialist. "+fragTone+" Escalate politely.", rec.system())
}

// roundTripSource is a prompt config and its fragment files, written as YAML
// sources the way packc and PromptArena read them.
type roundTripSource struct {
	name      string
	template  string
	fragments map[string]string // name -> content, each referenced by path
	variables map[string]string // declared variable -> value to render with
}

// writeRoundTripSource writes base/prompts/support.yaml and its fragments in
// base/fragments/, and returns the registry packc would build on them.
func writeRoundTripSource(t *testing.T, src roundTripSource) *prompt.Registry {
	t.Helper()
	base := t.TempDir()
	var refs, vars strings.Builder
	for name, content := range src.fragments {
		fmt.Fprintf(&refs, "    - name: %s\n      path: ../fragments/%s.yaml\n      required: true\n", name, name)
		writeFile(t, filepath.Join(base, "fragments", name+".yaml"),
			fmt.Sprintf("fragment_type: text\nversion: v1\ncontent: %q\n", content))
	}
	if len(src.variables) > 0 {
		vars.WriteString("  variables:\n")
		for name := range src.variables {
			fmt.Fprintf(&vars, "    - name: %s\n      required: true\n", name)
		}
	}
	writeFile(t, filepath.Join(base, "prompts", "support.yaml"), fmt.Sprintf(`apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: support
spec:
  task_type: support
  version: 1.0.0
  description: Support prompt
  system_template: %q
  fragments:
%s%s`, src.template, refs.String(), vars.String()))
	repo := yamlrepo.NewYAMLPromptRepository(base, map[string]string{"support": "prompts/support.yaml"})
	return prompt.NewRegistryWithRepository(repo)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// S4: compile source configs with PackCompiler, write the pack, open it with
// the SDK (schema validation on) and Send. The provider must receive the same
// system prompt Registry.LoadWithVars renders from the sources: the compiler
// and the SDK must agree on what the pack means.
func TestFragments_CompiledPackRendersAsSource(t *testing.T) {
	cases := []roundTripSource{
		{
			name:      "static required fragment",
			template:  "You are support. {{tone}}",
			fragments: map[string]string{"tone": fragTone},
		},
		{
			name:      "template is only a fragment",
			template:  "{{tone}}",
			fragments: map[string]string{"tone": fragTone},
		},
		{
			name:      "fragment and variable",
			template:  "{{tone}} Customer: {{customer}}.",
			fragments: map[string]string{"tone": fragTone},
			variables: map[string]string{"customer": "Ada"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg := writeRoundTripSource(t, tc)
			compiler := prompt.NewPackCompiler(reg)
			pack, err := compiler.Compile("support", "test")
			require.NoError(t, err)
			require.Empty(t, pack.ValidateDetailed().Errors, "the compiled pack must validate")
			packPath := filepath.Join(t.TempDir(), "support.pack.json")
			require.NoError(t, compiler.WritePack(pack, packPath))

			source := reg.LoadWithVars("support", tc.variables, "")
			require.NotNil(t, source, "the source prompt must render")

			// The sources declare their variables required. Supplied at Open or
			// only with SetVar, the render matches the source's.
			for _, viaSetVar := range []bool{false, true} {
				rec := newRecordingProvider()
				opts := []sdk.Option{sdk.WithProvider(rec)}
				if !viaSetVar {
					opts = append(opts, sdk.WithVariables(tc.variables))
				}
				conv, err := sdk.Open(packPath, "support", opts...)
				require.NoError(t, err)
				t.Cleanup(func() { _ = conv.Close() })
				if viaSetVar {
					for k, v := range tc.variables {
						conv.SetVar(k, v)
					}
				}
				_, err = conv.Send(context.Background(), "hi")
				require.NoError(t, err, "SetVar only: %v", viaSetVar)
				assert.Equal(t, source.SystemPrompt, rec.system(), "SetVar only: %v", viaSetVar)
			}
		})
	}
}
