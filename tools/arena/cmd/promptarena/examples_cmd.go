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

func writeExampleShow(w io.Writer, loader agentkb.ExampleLoader, name string) error {
	text, err := agentkb.RenderExample(loader, name)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, text)
	return err
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
