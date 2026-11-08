package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/template"
)

// mockFragmentRepository is a simple in-memory repository for testing
type mockFragmentRepository struct {
	fragments map[string]*Fragment
	basePath  string
}

func newMockFragmentRepository(basePath string) *mockFragmentRepository {
	return &mockFragmentRepository{
		fragments: make(map[string]*Fragment),
		basePath:  basePath,
	}
}

func (m *mockFragmentRepository) LoadFragment(name string, relativePath string, baseDir string) (*Fragment, error) {
	if f, ok := m.fragments[name]; ok {
		return f, nil
	}

	// Try to load from filesystem for testing
	var filePath string
	if relativePath != "" {
		filePath = filepath.Join(baseDir, relativePath)
	} else {
		filePath = filepath.Join(m.basePath, name+".yaml")
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("fragment not found: %s: %w", name, err)
	}

	// Parse simple format for testing
	content := string(data)
	fragment := &Fragment{
		Content: content,
	}
	m.fragments[name] = fragment

	return fragment, nil
}

func TestNewFragmentResolverWithRepository(t *testing.T) {
	repo := newMockFragmentRepository("")

	resolver := NewFragmentResolverWithRepository(repo)

	if resolver == nil {
		t.Fatal("Expected non-nil resolver")
	}

	if resolver.repository == nil {
		t.Error("Expected non-nil repository")
	}

	if resolver.fragmentCache == nil {
		t.Error("Expected non-nil fragmentCache")
	}
}

