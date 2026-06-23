package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/runtime/deploy"
)

var deployImportCmd = &cobra.Command{
	Use:   "import <type> <name> <id>",
	Short: "Import a pre-existing resource into deployment state",
	Long: `Import a resource that already exists in the target environment
into the local deployment state. This allows PromptKit to manage
resources that were created outside of the deploy workflow.

Arguments:
  type   Resource type (e.g., "agent_runtime", "a2a_endpoint")
  name   Resource name to assign in local state
  id     Provider-specific resource identifier

Examples:
  promptarena deploy import agent_runtime my-agent container-abc123
  promptarena deploy import a2a_endpoint my-ep endpoint-xyz789`,
	Args: cobra.ExactArgs(importArgCount),
	RunE: runDeployImport,
}

func runDeployImport(cmd *cobra.Command, args []string) error {
	resourceType := args[0]
	resourceName := args[1]
	identifier := args[2]

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

	fmt.Printf("Importing %s.%s (%s) into environment: %s\n", resourceType, resourceName, identifier, env)

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

	resp, err := client.Import(ctx, &deploy.ImportRequest{
		ResourceType: resourceType,
		ResourceName: resourceName,
		Identifier:   identifier,
		DeployConfig: configJSON,
		Environment:  env,
		PriorState:   priorStateStr,
	})
	if err != nil {
		return fmt.Errorf("import failed: %w", err)
	}

	// Update or create state with the new adapter state.
	if priorState == nil {
		info, _ := client.GetProviderInfo(ctx)
		adapterVersion := ""
		if info != nil {
			adapterVersion = info.Version
		}
		priorState = deploy.NewState(deployCfg.Provider, env, "", "", adapterVersion)
	}
	if resp.State != "" {
		priorState.State = resp.State
	}
	priorState.LastRefreshed = time.Now().UTC().Format(time.RFC3339)
	if err := stateStore.Save(priorState); err != nil {
		return fmt.Errorf("failed to save state after import: %w", err)
	}

	fmt.Println()
	fmt.Printf("  Imported: %s.%s — %s\n", resp.Resource.Type, resp.Resource.Name, resp.Resource.Status)
	if resp.Resource.Detail != "" {
		fmt.Printf("  Detail:   %s\n", resp.Resource.Detail)
	}
	fmt.Println()
	return nil
}
