package templates

import (
	"time"
)

// Template represents a project template configuration
type Template struct {
	APIVersion string           `yaml:"apiVersion" json:"apiVersion"`
	Kind       string           `yaml:"kind" json:"kind"`
	Metadata   TemplateMetadata `yaml:"metadata" json:"metadata"`
	Spec       TemplateSpec     `yaml:"spec" json:"spec"`
	BaseDir    string           `yaml:"-" json:"-"` // directory containing the template file (for source resolution)
	BaseURL    string           `yaml:"-" json:"-"` // base URL for remote templates (for source resolution)
}

// TemplateMetadata contains template identification and metadata
type TemplateMetadata struct {
	Name        string            `yaml:"name" json:"name"`
	Labels      map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
	Annotations map[string]string `yaml:"annotations,omitempty" json:"annotations,omitempty"`
}

// TemplateSpec defines the template specification
type TemplateSpec struct {
	Variables []Variable    `yaml:"variables,omitempty" json:"variables,omitempty"`
	Files     []FileSpec    `yaml:"files" json:"files"`
	Hooks     *HookSet      `yaml:"hooks,omitempty" json:"hooks,omitempty"`
	Examples  *Examples     `yaml:"examples,omitempty" json:"examples,omitempty"`
	Docs      *Docs         `yaml:"docs,omitempty" json:"docs,omitempty"`
	Requires  *Requirements `yaml:"requires,omitempty" json:"requires,omitempty"`
}

// Variable defines a template variable that can be customized
type Variable struct {
	Name        string      `yaml:"name" json:"name"`
	Description string      `yaml:"description,omitempty" json:"description,omitempty"`
	Type        string      `yaml:"type,omitempty" json:"type,omitempty"` // string, number, boolean, array, select
	Default     interface{} `yaml:"default,omitempty" json:"default,omitempty"`
	Required    bool        `yaml:"required,omitempty" json:"required,omitempty"`
	Validation  string      `yaml:"validation,omitempty" json:"validation,omitempty"` // regex pattern
	Options     []string    `yaml:"options,omitempty" json:"options,omitempty"`       // for select/array types
	Prompt      string      `yaml:"prompt,omitempty" json:"prompt,omitempty"`         // interactive prompt text
}

// FileSpec defines a file to be generated from the template
type FileSpec struct {
	Path     string `yaml:"path" json:"path"`
	Template string `yaml:"template,omitempty" json:"template,omitempty"`
	Content  string `yaml:"content,omitempty" json:"content,omitempty"`
	// Source is an external file path relative to the template directory
	Source     string            `yaml:"source,omitempty" json:"source,omitempty"`
	Condition  string            `yaml:"condition,omitempty" json:"condition,omitempty"`
	ForEach    string            `yaml:"foreach,omitempty" json:"foreach,omitempty"`
	Variables  map[string]string `yaml:"variables,omitempty" json:"variables,omitempty"`
	Executable bool              `yaml:"executable,omitempty" json:"executable,omitempty"`
}

// HookSet defines pre and post generation hooks
type HookSet struct {
	PreCreate  []Hook `yaml:"pre_create,omitempty" json:"pre_create,omitempty"`
	PostCreate []Hook `yaml:"post_create,omitempty" json:"post_create,omitempty"`
}

// Hook defines a command to run during generation
type Hook struct {
	Message   string `yaml:"message,omitempty" json:"message,omitempty"`
	Command   string `yaml:"command" json:"command"`
	Condition string `yaml:"condition,omitempty" json:"condition,omitempty"`
	Required  bool   `yaml:"required,omitempty" json:"required,omitempty"`
}

// Examples contains example data for quick testing
type Examples struct {
	Scenarios []string `yaml:"scenarios,omitempty" json:"scenarios,omitempty"`
	Providers []string `yaml:"providers,omitempty" json:"providers,omitempty"`
	UseCases  []string `yaml:"use_cases,omitempty" json:"use_cases,omitempty"`
}

// Docs contains documentation links
type Docs struct {
	GettingStarted string `yaml:"getting_started,omitempty" json:"getting_started,omitempty"`
	APIReference   string `yaml:"api_reference,omitempty" json:"api_reference,omitempty"`
	Examples       string `yaml:"examples,omitempty" json:"examples,omitempty"`
}

// Requirements defines template requirements
type Requirements struct {
	CLIVersion string   `yaml:"cli_version,omitempty" json:"cli_version,omitempty"`
	Tools      []string `yaml:"tools,omitempty" json:"tools,omitempty"`
}

// TemplateConfig holds the configuration for template generation
type TemplateConfig struct {
	ProjectName string
	OutputDir   string
	Variables   map[string]interface{}
	Template    *Template
	Verbose     bool // Enable detailed logging
}

// GenerationResult contains the result of template generation
type GenerationResult struct {
	Success      bool
	ProjectPath  string
	FilesCreated []string
	Errors       []error
	Warnings     []string
	Duration     time.Duration
}

// TemplateSource represents where a template comes from
type TemplateSource int

const (
	SourceBuiltIn TemplateSource = iota
	SourceRemote
	SourceLocal
)

// TemplateInfo contains metadata about a template
type TemplateInfo struct {
	Name        string
	Version     string
	Description string
	Author      string
	Tags        []string
	Source      TemplateSource
	Verified    bool
	Downloads   int
	Path        string
}
