// Package compiler provides a reusable compilation pipeline for PromptKit packs.
// It extracts the core compilation logic from the packc CLI so it can be used
// programmatically by other tools and libraries.
package compiler

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

// CompileResult contains the output of a successful compilation.
type CompileResult struct {
	// Pack is the compiled prompt pack.
	Pack *prompt.Pack
	// JSON is the pack serialized as indented JSON.
	JSON []byte
	// Warnings contains non-fatal warnings from the compilation process.
	Warnings []string
}

// Option configures the compilation behavior.
type Option func(*compileOptions)

type compileOptions struct {
	packID               string
	compilerVersion      string
	skipSchemaValidation bool
}

// WithPackID sets the pack identifier. If not provided, a default is derived
// from the config file's parent directory name.
func WithPackID(id string) Option {
	return func(o *compileOptions) {
		o.packID = id
	}
}

// WithCompilerVersion sets the compiler version string embedded in the pack.
// If not provided, defaults to "compiler-dev".
func WithCompilerVersion(v string) Option {
	return func(o *compileOptions) {
		o.compilerVersion = v
	}
}

// WithSkipSchemaValidation disables JSON schema validation of the compiled pack.
func WithSkipSchemaValidation() Option {
	return func(o *compileOptions) {
		o.skipSchemaValidation = true
	}
}

// Compile loads an arena config file and compiles all prompts into a single pack.
// It performs media validation, skill validation, workflow validation, and
// schema validation (unless skipped). Errors are returned rather than printed
// or causing os.Exit.
func Compile(configFile string, opts ...Option) (*CompileResult, error) {
	options := applyOptions(opts)

	if err := resolvePackID(&options, configFile); err != nil {
		return nil, err
	}

	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		return nil, fmt.Errorf("loading arena config: %w", err)
	}

	pack, warnings, err := compilePack(cfg, configFile, options)
	if err != nil {
		return nil, err
	}

	configDir := filepath.Dir(configFile)
	if valErr := validatePack(pack, configDir, &warnings); valErr != nil {
		return nil, valErr
	}

	data, err := json.MarshalIndent(pack, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling pack: %w", err)
	}

	if !options.skipSchemaValidation {
		if err := validateSchema(data); err != nil {
			return nil, err
		}
	}

	return &CompileResult{
		Pack:     pack,
		JSON:     data,
		Warnings: warnings,
	}, nil
}

// applyOptions applies functional options and returns the resolved configuration.
func applyOptions(opts []Option) compileOptions {
	options := compileOptions{
		compilerVersion: "compiler-dev",
	}
	for _, opt := range opts {
		opt(&options)
	}
	return options
}

// resolvePackID derives the pack ID from the config file path if not explicitly set.
func resolvePackID(options *compileOptions, configFile string) error {
	if options.packID != "" {
		return nil
	}
	absPath, err := filepath.Abs(configFile)
	if err != nil {
		return fmt.Errorf("resolving config path: %w", err)
	}
	options.packID = sanitizePackID(filepath.Base(filepath.Dir(absPath)))
	return nil
}

// compilePack performs the core compilation: builds registry, parses config sections,
// and runs the pack compiler.
func compilePack(cfg *config.Config, configFile string, options compileOptions) (*prompt.Pack, []string, error) {
	memRepo, err := buildMemoryRepo(cfg)
	if err != nil {
		return nil, nil, err
	}

	registry := prompt.NewRegistryWithRepository(memRepo)
	if registry == nil {
		return nil, nil, fmt.Errorf("no prompt configs found in arena config")
	}

	configDir := filepath.Dir(configFile)
	warnings := collectMediaWarnings(cfg, configDir)

	compileOpts, err := buildCompileOptions(cfg)
	if err != nil {
		return nil, nil, err
	}

	compiler := prompt.NewPackCompiler(registry)
	pack, err := compiler.CompileFromRegistryWithOptions(
		options.packID, options.compilerVersion,
		parseToolsFromConfig(cfg), parsePackEvalsFromConfig(cfg),
		compileOpts...,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("compilation failed: %w", err)
	}

	return pack, warnings, nil
}

// buildCompileOptions parses workflow and agents from config into compile options.
func buildCompileOptions(cfg *config.Config) ([]prompt.CompileOption, error) {
	var opts []prompt.CompileOption

	workflowConfig, err := parseWorkflowFromConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("parsing workflow config: %w", err)
	}
	if workflowConfig != nil {
		opts = append(opts, prompt.WithWorkflow(workflowConfig))
	}

	agentsConfig, err := parseAgentsFromConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("parsing agents config: %w", err)
	}
	if agentsConfig != nil {
		opts = append(opts, prompt.WithAgents(agentsConfig))
	}

	return opts, nil
}

// validatePack runs skill and workflow validation, appending warnings and
// returning an error for any blocking issues.
func validatePack(pack *prompt.Pack, configDir string, warnings *[]string) error {
	skillErrs, skillWarnings := runSkillValidation(pack, configDir)
	if len(skillErrs) > 0 {
		return fmt.Errorf("skill validation errors: %v", skillErrs)
	}
	*warnings = append(*warnings, skillWarnings...)

	if pack.Workflow != nil {
		wfResult := pack.ValidateWorkflow()
		if wfResult.HasErrors() {
			return fmt.Errorf("workflow validation errors: %v", wfResult.Errors)
		}
		for _, w := range wfResult.Warnings {
			*warnings = append(*warnings, "workflow: "+w)
		}
	}

	return nil
}
