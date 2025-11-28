package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/AltairaLabs/PromptKit/tools/arena/templates"
)

var (
	templateIndex   string
	templateName    string
	templateVersion string
	templateCache   string
	templateValues  map[string]string
	templateFile    string
	dryRun          bool
	outputDir       string
	valuesFile      string
	promptMissing   bool
)

var templatesCmd = &cobra.Command{
	Use:   "templates",
	Short: "Manage PromptArena templates (list, fetch, render)",
}

var templatesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List templates from an index",
	RunE: func(cmd *cobra.Command, args []string) error {
		idx, err := templates.LoadIndex(templateIndex)
		if err != nil {
			return err
		}
		for _, e := range idx.Entries {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", e.Name, e.Version, e.Description); err != nil {
				return err
			}
		}
		return nil
	},
}

var templatesFetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "Fetch a template from an index into cache",
	RunE: func(cmd *cobra.Command, args []string) error {
		idx, err := templates.LoadIndex(templateIndex)
		if err != nil {
			return err
		}
		entry, err := idx.FindEntry(templateName, templateVersion)
		if err != nil {
			return err
		}
		path, err := templates.FetchTemplate(entry, templateCache)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Cached %s@%s at %s\n", entry.Name, entry.Version, path); err != nil {
			return err
		}
		return nil
	},
}

var templatesRenderCmd = &cobra.Command{
	Use:   "render",
	Short: "Render a cached template to an output directory (dry-run only)",
	RunE: func(cmd *cobra.Command, args []string) error {
		src := templateFile
		if src == "" {
			if templateName == "" {
				return fmt.Errorf("either --template or --file is required")
			}
			if templateVersion == "" {
				return fmt.Errorf("--version is required when rendering from cache")
			}
			src = filepath.Join(templateCache, templateName, templateVersion, "template.yaml")
		}
		pkg, err := templates.LoadTemplatePackage(src)
		if err != nil {
			return err
		}
		fileVars, err := loadValuesFile(valuesFile)
		if err != nil {
			return err
		}
		templateValues = mergeValues(fileVars, templateValues)
		if promptMissing {
			templateValues, err = promptForMissing(templateValues, pkg)
			if err != nil {
				return err
			}
		}
		out := outputDir
		if out == "" {
			tmp, err := os.MkdirTemp("", "promptarena-template-*")
			if err != nil {
				return fmt.Errorf("create temp dir: %w", err)
			}
			out = tmp
		}
		if err := templates.RenderDryRun(pkg, templateValues, out); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Rendered to %s\n", out); err != nil {
			return err
		}
		return nil
	},
}

//nolint:gochecknoinits // cobra command registration
func init() {
	rootCmd.AddCommand(templatesCmd)

	templatesCmd.PersistentFlags().StringVar(&templateIndex, "index", templates.DefaultGitHubIndex,
		"Path or URL to template index")
	templatesCmd.PersistentFlags().StringVar(&templateCache, "cache-dir",
		filepath.Join(os.TempDir(), "promptarena-templates"), "Template cache directory")

	templatesCmd.AddCommand(templatesListCmd)
	templatesCmd.AddCommand(templatesFetchCmd)
	templatesCmd.AddCommand(templatesRenderCmd)

	templatesFetchCmd.Flags().StringVar(&templateName, "template", "", "Template name")
	templatesFetchCmd.Flags().StringVar(&templateVersion, "version", "", "Template version")
	if err := templatesFetchCmd.MarkFlagRequired("template"); err != nil {
		panic(err)
	}

	templatesRenderCmd.Flags().StringVar(&templateName, "template", "", "Template name (cached)")
	templatesRenderCmd.Flags().StringVar(&templateVersion, "version", "", "Template version (default latest)")
	templatesRenderCmd.Flags().StringVar(&templateFile, "file", "", "Template file path (bypass cache)")
	templatesRenderCmd.Flags().BoolVar(&dryRun, "dry-run", true, "Render to a temp/output dir without touching project")
	templatesRenderCmd.Flags().StringToStringVar(&templateValues, "set", map[string]string{},
		"Template variables (key=value)")
	templatesRenderCmd.Flags().StringVar(&outputDir, "out", "", "Output directory (defaults to temp)")
	templatesRenderCmd.Flags().StringVar(&valuesFile, "values", "", "YAML file with template variables")
	templatesRenderCmd.Flags().BoolVar(&promptMissing, "prompt-missing", false, "Prompt for missing template variables")
}

func loadValuesFile(path string) (map[string]string, error) {
	if path == "" {
		return map[string]string{}, nil
	}
	data, err := os.ReadFile(path) //nolint:gosec // values file path is user provided
	if err != nil {
		return nil, fmt.Errorf("read values file: %w", err)
	}
	var parsed map[string]interface{}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("parse values file: %w", err)
	}
	out := make(map[string]string, len(parsed))
	for k, v := range parsed {
		out[k] = fmt.Sprintf("%v", v)
	}
	return out, nil
}

func mergeValues(base, override map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(override))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range override {
		out[k] = v
	}
	return out
}

var placeholderRegex = regexp.MustCompile(`{{\s*\.([a-zA-Z0-9_]+)\s*}}`)

func extractPlaceholders(pkg *templates.TemplatePackage) []string {
	seen := map[string]struct{}{}
	for _, f := range pkg.Files {
		matches := placeholderRegex.FindAllStringSubmatch(f.Content, -1)
		for _, m := range matches {
			if len(m) > 1 {
				seen[m[1]] = struct{}{}
			}
		}
	}
	var keys []string
	for k := range seen {
		keys = append(keys, k)
	}
	return keys
}

func promptForMissing(vars map[string]string, pkg *templates.TemplatePackage) (map[string]string, error) {
	if pkg == nil {
		return vars, nil
	}
	keys := extractPlaceholders(pkg)
	out := make(map[string]string, len(vars)+len(keys))
	for k, v := range vars {
		out[k] = v
	}
	for _, k := range keys {
		if _, ok := out[k]; ok {
			continue
		}
		p := promptui.Prompt{
			Label:     fmt.Sprintf("Value for %s", k),
			AllowEdit: true,
		}
		val, err := p.Run()
		if err != nil {
			if err == promptui.ErrInterrupt {
				return nil, fmt.Errorf("prompt canceled")
			}
			return nil, fmt.Errorf("prompt for %s: %w", k, err)
		}
		out[k] = strings.TrimSpace(val)
	}
	return out, nil
}
