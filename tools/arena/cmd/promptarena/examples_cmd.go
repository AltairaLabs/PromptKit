package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/tools/arena/agentkb"
	"github.com/AltairaLabs/PromptKit/tools/arena/templates"
)

// exampleLoader is the subset of templates.Loader that examples show needs.
type exampleLoader interface {
	Load(name string) (*templates.Template, error)
	ReadTemplateFile(templateName, filePath string) ([]byte, error)
}

const examplesListUse = "list"

var (
	examplesTagFlag  string
	examplesJSONFlag bool
)

var examplesCmd = &cobra.Command{
	Use:   "examples",
	Short: "Discover example PromptArena kits",
}

var examplesListCmd = &cobra.Command{
	Use:   examplesListUse,
	Short: "List example kits from the embedded catalog",
	Long: `List example kits the agent can copy from. Reads the embedded catalog,
so it works offline.

Examples:
  promptarena examples list
  promptarena examples list --tag mcp
  promptarena examples list --json`,
	RunE: runExamplesList,
}

var examplesShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Print an example kit's files",
	Args:  cobra.ExactArgs(1),
	RunE:  runExamplesShow,
}

func init() {
	rootCmd.AddCommand(examplesCmd)
	examplesCmd.AddCommand(examplesListCmd, examplesShowCmd)
	examplesListCmd.Flags().StringVar(&examplesTagFlag, "tag", "", "Filter by tag")
	examplesListCmd.Flags().BoolVar(&examplesJSONFlag, "json", false, "Output JSON")
}

func runExamplesList(cmd *cobra.Command, _ []string) error {
	return writeExamplesList(cmd.OutOrStdout(), examplesTagFlag, examplesJSONFlag)
}

func runExamplesShow(cmd *cobra.Command, args []string) error {
	return writeExampleShow(cmd.OutOrStdout(), templates.NewLoader(""), args[0])
}

func writeExamplesList(w io.Writer, tag string, asJSON bool) error {
	cat, err := agentkb.LoadCatalog()
	if err != nil {
		return err
	}

	entries := cat.Entries
	if tag != "" {
		filtered := entries[:0:0]
		for _, e := range entries {
			if containsString(e.Tags, tag) {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(entries)
	}

	tw := tabwriter.NewWriter(w, tabMinWidth, tabWidth, tabPadding, ' ', 0)
	if _, err := fmt.Fprintln(tw, "NAME\tTAGS\tDESCRIPTION"); err != nil {
		return err
	}
	for i := range entries {
		e := &entries[i]
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\n", e.Name, strings.Join(e.Tags, ","), e.Description); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func writeExampleShow(w io.Writer, loader exampleLoader, name string) error {
	cat, err := agentkb.LoadCatalog()
	if err != nil {
		return err
	}

	var entry *agentkb.CatalogEntry
	for i := range cat.Entries {
		if cat.Entries[i].Name == name {
			entry = &cat.Entries[i]
			break
		}
	}
	if entry == nil {
		return fmt.Errorf("unknown example %q (try 'promptarena examples list')", name)
	}

	loadName := entry.Source
	if idx := strings.IndexByte(loadName, ':'); idx >= 0 {
		loadName = loadName[idx+1:]
	}

	tmpl, err := loader.Load(loadName)
	if err != nil {
		return fmt.Errorf("load example %q: %w", name, err)
	}

	if _, err := fmt.Fprintf(w, "# %s\n\n%s\n\nFiles:\n", entry.Name, entry.Description); err != nil {
		return err
	}
	for i := range tmpl.Spec.Files {
		f := &tmpl.Spec.Files[i]
		if _, err := fmt.Fprintf(w, "\n--- %s ---\n", f.Path); err != nil {
			return err
		}
		body := fileBody(loader, tmpl.Metadata.Name, f)
		if _, err := fmt.Fprintln(w, body); err != nil {
			return err
		}
	}
	return nil
}

// fileBody returns the displayable body for a template file: inline content, the
// embedded template source, or a placeholder when the body lives elsewhere
// (external/remote source).
func fileBody(loader exampleLoader, templateName string, f *templates.FileSpec) string {
	switch {
	case f.Content != "":
		return f.Content
	case f.Template != "":
		data, err := loader.ReadTemplateFile(templateName, f.Template)
		if err != nil {
			return fmt.Sprintf("(template body unavailable: %v)", err)
		}
		return string(data)
	default:
		return "(external source — fetch the example to see this file)"
	}
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
