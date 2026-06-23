package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/runtime/deploy"
)

var deployDestroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Tear down a deployment",
	Long: `Destroy all resources associated with a deployment in the target environment.

This is a destructive operation. Use with caution.

Examples:
  promptarena deploy destroy --env staging
  promptarena deploy destroy --env production`,
	RunE: runDeployDestroy,
}

func runDeployDestroy(cmd *cobra.Command, args []string) error {
	deployCfg, err := loadDeployConfig()
	if err != nil {
		return err
	}

	env := resolveEnvironment()
	projectDir, _ := os.Getwd()
	ctx := context.Background()

	// Acquire deploy lock.
	unlock, err := acquireLock(projectDir)
	if err != nil {
		return err
	}
	defer unlock()

	fmt.Printf("Destroying deployment in environment: %s (provider: %s)\n", env, deployCfg.Provider)

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
		fmt.Println("  No deployment state found. Nothing to destroy.")
		return nil
	}

	client, err := connectAdapter(deployCfg.Provider, projectDir)
	if err != nil {
		return err
	}
	defer client.Close()

	err = client.Destroy(ctx, &deploy.DestroyRequest{
		DeployConfig: configJSON,
		Environment:  env,
		PriorState:   priorState.State,
	}, func(e *deploy.DestroyEvent) error {
		printDeployEvent(e.Type, e.Message, e.Resource)
		return nil
	})
	if err != nil {
		return fmt.Errorf("destroy failed: %w", err)
	}

	if err := stateStore.Delete(); err != nil {
		return fmt.Errorf("failed to remove deploy state: %w", err)
	}

	fmt.Println()
	fmt.Println("Deployment destroyed.")
	return nil
}
