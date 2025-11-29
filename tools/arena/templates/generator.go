package templates

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

// Generator handles template generation and file creation
type Generator struct {
	template *Template
	loader   *Loader
	config   *TemplateConfig
}

// NewGenerator creates a new template generator
func NewGenerator(tmpl *Template, loader *Loader) *Generator {
	return &Generator{
		template: tmpl,
		loader:   loader,
	}
}

// Generate generates a project from the template
func (g *Generator) Generate(config *TemplateConfig) (*GenerationResult, error) {
	startTime := time.Now()
	result := &GenerationResult{
		Success:      false,
		ProjectPath:  filepath.Join(config.OutputDir, config.ProjectName),
		FilesCreated: []string{},
		Errors:       []error{},
		Warnings:     []string{},
	}

	g.config = config

	// Create project directory
	if err := os.MkdirAll(result.ProjectPath, 0755); err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("failed to create project directory: %w", err))
		result.Duration = time.Since(startTime)
		return result, err
	}

	// Run pre-create hooks
	if g.template.Spec.Hooks != nil {
		if err := g.runHooks(g.template.Spec.Hooks.PreCreate, config.Variables); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("pre-create hook warning: %v", err))
		}
	}

	// Generate files
	for _, fileSpec := range g.template.Spec.Files {
		if err := g.generateFile(fileSpec, config.Variables, result); err != nil {
			result.Errors = append(result.Errors, err)
			// Continue generating other files
		}
	}

	// Run post-create hooks
	if g.template.Spec.Hooks != nil {
		if err := g.runHooks(g.template.Spec.Hooks.PostCreate, config.Variables); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("post-create hook warning: %v", err))
		}
	}

	result.Duration = time.Since(startTime)
	result.Success = len(result.Errors) == 0

	return result, nil
}

// generateFile generates a single file from a file specification
func (g *Generator) generateFile(fileSpec FileSpec, vars map[string]interface{}, result *GenerationResult) error {
	// Evaluate condition if present
	if fileSpec.Condition != "" {
		shouldCreate, err := g.evaluateCondition(fileSpec.Condition, vars)
		if err != nil {
			return fmt.Errorf("failed to evaluate condition for %s: %w", fileSpec.Path, err)
		}
		if !shouldCreate {
			return nil // Skip this file
		}
	}

	// Handle foreach iteration
	if fileSpec.ForEach != "" {
		return g.generateFileForEach(fileSpec, vars, result)
	}

	// Generate single file
	return g.generateSingleFile(fileSpec, vars, result)
}

// generateSingleFile generates a single file
func (g *Generator) generateSingleFile(fileSpec FileSpec, vars map[string]interface{}, result *GenerationResult) error {
	// Render path template
	outputPath, err := g.renderTemplate("path", fileSpec.Path, vars)
	if err != nil {
		return fmt.Errorf("failed to render path template: %w", err)
	}

	cleanOutput := filepath.Clean(outputPath)
	if filepath.IsAbs(cleanOutput) || strings.HasPrefix(cleanOutput, "..") {
		return fmt.Errorf("invalid output path: %s", outputPath)
	}

	fullPath := filepath.Join(result.ProjectPath, cleanOutput)
	if !strings.HasPrefix(fullPath, result.ProjectPath) {
		return fmt.Errorf("output path escapes project directory: %s", outputPath)
	}

	// Create parent directories
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", outputPath, err)
	}

	// Generate content
	var content string
	var data []byte
	if fileSpec.Source != "" {
		srcPath, readErr := g.resolveSourcePath(fileSpec.Source)
		if readErr != nil {
			return readErr
		}
		data, err = os.ReadFile(srcPath) //nolint:gosec // path already validated to stay under template base dir
		if err != nil {
			return fmt.Errorf("failed to read source file %s: %w", srcPath, err)
		}
		content, err = g.renderTemplate("source", string(data), vars)
		if err != nil {
			return fmt.Errorf("failed to render source content for %s: %w", outputPath, err)
		}
	} else if fileSpec.Content != "" {
		// Use inline content
		content, err = g.renderTemplate("content", fileSpec.Content, vars)
		if err != nil {
			return fmt.Errorf("failed to render inline content for %s: %w", outputPath, err)
		}
	} else if fileSpec.Template != "" {
		// Load and render template file
		templateData, err := g.loader.ReadTemplateFile(g.template.Metadata.Name, fileSpec.Template)
		if err != nil {
			return fmt.Errorf("failed to read template file %s: %w", fileSpec.Template, err)
		}

		content, err = g.renderTemplate(fileSpec.Template, string(templateData), vars)
		if err != nil {
			return fmt.Errorf("failed to render template %s: %w", fileSpec.Template, err)
		}
	}

	// Write file
	perm := os.FileMode(0644)
	if fileSpec.Executable {
		perm = 0755
	}

	if err := os.WriteFile(fullPath, []byte(content), perm); err != nil {
		return fmt.Errorf("failed to write file %s: %w", outputPath, err)
	}

	result.FilesCreated = append(result.FilesCreated, outputPath)
	return nil
}

