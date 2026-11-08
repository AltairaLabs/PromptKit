package prompt_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	jsonrepo "github.com/AltairaLabs/PromptKit/runtime/v2/persistence/json"
	"github.com/AltairaLabs/PromptKit/runtime/v2/persistence/memory"
	yamlrepo "github.com/AltairaLabs/PromptKit/runtime/v2/persistence/yaml"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/template"
)

// These tests take a fragment the way PromptArena and packc do: from source
// files on disk, through a file repository and the registry, into
// PackCompiler.Compile and Registry.LoadWithVars. Each case asserts both what
// the compiler wrote and what the render produced, and every pack that
// compiles must also render (requireCompiledPackRenders). Each case runs
// against every file repository, since a fake that honors `path` hid a real
// repository that did not.

// sourceFormat is one file repository and the encoding of its files.
type sourceFormat struct {
	name    string
	ext     string
	marshal func(any) ([]byte, error)
	newRepo func(base string, mapping map[string]string) prompt.Repository
}

var sourceFormats = []sourceFormat{
	{
		name: "yaml", ext: ".yaml", marshal: yaml.Marshal,
		newRepo: func(base string, mapping map[string]string) prompt.Repository {
			return yamlrepo.NewYAMLPromptRepository(base, mapping)
		},
	},
	{
		name: "json", ext: ".json", marshal: json.Marshal,
		newRepo: func(base string, mapping map[string]string) prompt.Repository {
			return jsonrepo.NewJSONPromptRepository(base, mapping)
		},
	},
}

// sourcePrompt is one prompt config written under <base>/prompts/.
type sourcePrompt struct {
	taskType  string
	template  string
	fragments []prompt.FragmentRef
	variables []string // declared, each required
}

// sourceTree is the files a case writes: its prompts, and its fragments by
// name, each written to <base>/fragments/<name><ext>.
type sourceTree struct {
	prompts   []sourcePrompt
	fragments map[string]string
}

// fragPath is the path a prompt under prompts/ uses to reference a fragment in
// the sibling fragments/ directory.
func (f sourceFormat) fragPath(name string) string {
	return "../fragments/" + name + f.ext
}

func (f sourceFormat) write(t *testing.T, path string, doc any) {
	t.Helper()
	data, err := f.marshal(doc)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, data, 0o600))
}

func promptDoc(p sourcePrompt) map[string]any {
	spec := map[string]any{
		"task_type":       p.taskType,
		"version":         "1.0.0",
		"description":     "Prompt " + p.taskType,
		"system_template": p.template,
	}
	if len(p.fragments) > 0 {
		refs := make([]map[string]any, 0, len(p.fragments))
		for _, ref := range p.fragments {
			doc := map[string]any{"name": ref.Name, "required": ref.Required}
			if ref.Path != "" {
				doc["path"] = ref.Path
			}
			refs = append(refs, doc)
		}
		spec["fragments"] = refs
	}
	if len(p.variables) > 0 {
		vars := make([]map[string]any, 0, len(p.variables))
		for _, name := range p.variables {
			vars = append(vars, map[string]any{"name": name, "required": true})
		}
		spec["variables"] = vars
	}
	return map[string]any{
		"apiVersion": "promptkit.altairalabs.ai/v1alpha1",
		"kind":       "PromptConfig",
		"metadata":   map[string]any{"name": p.taskType},
		"spec":       spec,
	}
}

// load writes tree under a new base directory and returns a registry built on
// the format's repository, as packc and PromptArena build theirs.
func (f sourceFormat) load(t *testing.T, tree sourceTree) *prompt.Registry {
	t.Helper()
	base := t.TempDir()
	mapping := make(map[string]string, len(tree.prompts))
	for _, p := range tree.prompts {
		rel := filepath.Join("prompts", p.taskType+f.ext)
		f.write(t, filepath.Join(base, rel), promptDoc(p))
		mapping[p.taskType] = rel
	}
	for name, content := range tree.fragments {
		f.write(t, filepath.Join(base, "fragments", name+f.ext),
			map[string]any{"fragment_type": "text", "version": "v1", "content": content})
	}
	return prompt.NewRegistryWithRepository(f.newRepo(base, mapping))
}

