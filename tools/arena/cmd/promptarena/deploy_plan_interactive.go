package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/runtime/deploy"
)

var deployPlanCmd = &cobra.Command{
	Use:   "plan",
	Short: "Preview the deployment plan",
	Long: `Show what resources would be created, updated, or destroyed
without making any changes. The plan is saved so that 'deploy apply'
can execute it without re-planning.

Examples:
  promptarena deploy plan --env staging
  promptarena deploy plan --config custom.yaml`,
	RunE: runDeployPlan,
}

func runDeployPlan(cmd *cobra.Command, args []string) error {
	arenaCfg, deployCfg, err := loadFullConfig()
	if err != nil {
		return err
	}

	env := resolveEnvironment()
	projectDir, _ := os.Getwd()
	ctx := context.Background()

	fmt.Printf("Deploy plan for environment: %s (provider: %s)\n", env, deployCfg.Provider)

	packData, err := resolvePackFile()
	if err != nil {
		return err
	}

	configJSON, err := mergedDeployConfigJSON(deployCfg, env)
	if err != nil {
		return err
	}

	stateStore := deploy.NewStateStore(projectDir)
	priorState, err := stateStore.Load()
	if err != nil {
		return fmt.Errorf("failed to load deploy state: %w", err)
	}

	client, err := connectAdapter(deployCfg.Provider, projectDir)
	if err != nil {
		return err
	}
	defer client.Close()

	// Refresh state from the adapter before planning.
	priorStateStr := refreshState(ctx, client, stateStore, priorState, configJSON, env)

	planReq := &deploy.PlanRequest{
		PackJSON:     string(packData),
		DeployConfig: configJSON,
		ArenaConfig:  serializeArenaConfig(arenaCfg),
		Environment:  env,
		PriorState:   priorStateStr,
	}

	plan, err := client.Plan(ctx, planReq)
	if err != nil {
		return fmt.Errorf("plan failed: %w", err)
	}

	printPlan(plan)

	// Save plan for later apply.
	savedPlan := deploy.NewSavedPlan(
		deployCfg.Provider, env, deploy.ComputePackChecksum(packData), plan, planReq,
	)
	if err := stateStore.SavePlan(savedPlan); err != nil {
		return fmt.Errorf("failed to save plan: %w", err)
	}
	fmt.Println("Plan saved. Run 'promptarena deploy apply' to execute.")
	return nil
}
