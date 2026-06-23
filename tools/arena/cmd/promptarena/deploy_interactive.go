package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/deploy"
	"github.com/AltairaLabs/PromptKit/tools/packc/compiler"
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
  refresh   Refresh local state from live environment
  import    Import a pre-existing resource

Examples:
  promptarena deploy --env production
  promptarena deploy plan --env staging
  promptarena deploy apply --env staging
  promptarena deploy status --env production
  promptarena deploy destroy --env staging
  promptarena deploy refresh --env production
  promptarena deploy import agent_runtime my-agent container-abc123`,
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
	deployCmd.PersistentFlags().StringVar(&deployPackFile, "pack", "",
		"Optional pre-compiled *.pack.json to deploy (default: compile from --config)")

	deployCmd.AddCommand(deployPlanCmd)
	deployCmd.AddCommand(deployApplyCmd)
	deployCmd.AddCommand(deployStatusCmd)
	deployCmd.AddCommand(deployDestroyCmd)
	deployCmd.AddCommand(deployRefreshCmd)
	deployCmd.AddCommand(deployImportCmd)
}

// loadDeployConfig loads the arena config and returns the deploy section.
func loadDeployConfig() (*config.DeployConfig, error) {
	_, deployCfg, err := loadFullConfig()
	return deployCfg, err
}

// loadFullConfig loads the arena config and returns both the full config and deploy section.
func loadFullConfig() (*config.Config, *config.DeployConfig, error) {
	if _, err := os.Stat(deployConfig); os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("config file not found: %s", deployConfig)
	}

	cfg, err := config.LoadConfig(deployConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load config: %w", err)
	}

	if cfg.Deploy == nil {
		return nil, nil, fmt.Errorf(
			"no deploy configuration found in %s\nAdd a 'deploy' section to your arena config",
			deployConfig,
		)
	}

	return cfg, cfg.Deploy, nil
}

// serializeArenaConfig serializes the full arena config as JSON for adapter consumption.
func serializeArenaConfig(cfg *config.Config) string {
	data, err := json.Marshal(cfg)
	if err != nil {
		log.Printf("Warning: failed to serialize arena config: %v", err)
		return ""
	}
	return string(data)
}

const defaultEnvironment = "default"

// importArgCount is the number of positional arguments for the import command.
const importArgCount = 3

// resolveEnvironment returns the target environment name, falling back to "default" if not specified.
func resolveEnvironment() string {
	if deployEnv != "" {
		return deployEnv
	}
	return defaultEnvironment
}

// resolvePackFile resolves the pack JSON to deploy. If --pack is set, it reads
// that pre-compiled *.pack.json file (explicit override). Otherwise it compiles
// the arena config (--config) on the fly and returns the freshly compiled JSON,
// so users do not need to run a separate compile step before deploying.
func resolvePackFile() ([]byte, error) {
	if deployPackFile != "" {
		data, err := os.ReadFile(deployPackFile) //nolint:gosec // path is from user flag
		if err != nil {
			return nil, fmt.Errorf("failed to read pack file %s: %w", deployPackFile, err)
		}
		fmt.Printf("  Pack file:   %s\n", deployPackFile)
		return data, nil
	}

	result, err := compiler.Compile(deployConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to compile pack from %s: %w", deployConfig, err)
	}
	for _, w := range result.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	fmt.Printf("  Pack:        compiled from %s\n", deployConfig)
	return result.JSON, nil
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
		case deploy.ActionDrift:
			symbol = "!"
		}
		line := fmt.Sprintf("  %s %s.%s", symbol, c.Type, c.Name)
		if c.Detail != "" {
			line += " (" + c.Detail + ")"
		}
		fmt.Println(line)
	}
	fmt.Println()
}

// resultStatusSymbol maps a resource result status to a display symbol,
// consistent with printPlan's action symbols.
func resultStatusSymbol(status string) string {
	switch status {
	case "created":
		return "+"
	case "updated":
		return "~"
	case "deleted":
		return "-"
	case "failed":
		return "!"
	default:
		return " "
	}
}

// printDeployEvent renders a streaming apply/destroy event to stdout. Apply and
// Destroy share the same event shape (type, message, resource), so both
// callbacks delegate here to surface per-resource progress live.
func printDeployEvent(eventType, message string, res *deploy.ResourceResult) {
	switch eventType {
	case "resource":
		if res != nil {
			line := fmt.Sprintf("  %s %s.%s (%s)",
				resultStatusSymbol(res.Status), res.Type, res.Name, res.Status)
			if res.Detail != "" {
				line += ": " + res.Detail
			}
			fmt.Println(line)
		}
	case "error":
		fmt.Printf("  ! %s\n", message)
	}
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

// refreshState calls Status on the adapter and updates local state with the
// refreshed adapter state. It is a soft-fail operation: if the refresh fails,
// it logs a warning and returns the existing prior state string unchanged.
func refreshState(
	ctx context.Context,
	client *deploy.AdapterClient,
	stateStore *deploy.StateStore,
	priorState *deploy.State,
	configJSON, env string,
) string {
	if priorState == nil {
		return ""
	}

	status, err := client.Status(ctx, &deploy.StatusRequest{
		DeployConfig: configJSON,
		Environment:  env,
		PriorState:   priorState.State,
	})
	if err != nil {
		log.Printf("Warning: state refresh failed: %v (proceeding with cached state)", err)
		return priorState.State
	}

	if status.State != "" {
		priorState.State = status.State
		priorState.LastRefreshed = time.Now().UTC().Format(time.RFC3339)
		if saveErr := stateStore.Save(priorState); saveErr != nil {
			log.Printf("Warning: failed to persist refreshed state: %v", saveErr)
		}
	}

	return priorState.State
}

// --- deploy (plan + apply) ---

func runDeploy(cmd *cobra.Command, args []string) error {
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

	// Connect adapter.
	client, err := connectAdapter(deployCfg.Provider, projectDir)
	if err != nil {
		return err
	}
	defer client.Close()

	// Refresh state from the adapter before planning.
	priorStateStr := refreshState(ctx, client, stateStore, priorState, configJSON, env)

	// Step 1: Plan.
	fmt.Println()
	fmt.Println("Step 1: Planning deployment...")

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

	// Step 2: Apply.
	fmt.Println("Step 2: Applying deployment...")

	adapterState, err := client.Apply(ctx, planReq, func(e *deploy.ApplyEvent) error {
		printDeployEvent(e.Type, e.Message, e.Resource)
		return nil
	})
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
