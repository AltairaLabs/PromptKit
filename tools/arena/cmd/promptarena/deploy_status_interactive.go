package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/runtime/deploy"
)

var deployStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show deployment status",
	Long: `Show the current status of a deployment in the target environment.

Examples:
  promptarena deploy status --env production
  promptarena deploy status --env staging`,
	RunE: runDeployStatus,
}

func runDeployStatus(cmd *cobra.Command, args []string) error {
	deployCfg, err := loadDeployConfig()
	if err != nil {
		return err
	}

	env := resolveEnvironment()
	projectDir, _ := os.Getwd()
	ctx := context.Background()

	fmt.Printf("Deployment status for environment: %s (provider: %s)\n", env, deployCfg.Provider)

	configJSON, err := mergedDeployConfigJSON(deployCfg, env)
	if err != nil {
		return err
	}

	stateStore := deploy.NewStateStore(projectDir)
	priorState, err := stateStore.Load()
	if err != nil {
		return fmt.Errorf("failed to load deploy state: %w", err)
	}

	if priorState == nil {
		fmt.Println()
		fmt.Println("  No deployment state found. Run 'promptarena deploy' first.")
		return nil
	}

	var priorStateStr string
	if priorState != nil {
		priorStateStr = priorState.State
	}

	client, err := connectAdapter(deployCfg.Provider, projectDir)
	if err != nil {
		return err
	}
	defer client.Close()

	status, err := client.Status(ctx, &deploy.StatusRequest{
		DeployConfig: configJSON,
		Environment:  env,
		PriorState:   priorStateStr,
	})
	if err != nil {
		return fmt.Errorf("status check failed: %w", err)
	}

	fmt.Println()
	printStatus(status)

	fmt.Printf("  Last deployed: %s\n", priorState.LastDeployed)
	fmt.Printf("  Pack checksum: %s\n", priorState.PackChecksum)
	return nil
}
