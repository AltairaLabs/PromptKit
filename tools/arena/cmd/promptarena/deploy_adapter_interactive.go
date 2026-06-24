package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

// adapterRegistryEntry describes a single deploy adapter in the registry.
type adapterRegistryEntry struct {
	Repo         string `json:"repo"`
	Description  string `json:"description"`
	Latest       string `json:"latest"`
	MaintainedBy string `json:"maintained_by"`
}

// adapterRegistry is the top-level registry manifest.
type adapterRegistry struct {
	Adapters map[string]adapterRegistryEntry `json:"adapters"`
}

// Embedded default registry JSON.
var defaultRegistryJSON = `{
  "adapters": {
    "agentcore": {
      "repo": "AltairaLabs/promptarena-deploy-agentcore",
      "description": "AWS Bedrock AgentCore",
      "latest": "0.2.0",
      "maintained_by": "AltairaLabs"
    },
    "omnia": {
      "repo": "AltairaLabs/PromptArena-deploy-omnia",
      "description": "Omnia Kubernetes platform",
      "latest": "1.0.0",
      "maintained_by": "AltairaLabs"
    }
  }
}`

// adapterBinaryPerms is the file permission mode for installed adapter binaries.
const adapterBinaryPerms = 0o755

// adaptersDirName is the subdirectory name where adapter binaries are stored.
const adaptersDirName = "adapters"

// promptarenaDotDir is the hidden configuration directory name.
const promptarenaDotDir = ".promptarena"

// parseRegistry parses a JSON registry manifest into an adapterRegistry.
func parseRegistry(data []byte) (*adapterRegistry, error) {
	var reg adapterRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("failed to parse adapter registry: %w", err)
	}
	return &reg, nil
}

// adapterBinaryName returns the platform-specific binary name for a provider.
func adapterBinaryName(provider, goos, goarch string) string {
	return fmt.Sprintf(
		"promptarena-deploy-%s_%s_%s",
		provider, goos, goarch,
	)
}

// adapterBaseDir returns the user-level adapter directory (~/.promptarena/adapters/).
func adapterBaseDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, promptarenaDotDir, adaptersDirName), nil
}

// adapterDownloadURL builds the GitHub Releases download URL for an adapter binary.
func adapterDownloadURL(
	repo, version, provider, goos, goarch string,
) string {
	binaryName := adapterBinaryName(provider, goos, goarch)
	return fmt.Sprintf(
		"https://github.com/%s/releases/download/v%s/%s",
		repo, version, binaryName,
	)
}

// --- cobra commands ---

var deployAdapterCmd = &cobra.Command{
	Use:   "adapter",
	Short: "Manage deploy adapter binaries",
	Long: `Install, list, and remove deploy adapter binaries.

Adapters are provider-specific binaries that handle deployment
to target environments (e.g., AWS Bedrock AgentCore).

Subcommands:
  install   Download and install an adapter binary
  list      List installed adapter binaries
  remove    Remove an installed adapter binary

Examples:
  promptarena deploy adapter install agentcore
  promptarena deploy adapter install agentcore@0.2.0
  promptarena deploy adapter list
  promptarena deploy adapter remove agentcore`,
}

var deployAdapterInstallCmd = &cobra.Command{
	Use:   "install {provider}[@version]",
	Short: "Download and install an adapter binary",
	Long: `Download an adapter binary from the registry and install it
to ~/.promptarena/adapters/.

If no version is specified, the latest version from the registry is used.

Examples:
  promptarena deploy adapter install agentcore
  promptarena deploy adapter install agentcore@0.2.0`,
	Args: cobra.ExactArgs(1),
	RunE: runAdapterInstall,
}

var deployAdapterListCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed adapter binaries",
	Long: `List all installed adapter binaries found in project-local
and user-level directories.

Search locations:
  .promptarena/adapters/   (project-local)
  ~/.promptarena/adapters/  (user-level)

Examples:
  promptarena deploy adapter list`,
	RunE: runAdapterList,
}

