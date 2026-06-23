package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/runtime/deploy"
)

var deployApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply the deployment",
	Long: `Apply the deployment plan to the target environment.
If a saved plan exists (from 'deploy plan'), it will be used.
Otherwise, a new plan is generated before applying.

Examples:
  promptarena deploy apply --env staging
  promptarena deploy apply --env production --config custom.yaml`,
	RunE: runDeployApply,
}

func runDeployApply(cmd *cobra.Command, args []string) error {
	arenaCfg, deployCfg, err := loadFullConfig()
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

	fmt.Printf("Applying deployment to environment: %s (provider: %s)\n", env, deployCfg.Provider)

	packData, err := resolvePackFile()
	if err != nil {
		return err
	}

	stateStore := deploy.NewStateStore(projectDir)
	packChecksum := deploy.ComputePackChecksum(packData)

	// Check for a saved plan.
	var planReq *deploy.PlanRequest
	savedPlan, err := stateStore.LoadPlan()
	if err != nil {
		return fmt.Errorf("failed to load saved plan: %w", err)
	}
	if savedPlan != nil && savedPlan.PackChecksum == packChecksum && savedPlan.Environment == env {
		fmt.Println("  Using saved plan.")
		planReq = savedPlan.Request
	} else {
		if savedPlan != nil {
			fmt.Println("  Saved plan is stale (pack or environment changed), re-planning...")
		}
		configJSON, mergeErr := mergedDeployConfigJSON(deployCfg, env)
		if mergeErr != nil {
			return mergeErr
		}

		priorState, loadErr := stateStore.Load()
		if loadErr != nil {
			return fmt.Errorf("failed to load deploy state: %w", loadErr)
		}
		var priorStateStr string
		if priorState != nil {
			priorStateStr = priorState.State
		}

		planReq = &deploy.PlanRequest{
			PackJSON:     string(packData),
			DeployConfig: configJSON,
			ArenaConfig:  serializeArenaConfig(arenaCfg),
			Environment:  env,
			PriorState:   priorStateStr,
		}
	}

	client, err := connectAdapter(deployCfg.Provider, projectDir)
	if err != nil {
		return err
	}
	defer client.Close()

	adapterState, err := client.Apply(ctx, planReq, func(e *deploy.ApplyEvent) error {
		printDeployEvent(e.Type, e.Message, e.Resource)
		return nil
	})
	if err != nil {
		return fmt.Errorf("apply failed: %w", err)
	}

	info, _ := client.GetProviderInfo(ctx)
	adapterVersion := ""
	if info != nil {
		adapterVersion = info.Version
	}

	newState := deploy.NewState(
		deployCfg.Provider, env, "", packChecksum, adapterVersion,
	)
	newState.State = adapterState
	if err := stateStore.Save(newState); err != nil {
		return fmt.Errorf("failed to save deploy state: %w", err)
	}
	_ = stateStore.DeletePlan()

	fmt.Println()
	fmt.Println("Apply complete.")
	return nil
}