// render renders a prompt through the registry and returns the system prompt,
// or the error that stopped it. LoadWithVars reports a failure only as nil, so
// the error comes from the same two steps it takes: LoadTemplate, then the
// renderer over the template's merged variables.
func render(t *testing.T, reg *prompt.Registry, taskType string, vars map[string]string) (string, error) {
	t.Helper()
	tmpl, err := reg.LoadTemplate(taskType, vars, "")
	if err != nil {
		require.Nil(t, reg.LoadWithVars(taskType, vars, ""), "LoadWithVars must fail where LoadTemplate does")
		return "", err
	}
	text, err := template.NewRenderer().Render(tmpl.RawTemplate, tmpl.DefaultVars)
	assembled := reg.LoadWithVars(taskType, vars, "")
	if err != nil {
		require.Nil(t, assembled, "LoadWithVars must fail where the render does")
		return "", err
	}
	require.NotNil(t, assembled, "LoadWithVars failed where the render succeeded")
	require.Equal(t, text, assembled.SystemPrompt)
	return assembled.SystemPrompt, nil
}

// packRegistry builds a registry from a compiled pack, the way a pack
// consumer (the SDK) does.
func packRegistry(pack *prompt.Pack) *prompt.Registry {
	repo := memory.NewPromptRepository()
	for taskType := range pack.Prompts {
		repo.RegisterPrompt(taskType, pack.PromptConfig(taskType))
	}
	for name, content := range pack.Fragments {
		repo.RegisterFragment(name, &prompt.Fragment{Type: "text", Content: content})
	}
	return prompt.NewRegistryWithRepository(repo)
}

// declaredVars returns a value for each variable a pack prompt declares:
// overrides first, then the variable's name prefixed with "v-".
func declaredVars(pp *prompt.PackPrompt, overrides map[string]string) map[string]string {
	vars := make(map[string]string, len(pp.Variables))
	for _, v := range pp.Variables {
		if val, ok := overrides[v.Name]; ok {
			vars[v.Name] = val
			continue
		}
		vars[v.Name] = "v-" + v.Name
	}
	return vars
}

// requireCompiledPackRenders is the invariant every compile case checks:
// anything that compiles must render. The pack must validate with no errors,
// survive MarshalPack and LoadPack unchanged, and every prompt in it must
// render from the loaded pack, given values for its declared variables
// (overrides, else a placeholder value). It returns the rendered system
// prompts by task type.
//
// A prompt whose fragment is chosen by a variable (persona_{{region}}) is the
// one exception, because the pack holds only the "{{name}}" reference to that
// fragment, not its content. For it the render must fail, naming the
// placeholder it could not fill: it must never render with the fragment
// silently missing.
func requireCompiledPackRenders(
	t *testing.T, pack *prompt.Pack, overrides map[string]string,
) map[string]string {
	t.Helper()
	result := pack.ValidateDetailed()
	require.Empty(t, result.Errors, "a compiled pack must validate")

	loaded := roundTripPack(t, pack)
	reg := packRegistry(loaded)
	out := make(map[string]string, len(loaded.Prompts))
	for _, taskType := range loaded.ListPrompts() {
		text, err := render(t, reg, taskType, declaredVars(loaded.Prompts[taskType], overrides))
		if unresolved := unresolvedFragmentRefs(loaded); len(unresolved) > 0 &&
			usesAny(loaded.Prompts[taskType].SystemTemplate, unresolved) {
			require.Error(t, err, "%s: a fragment the pack cannot hold must not render empty", taskType)
			assert.Contains(t, err.Error(), "unresolved template placeholders")
			continue
		}
		require.NoError(t, err, "%s: a prompt that compiles must render", taskType)
		out[taskType] = text
	}
	return out
}

// roundTripPack writes pack with MarshalPack, loads it back with LoadPack and
// requires the fragments and prompts to come back unchanged.
func roundTripPack(t *testing.T, pack *prompt.Pack) *prompt.Pack {
	t.Helper()
	data, err := prompt.NewPackCompiler(nil).MarshalPack(pack)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "compiled.pack.json")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	loaded, err := prompt.LoadPack(path)
	require.NoError(t, err)
	if len(pack.Fragments) == 0 {
		assert.Empty(t, loaded.Fragments)
	} else {
		assert.Equal(t, pack.Fragments, loaded.Fragments, "fragments must survive MarshalPack and LoadPack")
	}
	require.Equal(t, pack.ListPrompts(), loaded.ListPrompts())
	for name, pp := range pack.Prompts {
		assert.Equal(t, pp.SystemTemplate, loaded.Prompts[name].SystemTemplate)
	}
	return loaded
}

