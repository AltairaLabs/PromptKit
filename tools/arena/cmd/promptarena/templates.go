package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"

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
	repoConfigPath  string
	repoName        string
	repoURL         string
)

const (
	tabMinWidth = 0
	tabWidth    = 4
	tabPadding  = 2
)

var templatesCmd = &cobra.Command{
	Use:   "templates",
	Short: "Manage PromptArena templates (list, fetch, render)",
}

var templatesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List templates from an index",
	RunE: func(cmd *cobra.Command, args []string) error {
		indexPath, repoNameResolved, err := resolveIndexPath()
		if err != nil {
			return err
		}
		idx, err := templates.LoadIndex(indexPath)
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), tabMinWidth, tabWidth, tabPadding, ' ', 0)
		if _, err := fmt.Fprintln(w, "REPO\tTEMPLATE\tVERSION\tDESCRIPTION"); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, "----\t--------\t-------\t-----------"); err != nil {
			return err
		}
		repoPrefix := "-"
		if repoNameResolved != "" {
			repoPrefix = repoNameResolved
		}
		for _, e := range idx.Spec.Entries {
			name := e.Name
			if repoNameResolved != "" {
				name = fmt.Sprintf("%s/%s", repoNameResolved, e.Name)
			}
			if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", repoPrefix, name, e.Version, e.Description); err != nil {
				return err
			}
		}
		return w.Flush()
	},
}

var templatesFetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "Fetch a template from an index into cache",
	RunE: func(cmd *cobra.Command, args []string) error {
		if repo, name := splitTemplateRef(templateName); repo != "" {
			templateIndex = repo
			templateName = name
		}
		indexPath, _, err := resolveIndexPath()
		if err != nil {
			return err
		}
		idx, err := templates.LoadIndex(indexPath)
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

var templatesUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update all templates from an index into cache",
	RunE: func(cmd *cobra.Command, args []string) error {
		indexPath, _, err := resolveIndexPath()
		if err != nil {
			return err
		}
		idx, err := templates.LoadIndex(indexPath)
		if err != nil {
			return err
		}
		seen := 0
		for i := range idx.Spec.Entries {
			entry := &idx.Spec.Entries[i]
			if _, err := templates.FetchTemplate(entry, templateCache); err != nil {
				return fmt.Errorf("fetch %s@%s: %w", entry.Name, entry.Version, err)
			}
			seen++
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Updated %d templates\n", seen); err != nil {
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
			if repo, name := splitTemplateRef(templateName); repo != "" {
				templateIndex = repo
				templateName = name
			}
			if templateVersion == "" {
				return fmt.Errorf("--version is required when rendering from cache")
			}
			src = filepath.Join(templateCache, templateName, templateVersion, "template.yaml")
		}
		pkg, err := templates.LoadTemplatePackage(src)
		if err != nil {
			// When rendering from cache and the template is missing, provide a helpful hint.
			if templateFile == "" && errors.Is(err, os.ErrNotExist) {
				msg := "template %s@%s not found in cache (%s). " +
					"Run `promptarena templates fetch --template %s --version %s` first, " +
					"or use --file to render a local template"
				return fmt.Errorf(msg, templateName, templateVersion, src, templateName, templateVersion)
			}
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

var templatesRepoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Manage template repositories",
}

var templatesRepoListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured template repositories",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := templates.LoadRepoConfig(repoConfigPath)
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), tabMinWidth, tabWidth, tabPadding, ' ', 0)
		if _, err := fmt.Fprintln(w, "REPO\tURL"); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, "----\t---"); err != nil {
			return err
		}
		for name, url := range cfg.Repos {
			if _, err := fmt.Fprintf(w, "%s\t%s\n", name, url); err != nil {
				return err
			}
		}
		return w.Flush()
	},
}

var templatesRepoAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add or update a template repository",
	RunE: func(cmd *cobra.Command, args []string) error {
		if repoName == "" || repoURL == "" {
			return fmt.Errorf("--name and --url are required")
		}
		cfg, err := templates.LoadRepoConfig(repoConfigPath)
		if err != nil {
			return err
		}
		cfg.Add(repoName, repoURL)
		if err := cfg.Save(repoConfigPath); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Added repo %s -> %s\n", repoName, repoURL); err != nil {
			return err
		}
		return nil
	},
}

var templatesRepoRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove a template repository",
	RunE: func(cmd *cobra.Command, args []string) error {
		if repoName == "" {
			return fmt.Errorf("--name is required")
		}
		cfg, err := templates.LoadRepoConfig(repoConfigPath)
		if err != nil {
			return err
		}
		if _, ok := cfg.Repos[repoName]; !ok {
			return fmt.Errorf("repo %s not found", repoName)
		}
		cfg.Remove(repoName)
		if err := cfg.Save(repoConfigPath); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Removed repo %s\n", repoName); err != nil {
			return err
		}
		return nil
	},
}

//nolint:gochecknoinits // cobra command registration
func init() {
	rootCmd.AddCommand(templatesCmd)

	templatesCmd.PersistentFlags().StringVar(&templateIndex, "index", templates.DefaultRepoName,
		"Repo name or path/URL to template index")
	templatesCmd.PersistentFlags().StringVar(&repoConfigPath, "repo-config", templates.DefaultRepoConfigPath(),
		"Template repo config file")
	templatesCmd.PersistentFlags().StringVar(&templateCache, "cache-dir", templates.DefaultCacheDir(),
		"Template cache directory")

	templatesCmd.AddCommand(templatesListCmd)
	templatesCmd.AddCommand(templatesFetchCmd)
	templatesCmd.AddCommand(templatesUpdateCmd)
	templatesCmd.AddCommand(templatesRenderCmd)
	templatesCmd.AddCommand(templatesRepoCmd)

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

	templatesRepoCmd.AddCommand(templatesRepoListCmd)
	templatesRepoCmd.AddCommand(templatesRepoAddCmd)
	templatesRepoCmd.AddCommand(templatesRepoRemoveCmd)
	templatesRepoAddCmd.Flags().StringVar(&repoName, "name", "", "Short name for the repo")
	templatesRepoAddCmd.Flags().StringVar(&repoURL, "url", "", "Index URL for the repo")
	templatesRepoRemoveCmd.Flags().StringVar(&repoName, "name", "", "Short name for the repo")
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

func resolveIndexPath() (indexPath, repoName string, err error) {
	cfg, err := templates.LoadRepoConfig(repoConfigPath)
	if err != nil {
		return "", "", err
	}
	path := templates.ResolveIndex(templateIndex, cfg)
	name := ""
	if cfg != nil {
		if _, ok := cfg.Repos[templateIndex]; ok {
			name = templateIndex
		}
	}
	return path, name, nil
}

// splitTemplateRef parses repo/template into repo + name. If no repo is present, repo is empty.
func splitTemplateRef(ref string) (repo, name string) {
	const maxParts = 2
	parts := strings.SplitN(ref, "/", maxParts)
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		return parts[0], parts[1]
	}
	return "", ref
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
	// Delegate to interactive implementation to simplify coverage management.
	return promptForMissingInteractive(vars, pkg)
}
