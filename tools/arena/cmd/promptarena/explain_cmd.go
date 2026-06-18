package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/tools/arena/agentkb"
)

var explainListFlag bool

var explainCmd = &cobra.Command{
	Use:   "explain [concept-id]",
	Short: "Explain a PromptArena authoring concept",
	Long: `Print an authoring idiom from the embedded knowledge base — mock providers,
assertions-vs-evals, schema validation, and more.

Examples:
  promptarena explain --list
  promptarena explain mock-providers`,
	RunE: runExplain,
}

func init() {
	rootCmd.AddCommand(explainCmd)
	explainCmd.Flags().BoolVar(&explainListFlag, "list", false, "List available concepts")
}

func runExplain(cmd *cobra.Command, args []string) error {
	return writeExplain(cmd.OutOrStdout(), explainListFlag, args)
}

func writeExplain(w io.Writer, list bool, args []string) error {
	if list {
		concepts, err := agentkb.Concepts()
		if err != nil {
			return err
		}
		for _, c := range concepts {
			if _, err := fmt.Fprintf(w, "%-24s %s\n", c.ID, c.Summary); err != nil {
				return err
			}
		}
		return nil
	}

	if len(args) == 0 {
		return fmt.Errorf("concept id required (try --list)")
	}

	c, err := agentkb.ConceptByID(args[0])
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "# %s\n\n%s\n", c.Title, c.Body)
	return err
}
