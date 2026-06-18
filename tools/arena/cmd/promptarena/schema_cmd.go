package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/tools/arena/agentkb"
)

var schemaListFlag bool

var schemaCmd = &cobra.Command{
	Use:   "schema [type]",
	Short: "Print the embedded JSON schema for a config type",
	Long: `Print the authoritative JSON schema for a PromptArena config type
(scenario, provider, promptconfig, tool, arena, ...). The schema is embedded in
this binary and is the exact version 'promptarena validate' enforces — prefer it
over the public web copy, which may be a different release.

Examples:
  promptarena schema --list
  promptarena schema scenario`,
	RunE: runSchema,
}

func init() {
	rootCmd.AddCommand(schemaCmd)
	schemaCmd.Flags().BoolVar(&schemaListFlag, "list", false, "List available schema types")
}

func runSchema(cmd *cobra.Command, args []string) error {
	return writeSchema(cmd.OutOrStdout(), schemaListFlag, args)
}

func writeSchema(w io.Writer, list bool, args []string) error {
	if list {
		names, err := agentkb.SchemaNames()
		if err != nil {
			return err
		}
		for _, n := range names {
			if _, err := fmt.Fprintln(w, n); err != nil {
				return err
			}
		}
		return nil
	}

	if len(args) == 0 {
		return fmt.Errorf("schema type required (try --list)")
	}

	b, err := agentkb.Schema(args[0])
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}