var deployAdapterRemoveCmd = &cobra.Command{
	Use:   "remove {provider}",
	Short: "Remove an installed adapter binary",
	Long: `Remove an adapter binary from the user-level directory
(~/.promptarena/adapters/).

Examples:
  promptarena deploy adapter remove agentcore`,
	Args: cobra.ExactArgs(1),
	RunE: runAdapterRemove,
}

func init() {
	deployCmd.AddCommand(deployAdapterCmd)
	deployAdapterCmd.AddCommand(deployAdapterInstallCmd)
	deployAdapterCmd.AddCommand(deployAdapterListCmd)
	deployAdapterCmd.AddCommand(deployAdapterRemoveCmd)
}

// parseProviderVersion splits "provider" or "provider@version" into parts.
func parseProviderVersion(arg string) (provider, version string) {
	if idx := strings.IndexByte(arg, '@'); idx >= 0 {
		return arg[:idx], arg[idx+1:]
	}
	return arg, ""
}

// loadDefaultRegistry parses the embedded default registry.
func loadDefaultRegistry() (*adapterRegistry, error) {
	return parseRegistry([]byte(defaultRegistryJSON))
}

// httpDownloadFunc is the function used to download files. Replaceable for testing.
var httpDownloadFunc = httpDownload

// httpDownload downloads a URL and returns the response body bytes.
func httpDownload(url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(
		context.Background(), http.MethodGet, url, http.NoBody,
	)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"download failed: HTTP %d from %s",
			resp.StatusCode, url,
		)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	return data, nil
}

// latestVersionFunc resolves the most recent published version for an adapter
// repo. It is a package var so tests can stub network access.
var latestVersionFunc = githubLatestVersion

// githubLatestRelease is the subset of the GitHub releases/latest response read
// to discover the newest adapter version.
type githubLatestRelease struct {
	TagName string `json:"tag_name"`
}

// githubLatestVersion queries the GitHub Releases API for the newest published
// release of repo and returns its version with any leading "v" stripped
// (e.g. "v1.2.0" -> "1.2.0"). Resolving the version live means a newly
// published adapter release is installable WITHOUT rebuilding the CLI: the
// embedded registry only supplies the repo, never a frozen version.
func githubLatestVersion(repo string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("resolve latest version: %w", err)
	}
	// api.github.com rejects requests with no User-Agent.
	req.Header.Set("User-Agent", "promptarena")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("resolve latest version: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("resolve latest version: HTTP %d from %s", resp.StatusCode, url)
	}

	var rel githubLatestRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", fmt.Errorf("parse latest release for %s: %w", repo, err)
	}
	tag := strings.TrimPrefix(rel.TagName, "v")
	if tag == "" {
		return "", fmt.Errorf("no published release found for %s", repo)
	}
	return tag, nil
}

// resolveInstallVersion picks the version to install when the user did not pin
// one. It prefers the live latest release from GitHub; if that lookup fails
// (offline, rate-limited), it falls back to the registry's embedded default so
// installs still work without network access to the API. Returns "" only when
// neither source yields a version.
func resolveInstallVersion(provider string, entry adapterRegistryEntry) string {
	resolved, err := latestVersionFunc(entry.Repo)
	if err == nil && resolved != "" {
		return resolved
	}
	if entry.Latest != "" {
		fmt.Fprintf(os.Stderr,
			"warning: could not resolve latest %s from GitHub (%v); "+
				"falling back to registry default v%s\n",
			provider, err, entry.Latest,
		)
		return entry.Latest
	}
	return ""
}

