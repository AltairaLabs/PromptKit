package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/AltairaLabs/PromptKit/runtime/deploy"
)

// deployConfigCmd groups commands that operate on the deploy configuration
// itself (as opposed to a live deployment).
var deployConfigCmd = &cobra.Command{
	Use:   deployConfigCmdName,
	Short: "Manage the deploy configuration",
	Long: `Manage the deploy section of your arena config.

Subcommands:
  import   Merge an exported deploy profile into the config and validate it`,
}

var (
	deployConfigImportProvider     string
	deployConfigImportSkipValidate bool
)

var deployConfigImportCmd = &cobra.Command{
	Use:   "import <profile-file|->",
	Short: "Import a deploy profile into the deploy config and validate it",
	Long: `Merge an exported deploy profile fragment into the deploy config
(under the provider's config: block), then validate the result against the
adapter's validate_config RPC.

The profile is a JSON or YAML mapping — typically exported by your deploy
provider (e.g. Omnia) — containing connection details, a scoped token, and
discovered providers/skills. Pass "-" to read the profile from stdin.

The merge is surgical: only deploy.config is touched, and the rest of your
config file (comments, key order, other sections) is preserved. Existing keys
are overridden by the profile; nested mappings are deep-merged. If the deploy
section or its config block is absent it is created, with the provider set from
--provider.

Examples:
  promptarena deploy config import omnia-profile.json
  promptarena deploy config import - < omnia-profile.yaml
  promptarena deploy config import profile.json --provider omnia
  promptarena deploy config import profile.json --skip-validate`,
	Args: cobra.ExactArgs(1),
	RunE: runDeployConfigImport,
}

func init() {
	deployCmd.AddCommand(deployConfigCmd)
	deployConfigCmd.AddCommand(deployConfigImportCmd)

	deployConfigImportCmd.Flags().StringVar(
		&deployConfigImportProvider, "provider", "omnia",
		"Provider to set when the deploy section is created",
	)
	deployConfigImportCmd.Flags().BoolVar(
		&deployConfigImportSkipValidate, "skip-validate", false,
		"Skip validating the merged config against the adapter",
	)
}

const (
	// configFilePerms is the fallback permission used when writing a config
	// file that does not yet exist. The profile may carry a scoped token, so
	// default to owner-only.
	configFilePerms = 0o600
	// yamlIndentSpaces matches the repo's 2-space YAML convention.
	yamlIndentSpaces = 2

	yamlTagMap = "!!map"
	yamlTagStr = "!!str"

	// deployConfigCmdName is the "config" subcommand name (shared with the
	// --config flag string elsewhere in the package).
	deployConfigCmdName = "config"
)

// readProfile reads a deploy profile fragment from path (or stdin when path is
// "-") and parses it as YAML, which is a superset of JSON. The profile must be
// a mapping.
func readProfile(path string, stdin io.Reader) (map[string]interface{}, error) {
	var (
		data []byte
		err  error
	)
	if path == "-" {
		if stdin == nil {
			stdin = os.Stdin
		}
		data, err = io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("failed to read profile from stdin: %w", err)
		}
	} else {
		data, err = os.ReadFile(path) //nolint:gosec // path is a user-supplied flag
		if err != nil {
			return nil, fmt.Errorf("failed to read profile file %s: %w", path, err)
		}
	}

	var profile map[string]interface{}
	if err := yaml.Unmarshal(data, &profile); err != nil {
		return nil, fmt.Errorf("failed to parse profile (expects a JSON or YAML mapping): %w", err)
	}
	if profile == nil {
		return nil, fmt.Errorf("profile is empty or not a mapping")
	}
	return profile, nil
}

// mergeProfileIntoConfigDoc deep-merges the profile fragment into the
// spec.deploy.config mapping of the arena config YAML document, preserving the
// rest of the document (comments, key order, unrelated sections). The arena
// config is a Kubernetes-style manifest, so the deploy section lives under
// spec. If the deploy section or its config mapping is absent it is created,
// and provider sets spec.deploy.provider when it is otherwise empty.
func mergeProfileIntoConfigDoc(doc []byte, profile map[string]interface{}, provider string) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(doc, &root); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	rootMap := documentMapping(&root)

	specNode := mappingGet(rootMap, "spec")
	if specNode == nil || specNode.Kind != yaml.MappingNode {
		return nil, fmt.Errorf(
			"config is not a valid arena manifest: missing or malformed spec: section",
		)
	}

	deployNode := mappingGet(specNode, "deploy")
	if deployNode == nil {
		deployNode = &yaml.Node{Kind: yaml.MappingNode, Tag: yamlTagMap}
		mappingSet(specNode, "deploy", deployNode)
	}
	if deployNode.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("deploy section is not a mapping")
	}

	if provider != "" {
		provNode := mappingGet(deployNode, "provider")
		switch {
		case provNode == nil:
			mappingSet(deployNode, "provider", scalarNode(provider))
		case strings.TrimSpace(provNode.Value) == "":
			provNode.Kind = yaml.ScalarNode
			provNode.Tag = yamlTagStr
			provNode.Value = provider
		}
	}

	cfgNode := mappingGet(deployNode, "config")
	if cfgNode == nil {
		cfgNode = &yaml.Node{Kind: yaml.MappingNode, Tag: yamlTagMap}
		mappingSet(deployNode, "config", cfgNode)
	}
	if cfgNode.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("deploy.config is not a mapping")
	}

	if err := mergeMapIntoNode(cfgNode, profile); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	// Match the repo's 2-space YAML convention; yaml.Marshal would otherwise
	// reindent the entire document to 4 spaces.
	enc.SetIndent(yamlIndentSpaces)
	if err := enc.Encode(&root); err != nil {
		return nil, fmt.Errorf("failed to marshal merged config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("failed to flush merged config: %w", err)
	}
	return buf.Bytes(), nil
}