func (g *Generator) resolveSourcePath(src string) (string, error) {
	if filepath.IsAbs(src) {
		return "", fmt.Errorf("absolute source paths are not allowed: %s", src)
	}
	clean := filepath.Clean(src)
	if g.template.BaseDir == "" {
		return "", fmt.Errorf("source requires template base directory: %s", src)
	}
	resolved := filepath.Join(g.template.BaseDir, clean)
	if !strings.HasPrefix(resolved, g.template.BaseDir+string(filepath.Separator)) && resolved != g.template.BaseDir {
		return "", fmt.Errorf("source path escapes template directory: %s", src)
	}
	return resolved, nil
}

// generateFileForEach generates multiple files by iterating over a variable
func (g *Generator) generateFileForEach(fileSpec FileSpec, vars map[string]interface{}, result *GenerationResult) error {
	// Get the array to iterate over
	arrayVar, ok := vars[fileSpec.ForEach]
	if !ok {
		return fmt.Errorf("foreach variable %s not found", fileSpec.ForEach)
	}

	array, ok := arrayVar.([]interface{})
	if !ok {
		// Try []string
		if strArray, ok := arrayVar.([]string); ok {
			array = make([]interface{}, len(strArray))
			for i, s := range strArray {
				array[i] = s
			}
		} else {
			return fmt.Errorf("foreach variable %s is not an array", fileSpec.ForEach)
		}
	}

	// Generate a file for each item
	for _, item := range array {
		// Create a new variable context with the item
		itemVars := make(map[string]interface{})
		for k, v := range vars {
			itemVars[k] = v
		}

		// Add the current item as both the singular name and "item"
		singularName := strings.TrimSuffix(fileSpec.ForEach, "s")
		itemVars[singularName] = item
		itemVars["item"] = item

		// Merge file-specific variables
		for k, v := range fileSpec.Variables {
			itemVars[k] = v
		}

		if err := g.generateSingleFile(fileSpec, itemVars, result); err != nil {
			return err
		}
	}

	return nil
}

// renderTemplate renders a template string with the given variables
func (g *Generator) renderTemplate(name, templateStr string, vars map[string]interface{}) (string, error) {
	tmpl, err := template.New(name).Parse(templateStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// evaluateCondition evaluates a condition template
func (g *Generator) evaluateCondition(condition string, vars map[string]interface{}) (bool, error) {
	result, err := g.renderTemplate("condition", condition, vars)
	if err != nil {
		return false, err
	}

	result = strings.TrimSpace(result)
	return result == "true" || result == "1" || result == "yes", nil
}

// runHooks executes a set of hooks
func (g *Generator) runHooks(hooks []Hook, vars map[string]interface{}) error {
	for _, hook := range hooks {
		// Evaluate condition if present
		if hook.Condition != "" {
			shouldRun, err := g.evaluateCondition(hook.Condition, vars)
			if err != nil {
				return fmt.Errorf("failed to evaluate hook condition: %w", err)
			}
			if !shouldRun {
				continue
			}
		}

		// Render command
		command, err := g.renderTemplate("hook", hook.Command, vars)
		if err != nil {
			return fmt.Errorf("failed to render hook command: %w", err)
		}

		// Log hook execution (actual command execution is deferred to post-generation)
		if hook.Message != "" {
			fmt.Printf("Hook: %s\n", hook.Message)
		}
		fmt.Printf("Would execute: %s\n", command)

		// Note: Command execution is intentionally deferred to allow users to review
		// generated files before running potentially destructive commands
	}

	return nil
}
