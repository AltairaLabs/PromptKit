package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

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

	templatesCmd.PersistentFlags().StringVar(&templateIndex, "index", templates.DefaultGitHubIndex, "Path or URL to template index")
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
}
