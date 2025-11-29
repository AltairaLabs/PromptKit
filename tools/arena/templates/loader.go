package templates

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed builtin/*
var builtinTemplates embed.FS

// Loader handles loading templates from various sources
type Loader struct {
	cacheDir string
}

// NewLoader creates a new template loader
func NewLoader(cacheDir string) *Loader {
	if cacheDir == "" {
		cacheDir = DefaultCacheDir()
	}
	return &Loader{
		cacheDir: cacheDir,
	}
}

// Load loads a template by name from any source
func (l *Loader) Load(name string) (*Template, error) {
	// Try built-in first
	if tmpl, _ := l.LoadBuiltIn(name); tmpl != nil {
		return tmpl, nil
	}

	// Try local file
	if strings.HasPrefix(name, "./") || strings.HasPrefix(name, "/") {
		return l.LoadFromFile(name)
	}

	// Try remote registry (default index)
	if tmpl, err := l.LoadFromRegistry(name); err == nil {
		return tmpl, nil
	}

	return nil, fmt.Errorf("template not found: %s", name)
}

// LoadBuiltIn loads a built-in template
func (l *Loader) LoadBuiltIn(name string) (*Template, error) {
	templatePath := fmt.Sprintf("builtin/%s/template.yaml", name)
	data, err := builtinTemplates.ReadFile(templatePath)
	if err != nil {
		return nil, fmt.Errorf("built-in template %s not found: %w", name, err)
	}

	var tmpl Template
	if err := yaml.Unmarshal(data, &tmpl); err != nil {
		return nil, fmt.Errorf("failed to parse template %s: %w", name, err)
	}

	if err := l.validate(&tmpl); err != nil {
		return nil, fmt.Errorf("invalid template %s: %w", name, err)
	}

	tmpl.BaseDir = "" // built-ins resolve via embedded files
	return &tmpl, nil
}

// LoadFromFile loads a template from a file path
func (l *Loader) LoadFromFile(path string) (*Template, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read template file: %w", err)
	}

	var tmpl Template
	if err := yaml.Unmarshal(data, &tmpl); err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	if err := l.validate(&tmpl); err != nil {
		return nil, fmt.Errorf("invalid template: %w", err)
	}

	tmpl.BaseDir = filepath.Dir(path)
	return &tmpl, nil
}

// ListBuiltIn returns a list of all built-in templates
func (l *Loader) ListBuiltIn() ([]TemplateInfo, error) {
	entries, err := builtinTemplates.ReadDir("builtin")
	if err != nil {
		return nil, fmt.Errorf("failed to read built-in templates: %w", err)
	}

	var templates []TemplateInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		tmpl, err := l.LoadBuiltIn(entry.Name())
		if err != nil {
			continue // Skip invalid templates
		}

		info := TemplateInfo{
			Name:        tmpl.Metadata.Name,
			Version:     tmpl.Metadata.Labels["version"],
			Description: tmpl.Metadata.Annotations["description"],
			Author:      tmpl.Metadata.Annotations["author"],
			Source:      SourceBuiltIn,
			Verified:    true,
			Path:        entry.Name(),
		}

		if tags := tmpl.Metadata.Annotations["tags"]; tags != "" {
			info.Tags = strings.Split(tags, ",")
		}

		templates = append(templates, info)
	}

	return templates, nil
}

// LoadFromRegistry fetches a template using the default index and cache.
func (l *Loader) LoadFromRegistry(ref string) (*Template, error) {
	name, version := parseTemplateRef(ref)
	index, err := LoadIndex(DefaultIndex)
	if err != nil {
		return nil, fmt.Errorf("load index: %w", err)
	}
	entry, err := index.FindEntry(name, version)
	if err != nil {
		return nil, err
	}
	path, err := FetchTemplate(entry, l.cacheDir)
	if err != nil {
		return nil, err
	}
	return l.LoadFromFile(path)
}

// parseTemplateRef splits "name@version" into name/version.
func parseTemplateRef(ref string) (name, version string) {
	parts := strings.Split(ref, "@")
	if len(parts) == 2 { //nolint:mnd // ref format is name@version
		return parts[0], parts[1]
	}
	return ref, ""
}

// compareSemver compares two semver-ish strings (major.minor.patch), missing parts treated as 0.
func compareSemver(a, b string) int {
	split := func(v string) []int {
		parts := strings.Split(v, ".")
		out := []int{0, 0, 0}
		for i := 0; i < len(parts) && i < 3; i++ {
			n, _ := strconv.Atoi(parts[i])
			out[i] = n
		}
		return out
	}
	va := split(a)
	vb := split(b)
	for i := 0; i < 3; i++ {
		if va[i] > vb[i] {
			return 1
		}
		if va[i] < vb[i] {
			return -1
		}
	}
	return 0
}

// ReadTemplateFile reads a template file from the built-in templates
func (l *Loader) ReadTemplateFile(templateName, filePath string) ([]byte, error) {
	fullPath := filepath.Join("builtin", templateName, filePath)
	data, err := builtinTemplates.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read template file %s: %w", filePath, err)
	}
	return data, nil
}

// validate performs basic validation on a template
func (l *Loader) validate(tmpl *Template) error {
	if tmpl.APIVersion == "" {
		return fmt.Errorf("apiVersion is required")
	}

	if tmpl.Kind != "Template" {
		return fmt.Errorf("kind must be 'Template', got '%s'", tmpl.Kind)
	}

	if tmpl.Metadata.Name == "" {
		return fmt.Errorf("metadata.name is required")
	}

	if len(tmpl.Spec.Files) == 0 {
		return fmt.Errorf("spec.files must not be empty")
	}

	// Validate variables
	for i, v := range tmpl.Spec.Variables {
		if v.Name == "" {
			return fmt.Errorf("spec.variables[%d].name is required", i)
		}
	}

	// Validate files
	for i, f := range tmpl.Spec.Files {
		if f.Path == "" {
			return fmt.Errorf("spec.files[%d].path is required", i)
		}
		if f.Template == "" && f.Content == "" && f.Source == "" {
			return fmt.Errorf("spec.files[%d] must have either template, source or content", i)
		}
	}

	return nil
}
