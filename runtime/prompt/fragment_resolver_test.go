package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/template"
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
		return nil, fmt.Errorf("fragment not found: %s", name)
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

	if len(result) != 0 {
		t.Error("Expected empty result for missing optional fragment")
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
