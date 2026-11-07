package prompt

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
)

// orderedLoader is a Loader whose ListTaskTypes returns task types in exactly
// the order given, so a test can make the compiler see them in an order that
// is not sorted.
type orderedLoader struct {
	order   []string
	configs map[string]*Config
}

func (l *orderedLoader) LoadConfig(taskType string) (*Config, error) {
	c, ok := l.configs[taskType]
	if !ok {
		return nil, fmt.Errorf("no config %q", taskType)
	}
	return c, nil
}

func (l *orderedLoader) ListTaskTypes() []string { return l.order }

func engineConfig(taskType string, engine *TemplateEngineInfo) *Config {
	return &Config{
		Metadata: metav1.ObjectMeta{Name: taskType},
		Spec: Spec{
			TaskType:       taskType,
			Version:        "1.0.0",
			SystemTemplate: "hi",
			TemplateEngine: engine,
		},
	}
}

func newOrderedCompiler(l *orderedLoader) *PackCompiler {
	return NewPackCompilerWithDeps(l, mockTimeProvider{fixedTime: time.Unix(0, 0)}, newMockFileWriter())
}

func TestCompileFromRegistry_TemplateEngine_DisagreementIsAnError(t *testing.T) {
	l := &orderedLoader{
		order: []string{"b", "a"},
		configs: map[string]*Config{
			"a": engineConfig("a", &TemplateEngineInfo{Version: "v1", Syntax: "{{variable}}"}),
			"b": engineConfig("b", &TemplateEngineInfo{Version: "v2", Syntax: "{{variable}}"}),
		},
	}
	_, err := newOrderedCompiler(l).CompileFromRegistry("p", "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"a"`)
	assert.Contains(t, err.Error(), `"b"`)
	assert.Contains(t, err.Error(), "template engine")
	// Prompts are compiled in sorted order, so "a" is always the reference.
	assert.Contains(t, err.Error(), `"a" uses v1 {{variable}}, but "b" (v2 {{variable}})`)
}

func TestCompileFromRegistry_TemplateEngine_DeclaredBeatsDefault(t *testing.T) {
	declared := &TemplateEngineInfo{Version: "v2", Syntax: "{{variable}}"}
	// "a" sorts first and carries the registry default; "b" declares its own.
	// The default must not win just because its prompt sorts (or iterates) first.
	for _, order := range [][]string{{"a", "b"}, {"b", "a"}} {
		l := &orderedLoader{
			order: order,
			configs: map[string]*Config{
				"a": engineConfig("a", defaultTemplateEngine()),
				"b": engineConfig("b", declared),
			},
		}
		pack, err := newOrderedCompiler(l).CompileFromRegistry("p", "test")
		require.NoError(t, err)
		assert.Equal(t, declared, pack.TemplateEngine, "order %v", order)
	}
}

func TestCompileFromRegistry_TemplateEngine_AgreeingAndDefaulted(t *testing.T) {
	same := func() *TemplateEngineInfo { return &TemplateEngineInfo{Version: "v2", Syntax: "<<x>>"} }
	l := &orderedLoader{
		order: []string{"c", "b", "a"},
		configs: map[string]*Config{
			"a": engineConfig("a", same()),
			"b": engineConfig("b", same()),
			"c": engineConfig("c", nil),
		},
	}
	pack, err := newOrderedCompiler(l).CompileFromRegistry("p", "test")
	require.NoError(t, err)
	assert.Equal(t, same(), pack.TemplateEngine)

	// Every prompt defaulted: the pack carries the default.
	l = &orderedLoader{
		order:   []string{"b", "a"},
		configs: map[string]*Config{"a": engineConfig("a", nil), "b": engineConfig("b", defaultTemplateEngine())},
	}
	pack, err = newOrderedCompiler(l).CompileFromRegistry("p", "test")
	require.NoError(t, err)
	assert.Equal(t, defaultTemplateEngine(), pack.TemplateEngine)
}

func TestCompile_SetsSchemaURL(t *testing.T) {
	l := &orderedLoader{
		order:   []string{"a"},
		configs: map[string]*Config{"a": engineConfig("a", nil)},
	}
	pack, err := newOrderedCompiler(l).Compile("a", "test")
	require.NoError(t, err)
	require.NotNil(t, pack.Schema)
	assert.Equal(t, PromptPackSchemaURL, *pack.Schema)
}

func TestPack_ListPrompts_Sorted(t *testing.T) {
	p := &Pack{Pack: packspec.Pack{Prompts: map[string]*PackPrompt{}}}
	for i := 0; i < 40; i++ {
		p.Prompts[fmt.Sprintf("task-%02d", 39-i)] = &PackPrompt{}
	}
	got := p.ListPrompts()
	assert.Len(t, got, 40)
	assert.True(t, sort.StringsAreSorted(got), "ListPrompts must be sorted, got %v", got)
}

func TestPack_ListTools_Sorted(t *testing.T) {
	p := &Pack{Pack: packspec.Pack{Tools: map[string]*PackTool{}}}
	for i := 0; i < 40; i++ {
		p.Tools[fmt.Sprintf("tool-%02d", 39-i)] = &PackTool{}
	}
	got := p.ListTools()
	assert.Len(t, got, 40)
	assert.True(t, sort.StringsAreSorted(got), "ListTools must be sorted, got %v", got)
}

func validPackForSeverity() *Pack {
	return &Pack{Pack: packspec.Pack{
		ID:             "p",
		Name:           "p",
		Version:        "1.0.0",
		TemplateEngine: &TemplateEngineInfo{Version: "v1", Syntax: "{{variable}}"},
		Compilation:    &CompilationInfo{CompiledWith: "x", CreatedAt: "now", Schema: "v1"},
		Prompts: map[string]*PackPrompt{
			"chat": {ID: "chat", Name: "chat", Version: "1.0.0", SystemTemplate: "hi", Tools: []string{"search"}},
		},
		Tools: map[string]*PackTool{"search": {Name: "search"}},
	}}
}

func TestPack_ValidateDetailed_CleanPack(t *testing.T) {
	r := validPackForSeverity().ValidateDetailed()
	assert.Empty(t, r.Errors)
	assert.Empty(t, r.Warnings)
}

func TestPack_ValidateDetailed_MissingCompilationIsAWarning(t *testing.T) {
	p := validPackForSeverity()
	p.Compilation = nil
	r := p.ValidateDetailed()
	assert.Empty(t, r.Errors)
	assert.Equal(t, []string{"missing compilation metadata"}, r.Warnings)
	assert.Contains(t, p.Validate(), "missing compilation metadata")
}

func TestPack_ValidateDetailed_UndeclaredPromptToolIsAWarning(t *testing.T) {
	p := validPackForSeverity()
	p.Prompts["chat"].Tools = []string{"search", "lookup"}
	p.Prompts["other"] = &PackPrompt{
		ID: "other", Name: "other", Version: "1.0.0", SystemTemplate: "hi", Tools: []string{"zap"},
	}
	r := p.ValidateDetailed()
	assert.Empty(t, r.Errors)
	want := []string{
		"prompt 'chat': tool 'lookup' is not defined in the pack's tools",
		"prompt 'other': tool 'zap' is not defined in the pack's tools",
	}
	assert.Equal(t, want, r.Warnings)
	// Validate's callers treat its findings as blocking, so it leaves these out.
	assert.Empty(t, p.Validate())
}

func TestPack_ValidateDetailed_StructuralFindingsAreErrors(t *testing.T) {
	p := validPackForSeverity()
	p.ID = ""
	p.TemplateEngine = nil
	p.Prompts["chat"].SystemTemplate = ""
	r := p.ValidateDetailed()
	assert.Contains(t, r.Errors, "missing required field: id")
	assert.Contains(t, r.Errors, "missing template_engine configuration")
	assert.Contains(t, r.Errors, "prompt 'chat': missing system_template")
	assert.Empty(t, r.Warnings)
}

func TestCompileFromRegistry_TemplateEngine_EmptyAndAbsentFeaturesAgree(t *testing.T) {
	l := &orderedLoader{
		order: []string{"a", "b"},
		configs: map[string]*Config{
			"a": engineConfig("a", &TemplateEngineInfo{Version: "v2", Syntax: "<<x>>", Features: []string{}}),
			"b": engineConfig("b", &TemplateEngineInfo{Version: "v2", Syntax: "<<x>>"}),
		},
	}
	pack, err := newOrderedCompiler(l).CompileFromRegistry("p", "test")
	require.NoError(t, err)
	assert.Equal(t, "v2", pack.TemplateEngine.Version)
	assert.Equal(t, "<<x>>", pack.TemplateEngine.Syntax)
}
