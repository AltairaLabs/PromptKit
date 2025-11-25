package templates

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
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

	// Try remote (future implementation)
	// For now, return error
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
		if f.Template == "" && f.Content == "" {
			return fmt.Errorf("spec.files[%d] must have either template or content", i)
		}
	}

	return nil
}
