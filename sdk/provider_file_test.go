package sdk

import (
	"os"
	"path/filepath"
	"testing"

	pkgconfig "github.com/AltairaLabs/PromptKit/pkg/config"
)

func cred(tok string) *pkgconfig.CredentialConfig {
	return &pkgconfig.CredentialConfig{APIKey: tok}
}

func TestApplyProviderConfig_LLMBecomesAgent(t *testing.T) {
	c := &config{}
	if err := c.applyProviderConfig(&pkgconfig.Provider{ID: "m", Type: "mock", Role: pkgconfig.RoleLLM}); err != nil {
		t.Fatalf("applyProviderConfig: %v", err)
	}
	if c.getAgentProvider() == nil {
		t.Fatal("expected agent provider to be set")
	}
}

func TestApplyProviderConfig_SecondLLMPooledNotAgent(t *testing.T) {
	c := &config{}
	if err := c.applyProviderConfig(&pkgconfig.Provider{ID: "first", Type: "mock", Role: pkgconfig.RoleLLM}); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := c.applyProviderConfig(&pkgconfig.Provider{ID: "second", Type: "mock", Role: pkgconfig.RoleLLM}); err != nil {
		t.Fatalf("second: %v", err)
	}
	if got := c.agentProviderID; got != "first" {
		t.Fatalf("agent should remain first-declared, got %q", got)
	}
	if _, ok := c.providers.Get("second"); !ok {
		t.Fatal("second provider should still be registered in the pool")
	}
}

func TestApplyProviderConfig_TTS(t *testing.T) {
	c := &config{}
	if err := c.applyProviderConfig(&pkgconfig.Provider{Type: "openai", Role: pkgconfig.RoleTTS, Credential: cred("tok")}); err != nil {
		t.Fatalf("tts: %v", err)
	}
	if c.ttsService == nil {
		t.Fatal("expected ttsService set")
	}
}

func TestApplyProviderConfig_Inference(t *testing.T) {
	c := &config{}
	if err := c.applyProviderConfig(&pkgconfig.Provider{ID: "hf", Type: "huggingface", Role: pkgconfig.RoleInference, Credential: cred("tok")}); err != nil {
		t.Fatalf("inference: %v", err)
	}
	if _, err := c.classifyRegistry.AudioClassifier("hf"); err != nil {
		t.Fatalf("hf classifier should resolve: %v", err)
	}
}

func TestApplyProviderConfig_EmbeddingPlatformRejected(t *testing.T) {
	c := &config{}
	err := c.applyProviderConfig(&pkgconfig.Provider{
		Type:     "openai",
		Role:     pkgconfig.RoleEmbedding,
		Platform: &pkgconfig.PlatformConfig{Type: "azure"},
	})
	if err == nil {
		t.Fatal("expected platform-auth embedding via file to be rejected")
	}
}

func TestApplyProviderConfig_UnknownRoleRejected(t *testing.T) {
	c := &config{}
	if err := c.applyProviderConfig(&pkgconfig.Provider{Type: "x", Role: "bogus"}); err == nil {
		t.Fatal("expected unknown-role error")
	}
}

// --- Task 2: WithProviderFile + WithProvidersDir ---

func writeProviderYAML(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

const ttsProviderYAML = `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: openai-tts
spec:
  id: openai-tts
  type: openai
  role: tts
  credential:
    api_key: tok
`

func TestWithProviderFile_LoadsTTS(t *testing.T) {
	dir := t.TempDir()
	path := writeProviderYAML(t, dir, "tts.provider.yaml", ttsProviderYAML)

	c := &config{}
	if err := WithProviderFile(path)(c); err != nil {
		t.Fatalf("WithProviderFile: %v", err)
	}
	if c.ttsService == nil {
		t.Fatal("expected ttsService set from file")
	}
}

func TestWithProviderFile_MissingFile(t *testing.T) {
	c := &config{}
	if err := WithProviderFile("/nonexistent/x.provider.yaml")(c); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestWithProvidersDir_LoadsAllFirstWinsAgent(t *testing.T) {
	dir := t.TempDir()
	// Two llm providers + one tts; glob is sorted, so "a-llm" sorts before "b-llm".
	writeProviderYAML(t, dir, "a-llm.provider.yaml", `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: a
spec:
  id: a
  type: mock
  role: llm
`)
	writeProviderYAML(t, dir, "b-llm.provider.yaml", `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: b
spec:
  id: b
  type: mock
  role: llm
`)
	writeProviderYAML(t, dir, "tts.provider.yaml", ttsProviderYAML)

	c := &config{}
	if err := WithProvidersDir(dir)(c); err != nil {
		t.Fatalf("WithProvidersDir: %v", err)
	}
	if c.agentProviderID != "a" {
		t.Fatalf("first-declared (sorted) llm should be agent, got %q", c.agentProviderID)
	}
	if _, ok := c.providers.Get("b"); !ok {
		t.Fatal("second llm should still be pooled")
	}
	if c.ttsService == nil {
		t.Fatal("tts from dir should be set")
	}
}

func TestWithProvidersDir_Empty(t *testing.T) {
	c := &config{}
	if err := WithProvidersDir(t.TempDir())(c); err != nil {
		t.Fatalf("empty dir should be a no-op, got %v", err)
	}
}
