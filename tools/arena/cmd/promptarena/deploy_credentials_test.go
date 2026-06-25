package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDeployCredentials_Malformed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, promptarenaDotDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "credentials"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadDeployCredentials(); err == nil {
		t.Error("expected parse error for malformed credentials")
	}
}

func TestStoreDeployCredential_BadHome(t *testing.T) {
	// HOME is a regular file, so creating ~/.promptarena fails.
	f := filepath.Join(t.TempDir(), "home-as-file")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", f)
	if err := storeDeployCredential("omnia", "arena.yaml", deployCredential{Token: "t"}); err == nil {
		t.Error("expected error when the home dir cannot hold the credentials store")
	}
}

func TestDeployCredentialKey(t *testing.T) {
	k := deployCredentialKey("omnia", "arena.yaml")
	if !strings.HasPrefix(k, "omnia|") {
		t.Errorf("key should start with provider, got %q", k)
	}
	// Same inputs → same key.
	if k != deployCredentialKey("omnia", "arena.yaml") {
		t.Error("key not deterministic")
	}
	// Different provider → different key.
	if k == deployCredentialKey("agentcore", "arena.yaml") {
		t.Error("different providers should produce different keys")
	}
}

func TestDeployCredentials_RoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Missing file → empty, no error.
	creds, err := loadDeployCredentials()
	if err != nil {
		t.Fatalf("loadDeployCredentials on missing file: %v", err)
	}
	if len(creds) != 0 {
		t.Errorf("expected empty creds, got %d", len(creds))
	}

	// Unknown lookup → not found.
	if _, ok := lookupDeployCredential("omnia", "arena.yaml"); ok {
		t.Error("expected no credential before storing")
	}

	// Store then look up.
	if err := storeDeployCredential("omnia", "arena.yaml", deployCredential{
		Token: "omnia_sk_abc", Endpoint: "https://x", Workspace: "demo",
	}); err != nil {
		t.Fatalf("storeDeployCredential: %v", err)
	}
	tok, ok := lookupDeployCredential("omnia", "arena.yaml")
	if !ok || tok != "omnia_sk_abc" {
		t.Errorf("lookup = (%q, %v), want (omnia_sk_abc, true)", tok, ok)
	}

	// A different config path is a different credential.
	if _, ok := lookupDeployCredential("omnia", "other.yaml"); ok {
		t.Error("credential should be scoped to the config path")
	}

	// Upsert overwrites.
	if err := storeDeployCredential("omnia", "arena.yaml", deployCredential{Token: "omnia_sk_new"}); err != nil {
		t.Fatalf("re-store: %v", err)
	}
	if tok, _ := lookupDeployCredential("omnia", "arena.yaml"); tok != "omnia_sk_new" {
		t.Errorf("expected overwrite, got %q", tok)
	}
}
