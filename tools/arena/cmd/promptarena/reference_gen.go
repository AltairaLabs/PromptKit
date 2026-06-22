package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/tools/arena/agentkb"
)

// referenceDocs builds the bundled reference docs from their live sources:
// the eval/assertion catalog (handler registry), the config-fields cheat-sheet
// (embedded schemas), and the CLI reference (cobra command tree). Keyed by the
// committed filename under tools/arena/agentkb/reference/.
func referenceDocs() (map[string][]byte, error) {
	evalsMD, err := agentkb.GenerateEvalsReference()
	if err != nil {
		return nil, fmt.Errorf("generate evals reference: %w", err)
	}
	configMD, err := agentkb.GenerateConfigFieldsReference()
	if err != nil {
		return nil, fmt.Errorf("generate config-fields reference: %w", err)
	}
	return map[string][]byte{
		"evals-and-assertions.md": evalsMD,
		"config-fields.md":        configMD,
		"cli.md":                  generateCLIReference(),
	}, nil
}

var genReferenceOut string

var genReferenceCmd = &cobra.Command{
	Use:    "gen-reference",
	Short:  "Regenerate the embedded agentkb reference docs",
	Hidden: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		docs, err := referenceDocs()
		if err != nil {
			return err
		}
		if err = os.MkdirAll(genReferenceOut, skillDirPerm); err != nil {
			return fmt.Errorf("create out dir: %w", err)
		}
		for name, content := range docs {
			path := filepath.Join(genReferenceOut, name)
			if err = os.WriteFile(path, content, briefFilePerm); err != nil {
				return fmt.Errorf("write %s: %w", name, err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", path)
		}
		return nil
	},
}

func init() {
	genReferenceCmd.Flags().StringVarP(&genReferenceOut, "out", "o",
		filepath.Join("tools", "arena", "agentkb", "reference"),
		"Directory to write the reference docs into")
	rootCmd.AddCommand(genReferenceCmd)
}
