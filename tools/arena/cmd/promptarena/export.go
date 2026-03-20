package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/tools/packc/compiler"
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export arena config as a compiled PromptPack",
	Long: `Compiles an Arena configuration file into a PromptPack JSON format.

The PromptPack is a self-contained, portable representation of the prompt
configuration that can be used by the PromptKit runtime and SDK.

Examples:
  # Export to stdout
  promptarena export

  # Export with a specific config file
  promptarena export -c my-config.arena.yaml

  # Export to a file with a custom pack ID
  promptarena export -o pack.json --id my-pack`,
	RunE: runExport,
}

var (
	exportConfig string
	exportOutput string
	exportID     string
)

func init() {
	rootCmd.AddCommand(exportCmd)
	exportCmd.Flags().StringVarP(&exportConfig, "config", "c", "config.arena.yaml", "Path to arena config file")
	exportCmd.Flags().StringVarP(&exportOutput, "output", "o", "", "Output file path (default: stdout)")
	exportCmd.Flags().StringVar(&exportID, "id", "", "Pack identifier (default: derived from folder name)")
}

func runExport(_ *cobra.Command, _ []string) error {
	var opts []compiler.Option
	if exportID != "" {
		opts = append(opts, compiler.WithPackID(exportID))
	}

	result, err := compiler.Compile(exportConfig, opts...)
	if err != nil {
		return fmt.Errorf("export failed: %w", err)
	}

	for _, w := range result.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}

	if exportOutput != "" {
		const outputFilePerm = 0o600
		if writeErr := os.WriteFile(exportOutput, result.JSON, outputFilePerm); writeErr != nil {
			return fmt.Errorf("writing output file: %w", writeErr)
		}
		fmt.Fprintf(os.Stderr, "Exported pack to %s\n", exportOutput)
		return nil
	}

	_, err = os.Stdout.Write(result.JSON)
	return err
}
