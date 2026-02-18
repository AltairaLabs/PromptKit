package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/deploy"
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy the arena pack to a target environment",
	Long: `Deploy the arena pack to a target environment using a deploy adapter.

When run without a subcommand, deploy executes a plan followed by apply.

Subcommands:
  plan      Preview what would be deployed
  apply     Apply the deployment
  status    Show current deployment status
  destroy   Tear down a deployment

Examples:
  promptarena deploy --env production
  promptarena deploy plan --env staging
  promptarena deploy apply --env staging
  promptarena deploy status --env production
  promptarena deploy destroy --env staging`,
	RunE: runDeploy,
}

var (
	deployEnv      string
	deployConfig   string
	deployPackFile string
)

func init() {
	rootCmd.AddCommand(deployCmd)

	deployCmd.PersistentFlags().StringVarP(&deployEnv, "env", "e", "", "Target environment")
	deployCmd.PersistentFlags().StringVar(&deployConfig, "config", "arena.yaml", "Config file path")
	deployCmd.PersistentFlags().StringVar(&deployPackFile, "pack", "", "Pack file path (default: auto-detect *.pack.json)")

	deployCmd.AddCommand(deployPlanCmd)
	deployCmd.AddCommand(deployApplyCmd)
	deployCmd.AddCommand(deployStatusCmd)
	deployCmd.AddCommand(deployDestroyCmd)
}

