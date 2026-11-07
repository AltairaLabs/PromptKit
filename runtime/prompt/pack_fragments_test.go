package prompt

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// dirRepository is a mockRepository that knows which directory each prompt
// came from, and records the base dir every fragment load was given.
type dirRepository struct {
	*mockRepository
	dirs     map[string]string
	baseDirs []string
	// byPath, when set, serves fragments by filepath.Join(baseDir, path).
	byPath map[string]*Fragment
}

func (d *dirRepository) PromptDir(taskType string) (string, error) {
	dir, ok := d.dirs[taskType]
	if !ok {
		return "", fmt.Errorf("unknown %q", taskType)
	}
	return dir, nil
}

func (d *dirRepository) LoadFragment(name, relativePath, baseDir string) (*Fragment, error) {
	d.baseDirs = append(d.baseDirs, baseDir)
	if relativePath != "" {
		if f, ok := d.byPath[filepath.Join(baseDir, relativePath)]; ok {
			return f, nil
		}
		return nil, fmt.Errorf("no fragment at %s", filepath.Join(baseDir, relativePath))
	}
	return d.mockRepository.LoadFragment(name, relativePath, baseDir)
}

func fragmentConfig(taskType string, refs ...FragmentRef) *Config {
	return &Config{
		Metadata: metav1.ObjectMeta{Name: taskType},
		Spec: Spec{
			TaskType:       taskType,
			Version:        "1.0.0",
			SystemTemplate: "{{intro}}",
			Fragments:      refs,
		},
	}
}

func compileWithRepo(t *testing.T, repo Repository, taskType string) (*Pack, error) {
	t.Helper()
	reg := NewRegistryWithRepository(repo)
	pc := NewPackCompilerWithDeps(reg, mockTimeProvider{fixedTime: time.Unix(0, 0)}, newMockFileWriter())
	return pc.Compile(taskType, "test")
}

func TestPackCompiler_Fragments_ResolvedToContent(t *testing.T) {
	repo := newMockRepository()
	repo.fragments["intro"] = &Fragment{Content: "Welcome to support."}
	repo.prompts["chat"] = fragmentConfig("chat",
		FragmentRef{Name: "intro", Required: true},
		FragmentRef{Name: "outro", Required: false}, // not in the repository
	)

	pack, err := compileWithRepo(t, repo, "chat")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"intro": "Welcome to support."}, pack.Fragments)
}

func TestPackCompiler_Fragments_RequiredMissingIsAnError(t *testing.T) {
	repo := newMockRepository()
	repo.prompts["chat"] = fragmentConfig("chat", FragmentRef{Name: "intro", Required: true})

	_, err := compileWithRepo(t, repo, "chat")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"intro"`)
	assert.Contains(t, err.Error(), `"chat"`)
}

func TestPackCompiler_Fragments_RequiredMissingFailsCompileFromRegistry(t *testing.T) {
	repo := newMockRepository()
	repo.prompts["chat"] = fragmentConfig("chat", FragmentRef{Name: "intro", Required: true})
	reg := NewRegistryWithRepository(repo)
	pc := NewPackCompilerWithDeps(reg, mockTimeProvider{fixedTime: time.Unix(0, 0)}, newMockFileWriter())

	_, err := pc.CompileFromRegistry("p", "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"intro"`)
}

func TestPackCompiler_Fragments_DynamicNameLeftAsReference(t *testing.T) {
	repo := newMockRepository()
	repo.prompts["chat"] = fragmentConfig("chat",
		FragmentRef{Name: "persona_{{region}}", Required: true},
		FragmentRef{Name: "policy", Path: "policies/{{region}}.yaml", Required: true},
	)

	pack, err := compileWithRepo(t, repo, "chat")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"persona_{{region}}": "{{persona_{{region}}}}",
		"policy":             "{{policy}}",
	}, pack.Fragments)
}

func TestPackCompiler_Fragments_PathResolvesAgainstPromptDir(t *testing.T) {
	repo := &dirRepository{
		mockRepository: newMockRepository(),
		dirs:           map[string]string{"chat": "/packs/support"},
		byPath:         map[string]*Fragment{"/packs/support/frags/intro.yaml": {Content: "from file"}},
	}
	repo.prompts["chat"] = fragmentConfig("chat", FragmentRef{Name: "intro", Path: "frags/intro.yaml", Required: true})

	pack, err := compileWithRepo(t, repo, "chat")
	require.NoError(t, err)
	assert.Equal(t, "from file", pack.Fragments["intro"])
}