// documentMapping returns the root mapping node of a YAML document, creating an
// empty mapping when the document is empty or not a mapping.
func documentMapping(root *yaml.Node) *yaml.Node {
	if root.Kind != yaml.DocumentNode {
		m := &yaml.Node{Kind: yaml.MappingNode, Tag: yamlTagMap}
		root.Kind = yaml.DocumentNode
		root.Content = []*yaml.Node{m}
		return m
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		m := &yaml.Node{Kind: yaml.MappingNode, Tag: yamlTagMap}
		root.Content = []*yaml.Node{m}
		return m
	}
	return root.Content[0]
}

// mappingGet returns the value node for key in a mapping node, or nil.
func mappingGet(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// mappingSet replaces the value for key in a mapping node, appending a new
// key/value pair when the key is absent.
func mappingSet(m *yaml.Node, key string, val *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = val
			return
		}
	}
	m.Content = append(m.Content, scalarNode(key), val)
}

// scalarNode builds a string scalar node.
func scalarNode(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

// mergeMapIntoNode deep-merges src into the target mapping node. Keys are
// applied in sorted order for deterministic output. Nested mappings recurse;
// every other value type replaces the existing node.
func mergeMapIntoNode(target *yaml.Node, src map[string]interface{}) error {
	keys := make([]string, 0, len(src))
	for k := range src {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := src[k]
		existing := mappingGet(target, k)
		if sub, ok := v.(map[string]interface{}); ok && existing != nil && existing.Kind == yaml.MappingNode {
			if err := mergeMapIntoNode(existing, sub); err != nil {
				return err
			}
			continue
		}
		node := &yaml.Node{}
		if err := node.Encode(v); err != nil {
			return fmt.Errorf("failed to encode profile value for key %q: %w", k, err)
		}
		mappingSet(target, k, node)
	}
	return nil
}

func runDeployConfigImport(cmd *cobra.Command, args []string) error {
	profilePath := args[0]

	profile, err := readProfile(profilePath, cmd.InOrStdin())
	if err != nil {
		return err
	}

	if _, statErr := os.Stat(deployConfig); os.IsNotExist(statErr) {
		return fmt.Errorf(
			"config file not found: %s\nCreate one first (e.g. 'promptarena init')",
			deployConfig,
		)
	}

	doc, err := os.ReadFile(deployConfig)
	if err != nil {
		return fmt.Errorf("failed to read config %s: %w", deployConfig, err)
	}

	merged, err := mergeProfileIntoConfigDoc(doc, profile, deployConfigImportProvider)
	if err != nil {
		return err
	}

	mode := os.FileMode(configFilePerms)
	if fi, statErr := os.Stat(deployConfig); statErr == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.WriteFile(deployConfig, merged, mode); err != nil {
		return fmt.Errorf("failed to write config %s: %w", deployConfig, err)
	}
	fmt.Printf("Imported profile into %s (merged under deploy.config)\n", deployConfig)

	if deployConfigImportSkipValidate {
		fmt.Println("Skipping validation (--skip-validate).")
		return nil
	}

	return validateDeployConfig()
}

// validateDeployConfig loads the (just-merged) deploy config and validates it
// against the adapter's validate_config RPC.
func validateDeployConfig() error {
	deployCfg, err := loadDeployConfig()
	if err != nil {
		return err
	}

	env := resolveEnvironment()
	projectDir, _ := os.Getwd()
	ctx := context.Background()

	configJSON, err := mergedDeployConfigJSON(deployCfg, env)
	if err != nil {
		return err
	}

	fmt.Printf("Validating config against %q adapter...\n", deployCfg.Provider)
	client, err := connectAdapter(deployCfg.Provider, projectDir)
	if err != nil {
		return err
	}
	defer client.Close()

	resp, err := client.ValidateConfig(ctx, &deploy.ValidateRequest{Config: configJSON})
	if err != nil {
		return fmt.Errorf("validate_config failed: %w", err)
	}
	if !resp.Valid {
		fmt.Println()
		fmt.Println("Config is INVALID:")
		for _, e := range resp.Errors {
			fmt.Printf("  - %s\n", e)
		}
		return fmt.Errorf("deploy config validation failed (%d error(s))", len(resp.Errors))
	}

	printDeployWarnings(resp.Warnings)
	fmt.Println("Config is valid.")
	return nil
}
