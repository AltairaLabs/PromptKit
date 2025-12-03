package templates

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
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
	result := g.initializeResult(config)
	g.config = config

	if err := g.createProjectDirectory(result); err != nil {
		result.Duration = time.Since(startTime)
		return result, err
	}

	g.runPreCreateHooks(result, config.Variables)
	g.printVerboseHeader()
	g.generateFiles(result, config)
	g.runPostCreateHooks(result, config.Variables)

	result.Duration = time.Since(startTime)
	result.Success = len(result.Errors) == 0

	return result, nil
}

func (g *Generator) initializeResult(config *TemplateConfig) *GenerationResult {
	return &GenerationResult{
		Success:      false,
		ProjectPath:  filepath.Join(config.OutputDir, config.ProjectName),
		FilesCreated: []string{},
		Errors:       []error{},
		Warnings:     []string{},
	}
}

func (g *Generator) createProjectDirectory(result *GenerationResult) error {
	if err := os.MkdirAll(result.ProjectPath, 0755); err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("failed to create project directory: %w", err))
		return err
	}
	return nil
}

func (g *Generator) runPreCreateHooks(result *GenerationResult, vars map[string]interface{}) {
	if g.template.Spec.Hooks != nil {
		if err := g.runHooks(g.template.Spec.Hooks.PreCreate, vars); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("pre-create hook warning: %v", err))
		}
	}
}

func (g *Generator) runPostCreateHooks(result *GenerationResult, vars map[string]interface{}) {
	if g.template.Spec.Hooks != nil {
		if err := g.runHooks(g.template.Spec.Hooks.PostCreate, vars); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("post-create hook warning: %v", err))
		}
	}
}

func (g *Generator) printVerboseHeader() {
	if !g.config.Verbose {
		return
	}
	fmt.Printf("\nGenerating %d files from template...\n", len(g.template.Spec.Files))
	switch {
	case g.template.BaseDir != "":
		fmt.Printf("Template base directory: %s\n", g.template.BaseDir)
	case g.template.BaseURL != "":
		fmt.Printf("Template base URL: %s\n", g.template.BaseURL)
	default:
		fmt.Printf("Template type: built-in\n")
	}
	fmt.Println()
}

func (g *Generator) generateFiles(result *GenerationResult, config *TemplateConfig) {
	for i := range g.template.Spec.Files {
		g.generateFileWithProgress(i, &g.template.Spec.Files[i], config.Variables, result)
	}
}

func (g *Generator) generateFileWithProgress(
	index int,
	fileSpec *FileSpec,
	vars map[string]interface{},
	result *GenerationResult,
) {
	if g.config.Verbose {
		fmt.Printf("[%d/%d] Processing: %s\n", index+1, len(g.template.Spec.Files), fileSpec.Path)
	}

	err := g.generateFile(*fileSpec, vars, result)
	if err != nil {
		if g.config.Verbose {
			fmt.Printf("  ❌ Error: %v\n", err)
		}
		result.Errors = append(result.Errors, err)
	} else if g.config.Verbose {
		fmt.Printf("  ✓ Created\n")
	}
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
			if g.config.Verbose {
				fmt.Printf("  ⊘ Skipped (condition evaluated to false)\n")
			}
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
	fullPath, outputPath, err := g.buildOutputPath(&fileSpec, vars, result.ProjectPath)
	if err != nil {
		return err
	}

	// Create parent directories
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", outputPath, err)
	}

	content, err := g.resolveContent(&fileSpec, vars, outputPath)
	if err != nil {
		return err
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
	// Normalize BaseDir for comparison (remove trailing slash if present)
	baseDir := strings.TrimSuffix(g.template.BaseDir, string(filepath.Separator))
	if !strings.HasPrefix(resolved, baseDir+string(filepath.Separator)) && resolved != baseDir {
		return "", fmt.Errorf("source path escapes template directory: %s", src)
	}
	return resolved, nil
}