func TestFragmentResolver_ResolveVariables_Empty(t *testing.T) {
	repo := newMockFragmentRepository("")
	resolver := NewFragmentResolverWithRepository(repo)

	// Empty vars
	result := resolver.resolveVariables("hello {{name}}", map[string]string{})
	expected := "hello {{name}}" // No substitution
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestFragmentResolver_ResolveVariables_MultipleOccurrences(t *testing.T) {
	repo := newMockFragmentRepository("")
	resolver := NewFragmentResolverWithRepository(repo)

	vars := map[string]string{"name": "Alice"}
	result := resolver.resolveVariables("{{name}} says hello to {{name}}", vars)
	expected := "Alice says hello to Alice"
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestFragmentResolver_AssembleFragments_WithCache(t *testing.T) {
	// Create temp directory for test
	tmpDir := t.TempDir()

	// Create a fragment file
	fragmentPath := filepath.Join(tmpDir, "test_fragment.yaml")
	fragmentContent := `name: test_fragment
content: "This is test content"
`
	if err := os.WriteFile(fragmentPath, []byte(fragmentContent), 0644); err != nil {
		t.Fatal(err)
	}

	repo := newMockFragmentRepository(tmpDir)
	resolver := NewFragmentResolverWithRepository(repo)

	fragments := []FragmentRef{
		{Name: "test_fragment", Required: true},
	}

	vars := make(map[string]string)
	configPath := filepath.Join(tmpDir, "config.yaml")
	result, err := resolver.AssembleFragments(fragments, vars, configPath)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if result["test_fragment"] == "" {
		t.Errorf("Unexpected empty fragment content")
	}
}

func TestFragmentResolver_AssembleFragments_OptionalMissing(t *testing.T) {
	tmpDir := t.TempDir()

	repo := newMockFragmentRepository(tmpDir)
	resolver := NewFragmentResolverWithRepository(repo)

	fragments := []FragmentRef{
		{Name: "nonexistent", Required: false}, // Optional
	}

	vars := make(map[string]string)
	configPath := filepath.Join(tmpDir, "config.yaml")

	result, err := resolver.AssembleFragments(fragments, vars, configPath)

	// Should succeed even though fragment is missing
	if err != nil {
		t.Errorf("Expected no error for optional fragment, got: %v", err)
	}

	// The missing optional fragment renders empty, so its placeholder does not
	// fail the render (#2523).
	if want := map[string]string{"nonexistent": ""}; !reflect.DeepEqual(result, want) {
		t.Errorf("result = %v, want %v", result, want)
	}
	rendered, err := template.NewRenderer().Render("a{{nonexistent}}b", result)
	if err != nil || rendered != "ab" {
		t.Errorf("Render = %q, %v; want \"ab\"", rendered, err)
	}

	// A variable the caller set under that name still fills the placeholder.
	result, err = resolver.AssembleFragments(fragments, map[string]string{"nonexistent": "set"}, configPath)
	if err != nil || len(result) != 0 {
		t.Errorf("with a caller variable: result = %v, err = %v; want no fragment var", result, err)
	}
}

func TestFragmentResolver_AssembleFragments_RequiredMissing(t *testing.T) {
	tmpDir := t.TempDir()

	repo := newMockFragmentRepository(tmpDir)
	resolver := NewFragmentResolverWithRepository(repo)

	fragments := []FragmentRef{
		{Name: "nonexistent", Required: true}, // Required
	}

	vars := make(map[string]string)
	configPath := filepath.Join(tmpDir, "config.yaml")

	_, err := resolver.AssembleFragments(fragments, vars, configPath)

	// Should fail because fragment is required
	if err == nil {
		t.Error("Expected error for missing required fragment")
	}
}

func TestFragmentResolver_AssembleFragments_DynamicNames(t *testing.T) {
	tmpDir := t.TempDir()

	// Create fragment file with dynamic name
	fragmentPath := filepath.Join(tmpDir, "persona_us.yaml")
	fragmentContent := `name: persona_us
content: "US persona content"
`
	if err := os.WriteFile(fragmentPath, []byte(fragmentContent), 0644); err != nil {
		t.Fatal(err)
	}

	repo := newMockFragmentRepository(tmpDir)
	resolver := NewFragmentResolverWithRepository(repo)

	fragments := []FragmentRef{
		{Name: "persona_{{region}}", Required: true},
	}

	vars := map[string]string{"region": "us"}
	configPath := filepath.Join(tmpDir, "config.yaml")

	result, err := resolver.AssembleFragments(fragments, vars, configPath)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Result key should use resolved name
	if result["persona_us"] == "" {
		t.Errorf("Unexpected empty content")
	}
}

func TestFragmentResolver_LoadFragment_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()

	// Create invalid YAML file
	fragmentPath := filepath.Join(tmpDir, "invalid.yaml")
	invalidYAML := "this is: not: valid: yaml: content:"
	if err := os.WriteFile(fragmentPath, []byte(invalidYAML), 0644); err != nil {
		t.Fatal(err)
	}

	repo := newMockFragmentRepository(tmpDir)
	resolver := NewFragmentResolverWithRepository(repo)

	configPath := filepath.Join(tmpDir, "config.yaml")
	_, err := resolver.LoadFragment("invalid", "", configPath)

	// We don't actually validate YAML in this simple test - the actual behavior depends on implementation
	_ = err // May or may not error depending on how the mock handles it
}

func TestTemplateRenderer_Render_NoVariables(t *testing.T) {
	renderer := template.NewRenderer()

	result, err := renderer.Render("Hello World", map[string]string{})

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if result != "Hello World" {
		t.Errorf("Expected 'Hello World', got '%s'", result)
	}
}

func TestTemplateRenderer_Render_EmptyTemplate(t *testing.T) {
	renderer := template.NewRenderer()

	result, err := renderer.Render("", map[string]string{"key": "value"})

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if result != "" {
		t.Errorf("Expected empty string, got '%s'", result)
	}
}

func TestTemplateRenderer_Render_RecursiveSubstitution(t *testing.T) {
	renderer := template.NewRenderer()

	vars := map[string]string{
		"name":     "Alice",
		"greeting": "Hello {{name}}",
	}

	result, err := renderer.Render("{{greeting}}!", vars)

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	expected := "Hello Alice!"
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestTemplateRenderer_Render_DeepNesting(t *testing.T) {
	renderer := template.NewRenderer()

	// Test that 3 passes is sufficient for moderate nesting (5 levels)
	vars := map[string]string{
		"a": "{{b}}",
		"b": "{{c}}",
		"c": "{{d}}",
		"d": "{{e}}",
		"e": "{{f}}",
		"f": "value",
	}

	result, err := renderer.Render("{{a}}", vars)

	// With 3 passes, this might partially resolve or fully resolve
	// The test verifies it handles deep nesting without crashing
	if err != nil {
		// It's OK if it errors on unresolved placeholders
		t.Logf("Got expected error for very deep nesting: %v", err)
	} else if result == "value" {
		// Or it might fully resolve
		t.Logf("Successfully resolved deep nesting to: %s", result)
	} else {
		// Or partially resolve
		t.Logf("Partially resolved to: %s", result)
	}
}

func TestGetUsedVars_MixedValues(t *testing.T) {
	vars := map[string]string{
		"used1":  "value1",
		"empty":  "",
		"used2":  "value2",
		"empty2": "",
	}

	used := GetUsedVars(vars)

	// Should only return non-empty vars
	if len(used) != 2 {
		t.Errorf("Expected 2 used vars, got %d: %v", len(used), used)
	}

	// Verify used vars contain the non-empty ones
	usedMap := make(map[string]bool)
	for _, key := range used {
		usedMap[key] = true
	}

	if !usedMap["used1"] || !usedMap["used2"] {
		t.Error("Expected 'used1' and 'used2' in used vars")
	}
}

func TestGetUsedVars_NilMap(t *testing.T) {
	var vars map[string]string // nil map

	used := GetUsedVars(vars)

	if len(used) != 0 {
		t.Errorf("Expected 0 used vars for nil map, got %d", len(used))
	}
}

func TestNewTemplateRenderer(t *testing.T) {
	renderer := template.NewRenderer()

	if renderer == nil {
		t.Fatal("Expected non-nil renderer")
	}
}

func TestFragmentResolver_AssembleFragments_MultipleFragments(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple fragment files
	fragments := map[string]string{
		"frag1": "content: \"Fragment 1\"\n",
		"frag2": "content: \"Fragment 2\"\n",
		"frag3": "content: \"Fragment 3\"\n",
	}

	for name, content := range fragments {
		path := filepath.Join(tmpDir, name+".yaml")
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	repo := newMockFragmentRepository(tmpDir)
	resolver := NewFragmentResolverWithRepository(repo)

	fragmentRefs := []FragmentRef{
		{Name: "frag1", Required: true},
		{Name: "frag2", Required: true},
		{Name: "frag3", Required: true},
	}

	vars := make(map[string]string)
	configPath := filepath.Join(tmpDir, "config.yaml")

	result, err := resolver.AssembleFragments(fragmentRefs, vars, configPath)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(result) != 3 {
		t.Errorf("Expected 3 fragments, got %d", len(result))
	}

	for i := 1; i <= 3; i++ {
		key := fmt.Sprintf("frag%d", i)
		if result[key] == "" {
			t.Errorf("Fragment %s is empty", key)
		}
	}
}

// baseDirRecordingRepository serves fragments from files keyed by the full
// path a file repository would read, recording every base dir it is asked for.
type baseDirRecordingRepository struct {
	files map[string]string // joined path -> content
	calls []string          // baseDir of each LoadFragment call
}

func (r *baseDirRecordingRepository) LoadFragment(_, relativePath, baseDir string) (*Fragment, error) {
	r.calls = append(r.calls, baseDir)
	path := relativePath
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, relativePath)
	}
	content, ok := r.files[path]
	if !ok {
		return nil, fmt.Errorf("failed to read fragment: %w", os.ErrNotExist)
	}
	return &Fragment{Content: content}, nil
}

// An absolute path names one file whatever the base dir, so it is one cache
// entry, not one per base dir.
func TestFragmentResolver_AbsolutePathIsOneCacheEntry(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(root, "shared", "intro.yaml")
	repo := &baseDirRecordingRepository{files: map[string]string{abs: "shared"}}
	fr := NewFragmentResolverWithRepository(repo)
	for _, base := range []string{filepath.Join(root, "a"), filepath.Join(root, "b")} {
		frag, err := fr.LoadFragment("intro", abs, base)
		if err != nil {
			t.Fatal(err)
		}
		if frag.Content != "shared" {
			t.Fatalf("content = %q", frag.Content)
		}
	}
	if len(repo.calls) != 1 {
		t.Fatalf("repository loaded the absolute path %d times, want 1 (calls: %v)", len(repo.calls), repo.calls)
	}
	if repo.calls[0] != "" {
		t.Errorf("an absolute path was loaded with base dir %q, want none", repo.calls[0])
	}
}

// A relative path not found under the base dir falls back to the repository's
// own default base (""), which for the file repositories is the working
// directory.
func TestFragmentResolver_RelativePathFallsBackToDefaultBase(t *testing.T) {
	repo := &baseDirRecordingRepository{files: map[string]string{"frags/intro.yaml": "cwd"}}
	fr := NewFragmentResolverWithRepository(repo)
	frag, err := fr.LoadFragment("intro", "frags/intro.yaml", "/prompts")
	if err != nil {
		t.Fatal(err)
	}
	if frag.Content != "cwd" {
		t.Fatalf("content = %q, want the fallback's", frag.Content)
	}
	if want := []string{"/prompts", ""}; fmt.Sprint(repo.calls) != fmt.Sprint(want) {
		t.Fatalf("base dirs tried = %q, want %q", repo.calls, want)
	}
	// The fallback result is cached for the base dir it was asked for.
	if _, err := fr.LoadFragment("intro", "frags/intro.yaml", "/prompts"); err != nil {
		t.Fatal(err)
	}
	if len(repo.calls) != 2 {
		t.Errorf("second load hit the repository again: %v", repo.calls)
	}
}

// Only a missing file falls back: any other failure under the base dir (a
// fragment that exists but cannot be parsed) is returned as it is.
func TestFragmentResolver_NonMissingErrorDoesNotFallBack(t *testing.T) {
	repo := &failingFragmentRepository{err: fmt.Errorf("failed to parse fragment YAML")}
	fr := NewFragmentResolverWithRepository(repo)
	if _, err := fr.LoadFragment("intro", "frags/intro.yaml", "/prompts"); err == nil {
		t.Fatal("want the parse error")
	}
	if repo.calls != 1 {
		t.Errorf("repository called %d times, want 1", repo.calls)
	}
}

// A file missing under the base dir but found and unusable under the default
// base reports the default base's error, not "not found".
func TestFragmentResolver_FallbackFailureIsReported(t *testing.T) {
	repo := &perBaseFragmentRepository{errs: map[string]error{
		"/prompts": fmt.Errorf("failed to read fragment: %w", os.ErrNotExist),
		"":         fmt.Errorf("failed to parse fragment YAML: bad indent"),
	}}
	fr := NewFragmentResolverWithRepository(repo)
	_, err := fr.LoadFragment("intro", "frags/intro.yaml", "/prompts")
	if err == nil || !strings.Contains(err.Error(), "bad indent") {
		t.Fatalf("err = %v, want the fallback's parse error", err)
	}
}

type perBaseFragmentRepository struct{ errs map[string]error }

func (r *perBaseFragmentRepository) LoadFragment(_, _, baseDir string) (*Fragment, error) {
	return nil, r.errs[baseDir]
}

type failingFragmentRepository struct {
	err   error
	calls int
}

func (r *failingFragmentRepository) LoadFragment(string, string, string) (*Fragment, error) {
	r.calls++
	return nil, r.err
}