// A Loader that does not implement FragmentLoader cannot load fragments, so
// the compiler keeps the v2.17.0 behavior for it: every reference, required or
// not, is written as its "{{name}}" placeholder rather than failing the compile.
func TestPackCompiler_Fragments_LoaderWithoutFragmentSupport(t *testing.T) {
	l := &orderedLoader{
		order: []string{"chat"},
		configs: map[string]*Config{
			"chat": fragmentConfig("chat", FragmentRef{Name: "intro", Required: true}),
			"opt":  fragmentConfig("opt", FragmentRef{Name: "intro", Required: false}),
		},
	}
	for _, taskType := range []string{"chat", "opt"} {
		pack, err := newOrderedCompiler(l).Compile(taskType, "test")
		require.NoError(t, err, "%s: a loader without fragment support must not fail the compile", taskType)
		assert.Equal(t, map[string]string{"intro": "{{intro}}"}, pack.Fragments, taskType)
	}
}

func TestRegistry_LoadTemplate_FragmentPathUsesPromptDir(t *testing.T) {
	repo := &dirRepository{
		mockRepository: newMockRepository(),
		dirs:           map[string]string{"chat": "/packs/support"},
		byPath:         map[string]*Fragment{"/packs/support/frags/intro.yaml": {Content: "from file"}},
	}
	repo.prompts["chat"] = fragmentConfig("chat", FragmentRef{Name: "intro", Path: "frags/intro.yaml", Required: true})

	tmpl, err := NewRegistryWithRepository(repo).LoadTemplate("chat", nil, "")
	require.NoError(t, err)
	assert.Equal(t, "from file", tmpl.FragmentVars["intro"])
	assert.Equal(t, []string{"/packs/support"}, repo.baseDirs)
}

func TestFragmentResolver_CacheKeyIncludesBaseDir(t *testing.T) {
	repo := &dirRepository{
		mockRepository: newMockRepository(),
		byPath: map[string]*Fragment{
			"/a/frag.yaml": {Content: "A"},
			"/b/frag.yaml": {Content: "B"},
		},
	}
	fr := NewFragmentResolverWithRepository(repo)
	a, err := fr.LoadFragment("frag", "frag.yaml", "/a")
	require.NoError(t, err)
	b, err := fr.LoadFragment("frag", "frag.yaml", "/b")
	require.NoError(t, err)
	assert.Equal(t, "A", a.Content)
	assert.Equal(t, "B", b.Content, "same relative path under another base dir is a different fragment")
}

func TestPackCompiler_Fragments_SameNameDifferentContentIsAnError(t *testing.T) {
	repo := &dirRepository{
		mockRepository: newMockRepository(),
		dirs:           map[string]string{"a": "/p/a", "b": "/p/b"},
		byPath: map[string]*Fragment{
			"/p/a/intro.yaml": {Content: "A"},
			"/p/b/intro.yaml": {Content: "B"},
		},
	}
	repo.prompts["a"] = fragmentConfig("a", FragmentRef{Name: "intro", Path: "intro.yaml", Required: true})
	repo.prompts["b"] = fragmentConfig("b", FragmentRef{Name: "intro", Path: "intro.yaml", Required: true})
	pc := NewPackCompilerWithDeps(NewRegistryWithRepository(repo),
		mockTimeProvider{fixedTime: time.Unix(0, 0)}, newMockFileWriter())

	_, err := pc.CompileFromRegistry("p", "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `fragment "intro"`)

	// The same content under one name is fine.
	repo.byPath["/p/b/intro.yaml"] = &Fragment{Content: "A"}
	pc = NewPackCompilerWithDeps(NewRegistryWithRepository(repo),
		mockTimeProvider{fixedTime: time.Unix(0, 0)}, newMockFileWriter())
	pack, err := pc.CompileFromRegistry("p", "test")
	require.NoError(t, err)
	assert.Equal(t, "A", pack.Fragments["intro"])
}

func TestRegistry_LoadTemplate_NameOnlyFragmentsSkipDirLookup(t *testing.T) {
	repo := &dirRepository{mockRepository: newMockRepository(), dirs: map[string]string{}}
	repo.fragments["intro"] = &Fragment{Content: "hi"}
	repo.prompts["chat"] = fragmentConfig("chat", FragmentRef{Name: "intro", Required: true})
	counting := &countingDirRepo{dirRepository: repo}

	_, err := NewRegistryWithRepository(counting).LoadTemplate("chat", nil, "")
	require.NoError(t, err)
	assert.Zero(t, counting.calls, "no fragment has a path, so the prompt dir is not needed")
}

type countingDirRepo struct {
	*dirRepository
	calls int
}

func (c *countingDirRepo) PromptDir(taskType string) (string, error) {
	c.calls++
	return c.dirRepository.PromptDir(taskType)
}