func (g *Generator) buildOutputPath(
	fileSpec *FileSpec,
	vars map[string]interface{},
	projectPath string,
) (fullPath, outputPath string, err error) {
	outputPath, err = g.renderTemplate("path", fileSpec.Path, vars)
	if err != nil {
		return "", "", fmt.Errorf("failed to render path template: %w", err)
	}

	cleanOutput := filepath.Clean(outputPath)
	if filepath.IsAbs(cleanOutput) || strings.HasPrefix(cleanOutput, "..") {
		return "", "", fmt.Errorf("invalid output path: %s", outputPath)
	}

	fullPath = filepath.Join(projectPath, cleanOutput)
	if !strings.HasPrefix(fullPath, projectPath) {
		return "", "", fmt.Errorf("output path escapes project directory: %s", outputPath)
	}

	return fullPath, outputPath, nil
}

func (g *Generator) resolveContent(fileSpec *FileSpec, vars map[string]interface{}, outputPath string) (string, error) {
	switch {
	case fileSpec.Source != "":
		return g.resolveSourceContent(fileSpec, vars, outputPath)
	case fileSpec.Content != "":
		return g.resolveInlineContent(fileSpec, vars, outputPath)
	case fileSpec.Template != "":
		return g.resolveTemplateContent(fileSpec, vars)
	default:
		return "", nil
	}
}

func (g *Generator) resolveSourceContent(
	fileSpec *FileSpec, vars map[string]interface{}, outputPath string,
) (string, error) {
	if g.config.Verbose {
		fmt.Printf("  Reading source file: %s\n", fileSpec.Source)
	}

	data, err := g.readSourceData(fileSpec.Source)
	if err != nil {
		return "", err
	}

	content, err := g.renderTemplate("source", string(data), vars)
	if err != nil {
		return "", fmt.Errorf("failed to render source content for %s: %w", outputPath, err)
	}
	return content, nil
}

func (g *Generator) readSourceData(source string) ([]byte, error) {
	if g.template.BaseURL != "" {
		return g.fetchRemoteSource(source)
	}
	return g.readLocalSource(source)
}

func (g *Generator) fetchRemoteSource(source string) ([]byte, error) {
	sourceURL := g.template.BaseURL + source
	if g.config.Verbose {
		fmt.Printf("  Fetching from URL: %s\n", sourceURL)
	}
	data, err := fetchURL(sourceURL)
	if err != nil {
		if g.config.Verbose {
			fmt.Printf("  Failed to fetch URL: %v\n", err)
		}
		return nil, fmt.Errorf("failed to fetch source file %s: %w", sourceURL, err)
	}
	return data, nil
}

func (g *Generator) readLocalSource(source string) ([]byte, error) {
	srcPath, err := g.resolveSourcePath(source)
	if err != nil {
		if g.config.Verbose {
			fmt.Printf("  Failed to resolve source path: %v\n", err)
		}
		return nil, err
	}
	if g.config.Verbose {
		fmt.Printf("  Resolved to: %s\n", srcPath)
	}
	data, err := os.ReadFile(srcPath) //nolint:gosec // path already validated to stay under template base dir
	if err != nil {
		if g.config.Verbose {
			fmt.Printf("  Failed to read file: %v\n", err)
		}
		return nil, fmt.Errorf("failed to read source file %s: %w", srcPath, err)
	}
	return data, nil
}

func (g *Generator) resolveInlineContent(
	fileSpec *FileSpec, vars map[string]interface{}, outputPath string,
) (string, error) {
	if g.config.Verbose {
		fmt.Printf("  Using inline content (%d bytes)\n", len(fileSpec.Content))
	}
	content, err := g.renderTemplate("content", fileSpec.Content, vars)
	if err != nil {
		return "", fmt.Errorf("failed to render inline content for %s: %w", outputPath, err)
	}
	return content, nil
}

func (g *Generator) resolveTemplateContent(fileSpec *FileSpec, vars map[string]interface{}) (string, error) {
	if g.config.Verbose {
		fmt.Printf("  Using template file: %s\n", fileSpec.Template)
	}
	templateData, err := g.loader.ReadTemplateFile(g.template.Metadata.Name, fileSpec.Template)
	if err != nil {
		return "", fmt.Errorf("failed to read template file %s: %w", fileSpec.Template, err)
	}

	content, err := g.renderTemplate(fileSpec.Template, string(templateData), vars)
	if err != nil {
		return "", fmt.Errorf("failed to render template %s: %w", fileSpec.Template, err)
	}
	return content, nil
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

// fetchURL fetches content from a URL
func fetchURL(url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	return data, nil
}