func runAdapterInstall(_ *cobra.Command, args []string) error {
	provider, version := parseProviderVersion(args[0])

	reg, err := loadDefaultRegistry()
	if err != nil {
		return err
	}

	entry, ok := reg.Adapters[provider]
	if !ok {
		return fmt.Errorf(
			"unknown adapter %q; available: %s",
			provider, registryProviderList(reg),
		)
	}

	if version == "" {
		version = resolveInstallVersion(provider, entry)
		if version == "" {
			return fmt.Errorf(
				"could not resolve a version for adapter %q; specify one explicitly (e.g. %s@<version>)",
				provider, provider,
			)
		}
	}

	goos := runtime.GOOS
	goarch := runtime.GOARCH
	url := adapterDownloadURL(
		entry.Repo, version, provider, goos, goarch,
	)

	destDir, err := adapterBaseDir()
	if err != nil {
		return err
	}

	if mkErr := os.MkdirAll(destDir, adapterBinaryPerms); mkErr != nil {
		return fmt.Errorf("failed to create adapter directory: %w", mkErr)
	}

	// The installed binary uses the canonical name without OS/arch suffix.
	canonicalName := fmt.Sprintf("promptarena-deploy-%s", provider)
	destPath := filepath.Join(destDir, canonicalName)

	fmt.Printf(
		"Installing adapter %q v%s (%s/%s)...\n",
		provider, version, goos, goarch,
	)
	fmt.Printf("  Downloading from: %s\n", url)

	data, err := httpDownloadFunc(url)
	if err != nil {
		return err
	}

	if writeErr := os.WriteFile(destPath, data, adapterBinaryPerms); writeErr != nil {
		return fmt.Errorf("failed to write adapter binary: %w", writeErr)
	}

	fmt.Printf("  Installed to: %s\n", destPath)
	return nil
}

func runAdapterList(_ *cobra.Command, _ []string) error {
	dirs := adapterSearchDirs()

	found := false
	for _, dir := range dirs {
		adapters := listAdaptersInDir(dir.path)
		if len(adapters) == 0 {
			continue
		}
		fmt.Printf("%s (%s):\n", dir.label, dir.path)
		for _, name := range adapters {
			fmt.Printf("  %s\n", name)
		}
		found = true
	}

	if !found {
		fmt.Println("No adapters installed.")
		fmt.Println(
			"Use 'promptarena deploy adapter install <provider>'" +
				" to install one.",
		)
	}
	return nil
}

func runAdapterRemove(_ *cobra.Command, args []string) error {
	provider := args[0]
	canonicalName := fmt.Sprintf("promptarena-deploy-%s", provider)

	destDir, err := adapterBaseDir()
	if err != nil {
		return err
	}

	destPath := filepath.Join(destDir, canonicalName)
	if _, err := os.Stat(destPath); os.IsNotExist(err) {
		return fmt.Errorf(
			"adapter %q not found at %s", provider, destPath,
		)
	}

	if err := os.Remove(destPath); err != nil {
		return fmt.Errorf("failed to remove adapter: %w", err)
	}

	fmt.Printf("Removed adapter %q from %s\n", provider, destPath)
	return nil
}

// adapterDir pairs a directory path with a human-readable label.
type adapterDir struct {
	label string
	path  string
}

// adapterSearchDirs returns the list of directories to scan for adapters.
func adapterSearchDirs() []adapterDir {
	var dirs []adapterDir

	// Project-local
	cwd, err := os.Getwd()
	if err == nil {
		localDir := filepath.Join(
			cwd, promptarenaDotDir, adaptersDirName,
		)
		dirs = append(dirs, adapterDir{
			label: "project-local",
			path:  localDir,
		})
	}

	// User-level
	userDir, err := adapterBaseDir()
	if err == nil {
		dirs = append(dirs, adapterDir{
			label: "user-level",
			path:  userDir,
		})
	}

	return dirs
}

// listAdaptersInDir scans a directory for adapter binaries.
func listAdaptersInDir(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var adapters []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "promptarena-deploy-") {
			adapters = append(adapters, name)
		}
	}
	return adapters
}

// registryProviderList returns a comma-separated list of known providers.
func registryProviderList(reg *adapterRegistry) string {
	providers := make([]string, 0, len(reg.Adapters))
	for name := range reg.Adapters {
		providers = append(providers, name)
	}
	return strings.Join(providers, ", ")
}
