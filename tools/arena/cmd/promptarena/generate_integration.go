package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/AltairaLabs/PromptKit/tools/arena/generate"
)

const (
	dirPerms  = 0o700
	filePerms = 0o600
)

func runGenerate(cmd *cobra.Command, _ []string) error {
	adapter, err := resolveAdapter(cmd)
	if err != nil {
		return err
	}

	listOpts, err := buildListOptions(cmd)
	if err != nil {
		return err
	}

	sessions, err := fetchSessions(cmd, adapter, listOpts)
	if err != nil {
		return err
	}
	if sessions == nil {
		return nil
	}

	return writeScenarios(cmd, sessions)
}

func fetchSessions(
	cmd *cobra.Command,
	adapter generate.SessionSourceAdapter,
	listOpts generate.ListOptions,
) ([]*generate.SessionDetail, error) {
	ctx := context.Background()

	summaries, err := adapter.List(ctx, listOpts)
	if err != nil {
		return nil, fmt.Errorf("listing sessions: %w", err)
	}

	if len(summaries) == 0 {
		fmt.Println("No sessions found matching the given filters.")
		return nil, nil
	}

	var sessions []*generate.SessionDetail
	for i := range summaries {
		detail, getErr := adapter.Get(ctx, summaries[i].ID)
		if getErr != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping session %s: %v\n", summaries[i].ID, getErr)
			continue
		}
		sessions = append(sessions, detail)
	}

	dedup, _ := cmd.Flags().GetBool("dedup")
	if dedup {
		before := len(sessions)
		sessions = generate.DeduplicateSessions(sessions)
		if removed := before - len(sessions); removed > 0 {
			fmt.Printf("Deduplicated: removed %d duplicate(s), %d session(s) remaining.\n", removed, len(sessions))
		}
	}

	return sessions, nil
}

func writeScenarios(cmd *cobra.Command, sessions []*generate.SessionDetail) error {
	packFile, _ := cmd.Flags().GetString("pack")
	outputDir, _ := cmd.Flags().GetString("output")

	if err := os.MkdirAll(outputDir, dirPerms); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	opts := generate.ConvertOptions{Pack: packFile}

	for _, session := range sessions {
		if err := writeSessionScenario(session, opts, outputDir); err != nil {
			return err
		}
	}

	fmt.Printf("\nGenerated %d scenario file(s).\n", len(sessions))
	return nil
}

func writeSessionScenario(
	session *generate.SessionDetail,
	opts generate.ConvertOptions,
	outputDir string,
) error {
	sc, err := generate.ConvertSessionToScenario(session, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: skipping session %s: %v\n", session.ID, err)
		return nil
	}

	data, err := yaml.Marshal(sc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: skipping session %s: marshal error: %v\n", session.ID, err)
		return nil
	}

	filename := sc.Metadata.Name + ".scenario.yaml"
	outPath := filepath.Join(outputDir, filename)

	if err := os.WriteFile(outPath, data, filePerms); err != nil {
		return fmt.Errorf("writing %s: %w", outPath, err)
	}

	fmt.Printf("Generated: %s\n", outPath)
	return nil
}
