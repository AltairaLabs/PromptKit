package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeployAdapterParseRegistry(t *testing.T) {
	data := []byte(`{
		"adapters": {
			"agentcore": {
				"repo": "AltairaLabs/promptarena-deploy-agentcore",
				"description": "AWS Bedrock AgentCore",
				"latest": "0.2.0",
				"maintained_by": "AltairaLabs"
			},
			"cloudrun": {
				"repo": "AltairaLabs/promptarena-deploy-cloudrun",
				"description": "Google Cloud Run",
				"latest": "1.0.0",
				"maintained_by": "AltairaLabs"
			}
		}
	}`)

	reg, err := parseRegistry(data)
	if err != nil {
		t.Fatalf("parseRegistry() error: %v", err)
	}

	if len(reg.Adapters) != 2 {
		t.Fatalf("expected 2 adapters, got %d", len(reg.Adapters))
	}

	ac, ok := reg.Adapters["agentcore"]
	if !ok {
		t.Fatal("expected agentcore adapter in registry")
	}
	if ac.Latest != "0.2.0" {
		t.Errorf("agentcore latest = %q, want %q", ac.Latest, "0.2.0")
	}
	if ac.Repo != "AltairaLabs/promptarena-deploy-agentcore" {
		t.Errorf("agentcore repo = %q, want AltairaLabs/...", ac.Repo)
	}
}

func TestDeployAdapterParseRegistryInvalid(t *testing.T) {
	_, err := parseRegistry([]byte(`{invalid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestDeployAdapterBinaryName(t *testing.T) {
	tests := []struct {
		provider string
		goos     string
		goarch   string
		want     string
	}{
		{
			"agentcore", "darwin", "arm64",
			"promptarena-deploy-agentcore_darwin_arm64",
		},
		{
			"agentcore", "linux", "amd64",
			"promptarena-deploy-agentcore_linux_amd64",
		},
		{
			"cloudrun", "windows", "amd64",
			"promptarena-deploy-cloudrun_windows_amd64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := adapterBinaryName(
				tt.provider, tt.goos, tt.goarch,
			)
			if got != tt.want {
				t.Errorf(
					"adapterBinaryName(%q, %q, %q) = %q, want %q",
					tt.provider, tt.goos, tt.goarch,
					got, tt.want,
				)
			}
		})
	}
}

func TestDeployAdapterDownloadURL(t *testing.T) {
	url := adapterDownloadURL(
		"AltairaLabs/promptarena-deploy-agentcore",
		"0.2.0", "agentcore", "darwin", "arm64",
	)
	want := "https://github.com/AltairaLabs/" +
		"promptarena-deploy-agentcore/releases/download/" +
		"v0.2.0/promptarena-deploy-agentcore_darwin_arm64"
	if url != want {
		t.Errorf("adapterDownloadURL() = %q, want %q", url, want)
	}
}

func TestDeployAdapterParseProviderVersion(t *testing.T) {
	tests := []struct {
		input       string
		wantProv    string
		wantVersion string
	}{
		{"agentcore", "agentcore", ""},
		{"agentcore@0.2.0", "agentcore", "0.2.0"},
		{"cloudrun@1.0.0-rc1", "cloudrun", "1.0.0-rc1"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			p, v := parseProviderVersion(tt.input)
			if p != tt.wantProv || v != tt.wantVersion {
				t.Errorf(
					"parseProviderVersion(%q) = (%q, %q), "+
						"want (%q, %q)",
					tt.input, p, v,
					tt.wantProv, tt.wantVersion,
				)
			}
		})
	}
}

func TestDeployAdapterListAdaptersInDir(t *testing.T) {
	dir := t.TempDir()

	// Create some adapter binaries.
	names := []string{
		"promptarena-deploy-agentcore",
		"promptarena-deploy-cloudrun",
	}
	for _, name := range names {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(
			path, []byte("binary"), adapterBinaryPerms,
		); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}

	// Create a non-adapter file (should be excluded).
	other := filepath.Join(dir, "some-other-binary")
	if err := os.WriteFile(
		other, []byte("other"), adapterBinaryPerms,
	); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Create a subdirectory (should be excluded).
	subdir := filepath.Join(dir, "promptarena-deploy-subdir")
	if err := os.Mkdir(subdir, adapterBinaryPerms); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}

	adapters := listAdaptersInDir(dir)
	if len(adapters) != 2 {
		t.Fatalf("expected 2 adapters, got %d: %v", len(adapters), adapters)
	}

	// Verify both expected adapters are found.
	found := map[string]bool{}
	for _, a := range adapters {
		found[a] = true
	}
	for _, name := range names {
		if !found[name] {
			t.Errorf("expected adapter %q in list", name)
		}
	}
}

func TestDeployAdapterListAdaptersInNonexistentDir(t *testing.T) {
	adapters := listAdaptersInDir("/nonexistent/path/adapters")
	if len(adapters) != 0 {
		t.Errorf("expected 0 adapters for nonexistent dir, got %d", len(adapters))
	}
}

func TestDeployAdapterRegistryProviderList(t *testing.T) {
	reg := &adapterRegistry{
		Adapters: map[string]adapterRegistryEntry{
			"alpha": {Latest: "1.0.0"},
		},
	}
	got := registryProviderList(reg)
	if got != "alpha" {
		t.Errorf("registryProviderList() = %q, want %q", got, "alpha")
	}
}

func TestDeployAdapterLoadDefaultRegistry(t *testing.T) {
	reg, err := loadDefaultRegistry()
	if err != nil {
		t.Fatalf("loadDefaultRegistry() error: %v", err)
	}
	if _, ok := reg.Adapters["agentcore"]; !ok {
		t.Error("expected agentcore in default registry")
	}
}