// unresolvedFragmentRefs returns the placeholders of the pack's fragments the
// compiler wrote as "{{name}}" references.
func unresolvedFragmentRefs(pack *prompt.Pack) []string {
	var refs []string
	for name, content := range pack.Fragments {
		if content == "{{"+name+"}}" {
			refs = append(refs, content)
		}
	}
	sort.Strings(refs)
	return refs
}

func usesAny(text string, placeholders []string) bool {
	for _, p := range placeholders {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

const toneContent = "Be warm and brief."

func supportPrompt(tmpl string, refs ...prompt.FragmentRef) sourcePrompt {
	return sourcePrompt{taskType: "support", template: tmpl, fragments: refs}
}

// Case 1: a static, required fragment that exists.
func TestFragmentsE2E_StaticRequiredPresent(t *testing.T) {
	for _, f := range sourceFormats {
		t.Run(f.name, func(t *testing.T) {
			reg := f.load(t, sourceTree{
				prompts: []sourcePrompt{
					supportPrompt("You are support. {{tone}}",
						prompt.FragmentRef{Name: "tone", Path: f.fragPath("tone"), Required: true}),
				},
				fragments: map[string]string{"tone": toneContent},
			})

			pack, err := prompt.NewPackCompiler(reg).Compile("support", "test")
			require.NoError(t, err)
			assert.Equal(t, toneContent, pack.Fragments["tone"])

			text, err := render(t, reg, "support", nil)
			require.NoError(t, err)
			assert.Equal(t, "You are support. "+toneContent, text)

			rendered := requireCompiledPackRenders(t, pack, nil)
			assert.Equal(t, text, rendered["support"], "the pack must render as the source does")
		})
	}
}

// Case 2: a static, required fragment whose file does not exist.
func TestFragmentsE2E_StaticRequiredMissing(t *testing.T) {
	for _, f := range sourceFormats {
		t.Run(f.name, func(t *testing.T) {
			t.Chdir(t.TempDir()) // nothing to fall back to in the working directory
			reg := f.load(t, sourceTree{prompts: []sourcePrompt{
				supportPrompt("You are support. {{tone}}",
					prompt.FragmentRef{Name: "tone", Path: f.fragPath("missing"), Required: true}),
			}})

			_, err := prompt.NewPackCompiler(reg).Compile("support", "test")
			require.Error(t, err)
			assert.Contains(t, err.Error(), `"support"`, "the error names the prompt")
			assert.Contains(t, err.Error(), `"tone"`, "the error names the fragment")
			assert.Contains(t, err.Error(), "missing"+f.ext, "the error names the file")

			_, err = render(t, reg, "support", nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "'tone'", "the render error names the fragment")
		})
	}
}

// Case 3: an optional fragment whose file does not exist (#2523). It compiles
// with no entry, and its placeholder renders empty.
func TestFragmentsE2E_StaticOptionalMissing(t *testing.T) {
	for _, f := range sourceFormats {
		t.Run(f.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			reg := f.load(t, sourceTree{prompts: []sourcePrompt{
				supportPrompt("You are support.{{tone}} Goodbye.",
					prompt.FragmentRef{Name: "tone", Path: f.fragPath("missing"), Required: false}),
			}})

			pack, err := prompt.NewPackCompiler(reg).Compile("support", "test")
			require.NoError(t, err)
			assert.NotContains(t, pack.Fragments, "tone")

			text, err := render(t, reg, "support", nil)
			require.NoError(t, err, "a missing optional fragment must not fail the render")
			assert.Equal(t, "You are support. Goodbye.", text)
		})
	}
}

// Case 4: the template is only a fragment, with no variables declared (#2522).
func TestFragmentsE2E_FragmentOnlyTemplate(t *testing.T) {
	for _, f := range sourceFormats {
		t.Run(f.name, func(t *testing.T) {
			reg := f.load(t, sourceTree{
				prompts: []sourcePrompt{
					supportPrompt("{{tone}}",
						prompt.FragmentRef{Name: "tone", Path: f.fragPath("tone"), Required: true}),
				},
				fragments: map[string]string{"tone": toneContent},
			})

			pack, err := prompt.NewPackCompiler(reg).Compile("support", "test")
			require.NoError(t, err)

			text, err := render(t, reg, "support", nil)
			require.NoError(t, err)
			assert.Equal(t, toneContent, text)

			rendered := requireCompiledPackRenders(t, pack, nil)
			assert.Equal(t, text, rendered["support"])
		})
	}
}

// Case 5: a fragment and a declared variable in the same template.
func TestFragmentsE2E_FragmentAndVariable(t *testing.T) {
	for _, f := range sourceFormats {
		t.Run(f.name, func(t *testing.T) {
			p := supportPrompt("{{tone}} Customer: {{customer}}.",
				prompt.FragmentRef{Name: "tone", Path: f.fragPath("tone"), Required: true})
			p.variables = []string{"customer"}
			reg := f.load(t, sourceTree{
				prompts:   []sourcePrompt{p},
				fragments: map[string]string{"tone": toneContent},
			})

			pack, err := prompt.NewPackCompiler(reg).Compile("support", "test")
			require.NoError(t, err)
			assert.Equal(t, toneContent, pack.Fragments["tone"])

			vars := map[string]string{"customer": "Ada"}
			text, err := render(t, reg, "support", vars)
			require.NoError(t, err)
			assert.Equal(t, toneContent+" Customer: Ada.", text)

			rendered := requireCompiledPackRenders(t, pack, vars)
			assert.Equal(t, text, rendered["support"])
		})
	}
}

// Case 6: the fragment's own content holds a declared variable.
func TestFragmentsE2E_VariableInsideFragment(t *testing.T) {
	for _, f := range sourceFormats {
		t.Run(f.name, func(t *testing.T) {
			p := supportPrompt("You are support. {{greeting}}",
				prompt.FragmentRef{Name: "greeting", Path: f.fragPath("greeting"), Required: true})
			p.variables = []string{"customer"}
			reg := f.load(t, sourceTree{
				prompts:   []sourcePrompt{p},
				fragments: map[string]string{"greeting": "Greet {{customer}} by name."},
			})

			pack, err := prompt.NewPackCompiler(reg).Compile("support", "test")
			require.NoError(t, err)
			assert.Equal(t, "Greet {{customer}} by name.", pack.Fragments["greeting"],
				"the compiler keeps the fragment's variables for render time")

			vars := map[string]string{"customer": "Ada"}
			text, err := render(t, reg, "support", vars)
			require.NoError(t, err)
			assert.Equal(t, "You are support. Greet Ada by name.", text)

			rendered := requireCompiledPackRenders(t, pack, vars)
			assert.Equal(t, text, rendered["support"])
		})
	}
}

// Case 7: a relative path resolves against the prompt config's directory, not
// the working directory.
func TestFragmentsE2E_RelativePathIgnoresWorkingDirectory(t *testing.T) {
	for _, f := range sourceFormats {
		t.Run(f.name, func(t *testing.T) {
			// A decoy where the path would land if it resolved against the
			// working directory instead.
			cwd := filepath.Join(t.TempDir(), "elsewhere", "cwd")
			f.write(t, filepath.Join(filepath.Dir(cwd), "fragments", "tone"+f.ext),
				map[string]any{"content": "decoy from the working directory"})
			require.NoError(t, os.MkdirAll(cwd, 0o750))
			t.Chdir(cwd)

			reg := f.load(t, sourceTree{
				prompts: []sourcePrompt{
					supportPrompt("{{tone}}",
						prompt.FragmentRef{Name: "tone", Path: f.fragPath("tone"), Required: true}),
				},
				fragments: map[string]string{"tone": toneContent},
			})

			pack, err := prompt.NewPackCompiler(reg).Compile("support", "test")
			require.NoError(t, err)
			assert.Equal(t, toneContent, pack.Fragments["tone"])

			text, err := render(t, reg, "support", nil)
			require.NoError(t, err)
			assert.Equal(t, toneContent, text)
			requireCompiledPackRenders(t, pack, nil)
		})
	}
}

// Case 8: a fragment chosen by a variable. The compiler can't resolve it, so
// it writes the reference; the render picks the file per value.
func TestFragmentsE2E_DynamicName(t *testing.T) {
	for _, f := range sourceFormats {
		t.Run(f.name, func(t *testing.T) {
			p := supportPrompt("You are support. {{persona_{{region}}}}",
				prompt.FragmentRef{Name: "persona_{{region}}", Path: f.fragPath("persona_{{region}}"), Required: true})
			p.variables = []string{"region"}
			reg := f.load(t, sourceTree{
				prompts: []sourcePrompt{p},
				fragments: map[string]string{
					"persona_us": "Use US spelling.",
					"persona_eu": "Use EU date formats.",
				},
			})

			pack, err := prompt.NewPackCompiler(reg).Compile("support", "test")
			require.NoError(t, err)
			assert.Equal(t, map[string]string{"persona_{{region}}": "{{persona_{{region}}}}"}, pack.Fragments)

			for region, want := range map[string]string{"us": "Use US spelling.", "eu": "Use EU date formats."} {
				text, err := render(t, reg, "support", map[string]string{"region": region})
				require.NoError(t, err, region)
				assert.Equal(t, "You are support. "+want, text, region)
			}
			requireCompiledPackRenders(t, pack, map[string]string{"region": "us"})
		})
	}
}

// A reference with no path loads <base>/fragments/<name><ext> from the
// repository's base directory.
func TestFragmentsE2E_NameOnlyReference(t *testing.T) {
	for _, f := range sourceFormats {
		t.Run(f.name, func(t *testing.T) {
			reg := f.load(t, sourceTree{
				prompts: []sourcePrompt{
					supportPrompt("You are support. {{tone}}", prompt.FragmentRef{Name: "tone", Required: true}),
				},
				fragments: map[string]string{"tone": toneContent},
			})

			pack, err := prompt.NewPackCompiler(reg).Compile("support", "test")
			require.NoError(t, err)
			assert.Equal(t, toneContent, pack.Fragments["tone"])

			text, err := render(t, reg, "support", nil)
			require.NoError(t, err)
			assert.Equal(t, "You are support. "+toneContent, text)
			assert.Equal(t, text, requireCompiledPackRenders(t, pack, nil)["support"])

			// The registry reports the fragment it loaded, by name, until its
			// cache is cleared.
			assert.Equal(t, []string{"tone"}, reg.GetCachedFragments())
			reg.ClearCache()
			assert.Empty(t, reg.GetCachedFragments())
		})
	}
}

// Pack fragments are pack-level: CompileFromRegistry gathers every prompt's
// fragment references into one map. A prompt that shares a fragment renders
// it; a prompt that references none renders as it did, with no fragment
// content added.
func TestFragmentsE2E_PackLevelFragmentsAcrossPrompts(t *testing.T) {
	for _, f := range sourceFormats {
		t.Run(f.name, func(t *testing.T) {
			toneRef := prompt.FragmentRef{Name: "tone", Path: f.fragPath("tone"), Required: true}
			reg := f.load(t, sourceTree{
				prompts: []sourcePrompt{
					{taskType: "billing", template: "Billing. {{tone}}", fragments: []prompt.FragmentRef{toneRef}},
					{taskType: "support", template: "Support. {{tone}} {{policy}}", fragments: []prompt.FragmentRef{
						toneRef, {Name: "policy", Path: f.fragPath("policy"), Required: true},
					}},
					{taskType: "plain", template: "Plain prompt."},
				},
				fragments: map[string]string{"tone": toneContent, "policy": "Refunds take 5 days."},
			})

			pack, err := prompt.NewPackCompiler(reg).CompileFromRegistry("multi", "test")
			require.NoError(t, err)
			assert.Equal(t, map[string]string{"tone": toneContent, "policy": "Refunds take 5 days."}, pack.Fragments)

			rendered := requireCompiledPackRenders(t, pack, nil)
			for _, taskType := range []string{"billing", "support", "plain"} {
				text, err := render(t, reg, taskType, nil)
				require.NoError(t, err)
				assert.Equal(t, text, rendered[taskType], "%s: the pack must render as the source does", taskType)
			}
			assert.Equal(t, "Plain prompt.", rendered["plain"])
		})
	}
}

// Two prompts whose fragments of one name come from different files can't
// share a pack, which holds one fragment per name.
func TestFragmentsE2E_ConflictingFragmentsFailCompile(t *testing.T) {
	for _, f := range sourceFormats {
		t.Run(f.name, func(t *testing.T) {
			reg := f.load(t, sourceTree{
				prompts: []sourcePrompt{
					{taskType: "billing", template: "{{tone}}", fragments: []prompt.FragmentRef{
						{Name: "tone", Path: f.fragPath("tone_billing"), Required: true},
					}},
					{taskType: "support", template: "{{tone}}", fragments: []prompt.FragmentRef{
						{Name: "tone", Path: f.fragPath("tone_support"), Required: true},
					}},
				},
				fragments: map[string]string{"tone_billing": "Be formal.", "tone_support": "Be friendly."},
			})

			_, err := prompt.NewPackCompiler(reg).CompileFromRegistry("multi", "test")
			require.Error(t, err)
			assert.Contains(t, err.Error(), fmt.Sprintf("%q", "tone"))
		})
	}
}