// loadDeployConfig loads the arena config and returns the deploy section.
func loadDeployConfig() (*config.DeployConfig, error) {
	if _, err := os.Stat(deployConfig); os.IsNotExist(err) {
		return nil, fmt.Errorf("config file not found: %s", deployConfig)
	}

	cfg, err := config.LoadConfig(deployConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	if cfg.Deploy == nil {
		return nil, fmt.Errorf(
			"no deploy configuration found in %s\nAdd a 'deploy' section to your arena config",
			deployConfig,
		)
	}

	return cfg.Deploy, nil
}

const defaultEnvironment = "default"

// resolveEnvironment returns the target environment name, falling back to "default" if not specified.
func resolveEnvironment() string {
	if deployEnv != "" {
		return deployEnv
	}
	return defaultEnvironment
}

// resolvePackFile finds and reads the .pack.json file. If --pack is set, use
// that; otherwise look for a single *.pack.json in the current directory.
func resolvePackFile() ([]byte, error) {
	path := deployPackFile
	if path == "" {
		matches, err := filepath.Glob("*.pack.json")
		if err != nil {
			return nil, fmt.Errorf("failed to search for pack files: %w", err)
		}
		switch len(matches) {
		case 0:
			return nil, fmt.Errorf(
				"no .pack.json file found in the current directory\n" +
					"Run 'packc compile' first or specify --pack <file>",
			)
		case 1:
			path = matches[0]
		default:
			return nil, fmt.Errorf(
				"multiple .pack.json files found: %s\nSpecify one with --pack <file>",
				strings.Join(matches, ", "),
			)
		}
	}

	data, err := os.ReadFile(path) //nolint:gosec // path is from user flag or glob
	if err != nil {
		return nil, fmt.Errorf("failed to read pack file %s: %w", path, err)
	}
	fmt.Printf("  Pack file:   %s\n", path)
	return data, nil
}

// mergedDeployConfigJSON merges the base deploy config with environment-specific
// overrides and returns the result as a JSON string.
func mergedDeployConfigJSON(deployCfg *config.DeployConfig, env string) (string, error) {
	merged := make(map[string]interface{})
	for k, v := range deployCfg.Config {
		merged[k] = v
	}
	if envCfg, ok := deployCfg.Environments[env]; ok && envCfg != nil {
		for k, v := range envCfg.Config {
			merged[k] = v
		}
	}
	data, err := json.Marshal(merged)
	if err != nil {
		return "", fmt.Errorf("failed to marshal deploy config: %w", err)
	}
	return string(data), nil
}

// connectAdapter discovers the adapter binary and returns a connected client.
func connectAdapter(provider, projectDir string) (*deploy.AdapterClient, error) {
	mgr := deploy.NewAdapterManager(projectDir)
	binaryPath, err := mgr.Discover(provider)
	if err != nil {
		return nil, fmt.Errorf(
			"adapter not found for provider %q: %w\n"+
				"Install it with: promptarena deploy adapter install %s",
			provider, err, provider,
		)
	}
	fmt.Printf("  Adapter:     %s\n", binaryPath)
	client, err := deploy.NewAdapterClient(binaryPath)
	if err != nil {
		return nil, fmt.Errorf("failed to start adapter: %w", err)
	}
	return client, nil
}

// printPlan displays a deployment plan to the user.
func printPlan(plan *deploy.PlanResponse) {
	fmt.Println()
	fmt.Printf("Plan: %s\n", plan.Summary)
	fmt.Println()
	if len(plan.Changes) == 0 {
		fmt.Println("  No changes required.")
		return
	}
	for _, c := range plan.Changes {
		symbol := " "
		switch c.Action {
		case deploy.ActionCreate:
			symbol = "+"
		case deploy.ActionUpdate:
			symbol = "~"
		case deploy.ActionDelete:
			symbol = "-"
		case deploy.ActionNoChange:
			symbol = " "
		}
		line := fmt.Sprintf("  %s %s.%s", symbol, c.Type, c.Name)
		if c.Detail != "" {
			line += " (" + c.Detail + ")"
		}
		fmt.Println(line)
	}
	fmt.Println()
}

// printStatus displays a deployment status to the user.
func printStatus(status *deploy.StatusResponse) {
	fmt.Printf("  Status: %s\n", status.Status)
	if len(status.Resources) > 0 {
		fmt.Println()
		for _, r := range status.Resources {
			line := fmt.Sprintf("  %s.%s: %s", r.Type, r.Name, r.Status)
			if r.Detail != "" {
				line += " — " + r.Detail
			}
			fmt.Println(line)
		}
	}
	fmt.Println()
}

// acquireLock acquires the deploy lock and returns an unlock function.
func acquireLock(projectDir string) (func(), error) {
	locker := deploy.NewLocker(projectDir)
	if err := locker.Lock(); err != nil {
		return nil, err
	}
	return func() { _ = locker.Unlock() }, nil
}

// --- deploy (plan + apply) ---

func runDeploy(cmd *cobra.Command, args []string) error {
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

	fmt.Printf("Deploying with provider %q to environment %q\n", deployCfg.Provider, env)
	fmt.Println()

	// Load pack.
	packData, err := resolvePackFile()
	if err != nil {
		return err
	}

	configJSON, err := mergedDeployConfigJSON(deployCfg, env)
	if err != nil {
		return err
	}

	// Load prior state.
	stateStore := deploy.NewStateStore(projectDir)
	priorState, err := stateStore.Load()
	if err != nil {
		return fmt.Errorf("failed to load deploy state: %w", err)
	}
	var priorStateStr string
	if priorState != nil {
		priorStateStr = priorState.State
	}

	// Connect adapter.
	client, err := connectAdapter(deployCfg.Provider, projectDir)
	if err != nil {
		return err
	}
	defer client.Close()

	// Step 1: Plan.
	fmt.Println()
	fmt.Println("Step 1: Planning deployment...")

	planReq := &deploy.PlanRequest{
		PackJSON:     string(packData),
		DeployConfig: configJSON,
		Environment:  env,
		PriorState:   priorStateStr,
	}

	plan, err := client.Plan(ctx, planReq)
	if err != nil {
		return fmt.Errorf("plan failed: %w", err)
	}
	printPlan(plan)

	// Step 2: Apply.
	fmt.Println("Step 2: Applying deployment...")

	adapterState, err := client.Apply(ctx, planReq, nil)
	if err != nil {
		return fmt.Errorf("apply failed: %w", err)
	}

	// Save state and clean up any saved plan.
	info, _ := client.GetProviderInfo(ctx)
	adapterVersion := ""
	if info != nil {
		adapterVersion = info.Version
	}

	newState := deploy.NewState(
		deployCfg.Provider, env, "", deploy.ComputePackChecksum(packData), adapterVersion,
	)
	newState.State = adapterState
	if err := stateStore.Save(newState); err != nil {
		return fmt.Errorf("failed to save deploy state: %w", err)
	}
	_ = stateStore.DeletePlan()

	fmt.Println("Deployment complete.")
	return nil
}

// --- deploy plan ---

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
	deployCfg, err := loadDeployConfig()
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
	var priorStateStr string
	if priorState != nil {
		priorStateStr = priorState.State
	}

	client, err := connectAdapter(deployCfg.Provider, projectDir)
	if err != nil {
		return err
	}
	defer client.Close()

	planReq := &deploy.PlanRequest{
		PackJSON:     string(packData),
		DeployConfig: configJSON,
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

// --- deploy apply ---

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
			Environment:  env,
			PriorState:   priorStateStr,
		}
	}

	client, err := connectAdapter(deployCfg.Provider, projectDir)
	if err != nil {
		return err
	}
	defer client.Close()

	adapterState, err := client.Apply(ctx, planReq, nil)
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

// --- deploy status ---

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

// --- deploy destroy ---

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
	}, nil)
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
