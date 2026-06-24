package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/deploy"
)

// fakeLoginAdapter implements loginAdapter for tests — no subprocess.
type fakeLoginAdapter struct {
	caps    []string
	profile map[string]interface{}
	token   string
}

func (f *fakeLoginAdapter) GetProviderInfo(_ context.Context) (*deploy.ProviderInfo, error) {
	return &deploy.ProviderInfo{Capabilities: f.caps}, nil
}

func (f *fakeLoginAdapter) GetLoginURL(
	_ context.Context, req *deploy.LoginURLRequest,
) (*deploy.LoginURLResponse, error) {
	// The "authorize URL" is the callback itself with a code, so opening it
	// drives the loopback — simulating the browser's redirect-back.
	return &deploy.LoginURLResponse{
		AuthorizeURL: req.CallbackURL + "?state=" + req.State + "&code=democode",
	}, nil
}

func (f *fakeLoginAdapter) CompleteLogin(
	_ context.Context, req *deploy.CompleteLoginRequest,
) (*deploy.CompleteLoginResponse, error) {
	_ = req
	return &deploy.CompleteLoginResponse{Profile: f.profile, Token: f.token}, nil
}

func TestNewLoginState(t *testing.T) {
	a, err := newLoginState()
	if err != nil {
		t.Fatalf("newLoginState: %v", err)
	}
	b, _ := newLoginState()
	if a == b {
		t.Error("two states should differ")
	}
	if len(a) != loginStateBytes*2 {
		t.Errorf("state len = %d, want %d hex chars", len(a), loginStateBytes*2)
	}
}

func TestResolveLoginProvider(t *testing.T) {
	orig := deployLoginProvider
	defer func() { deployLoginProvider = orig }()

	deployLoginProvider = "omnia"
	if p, err := resolveLoginProvider(); err != nil || p != "omnia" {
		t.Errorf("flag path: (%q, %v), want (omnia, nil)", p, err)
	}

	deployLoginProvider = ""
	origCfg := deployConfig
	deployConfig = filepath.Join(t.TempDir(), "nope.yaml")
	defer func() { deployConfig = origCfg }()
	if _, err := resolveLoginProvider(); err == nil {
		t.Error("expected error with no flag and no config")
	}

	// No flag, but the config has a deploy.provider → use it.
	cfgPath := filepath.Join(t.TempDir(), "arena.yaml")
	cfg := "apiVersion: promptkit.altairalabs.ai/v1alpha1\nkind: Arena\n" +
		"metadata:\n  name: t\nspec:\n  prompt_configs: []\n  providers: []\n" +
		"  defaults:\n    temperature: 0.7\n    max_tokens: 100\n" +
		"  deploy:\n    provider: omnia\n    config:\n      api_endpoint: https://x\n      workspace: demo\n"
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	deployConfig = cfgPath
	if p, err := resolveLoginProvider(); err != nil || p != "omnia" {
		t.Errorf("config path: (%q, %v), want (omnia, nil)", p, err)
	}
}

func TestRequireLoginCapability(t *testing.T) {
	if err := requireLoginCapability(context.Background(),
		&fakeLoginAdapter{caps: []string{deploy.LoginCapability}}, "omnia"); err != nil {
		t.Errorf("expected supported, got %v", err)
	}
	if err := requireLoginCapability(context.Background(),
		&fakeLoginAdapter{caps: nil}, "omnia"); err == nil {
		t.Error("expected error for adapter without the login capability")
	}
}

func TestStartLoopbackServer(t *testing.T) {
	t.Run("good state delivers params", func(t *testing.T) {
		cbURL, ch, shutdown, err := startLoopbackServer(context.Background(), "st8")
		if err != nil {
			t.Fatalf("startLoopbackServer: %v", err)
		}
		defer shutdown()
		resp, err := http.Get(cbURL + "?state=st8&code=cc") //nolint:noctx // test
		if err != nil {
			t.Fatalf("GET callback: %v", err)
		}
		_ = resp.Body.Close()
		select {
		case cb := <-ch:
			if cb.params["code"] != "cc" {
				t.Errorf("code = %q, want cc", cb.params["code"])
			}
		case <-time.After(2 * time.Second):
			t.Fatal("no callback received")
		}
	})

	t.Run("state mismatch is rejected", func(t *testing.T) {
		cbURL, ch, shutdown, err := startLoopbackServer(context.Background(), "right")
		if err != nil {
			t.Fatalf("startLoopbackServer: %v", err)
		}
		defer shutdown()
		resp, err := http.Get(cbURL + "?state=wrong&code=cc") //nolint:noctx // test
		if err != nil {
			t.Fatalf("GET callback: %v", err)
		}
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", resp.StatusCode)
		}
		_ = resp.Body.Close()
		select {
		case <-ch:
			t.Error("should not deliver a callback on state mismatch")
		case <-time.After(200 * time.Millisecond):
		}
	})
}

func TestRunLoginFlow_Unsupported(t *testing.T) {
	// An adapter without the login capability fails fast, before any browser work.
	err := runLoginFlow(context.Background(), &fakeLoginAdapter{caps: nil}, "omnia")
	if err == nil || !strings.Contains(err.Error(), "does not support login") {
		t.Errorf("expected unsupported error, got %v", err)
	}
}

func TestWaitForCallback_Timeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already done
	if _, err := waitForCallback(ctx, make(chan loginCallback)); err == nil {
		t.Error("expected timeout error when context is done")
	}
}

func TestWriteLoginResult_MissingConfig(t *testing.T) {
	origCfg := deployConfig
	deployConfig = filepath.Join(t.TempDir(), "absent.yaml")
	defer func() { deployConfig = origCfg }()

	err := writeLoginResult("omnia", &deploy.CompleteLoginResponse{
		Profile: map[string]interface{}{"workspace": "demo"},
	})
	if err == nil || !strings.Contains(err.Error(), "config file not found") {
		t.Errorf("expected config-not-found error, got %v", err)
	}
}

func TestWriteLoginResult_NoToken(t *testing.T) {
	// A profile with no token still merges the config and skips credential storage.
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "arena.yaml")
	manifest := "apiVersion: promptkit.altairalabs.ai/v1alpha1\nkind: Arena\n" +
		"metadata:\n  name: t\nspec:\n  prompt_configs: []\n"
	if err := os.WriteFile(cfgPath, []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	origCfg := deployConfig
	deployConfig = cfgPath
	defer func() { deployConfig = origCfg }()

	if err := writeLoginResult("omnia", &deploy.CompleteLoginResponse{
		Profile: map[string]interface{}{"workspace": "demo"},
	}); err != nil {
		t.Fatalf("writeLoginResult: %v", err)
	}
	if _, ok := lookupDeployCredential("omnia", cfgPath); ok {
		t.Error("no credential should be stored when there is no token")
	}
}

// loginTestEnv points the package vars at a temp config + simulated browser and
// returns the config path. Cleanup is registered on t.
func loginTestEnv(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	cfgPath := filepath.Join(t.TempDir(), "arena.yaml")
	manifest := "apiVersion: promptkit.altairalabs.ai/v1alpha1\nkind: Arena\n" +
		"metadata:\n  name: t\nspec:\n  prompt_configs: []\n"
	if err := os.WriteFile(cfgPath, []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	origCfg := deployConfig
	deployConfig = cfgPath
	t.Cleanup(func() { deployConfig = origCfg })

	origOpen := openBrowserFunc
	openBrowserFunc = func(u string) error {
		go func() {
			if resp, err := http.Get(u); err == nil { //nolint:noctx // test simulates a browser
				_ = resp.Body.Close()
			}
		}()
		return nil
	}
	t.Cleanup(func() { openBrowserFunc = origOpen })
	return cfgPath
}

func newTestLoginAdapter() *fakeLoginAdapter {
	return &fakeLoginAdapter{
		caps: []string{deploy.LoginCapability},
		profile: map[string]interface{}{
			"api_endpoint": "https://omnia.example.com",
			"workspace":    "demo",
			"providers":    []interface{}{},
		},
		token: "omnia_sk_secret",
	}
}

func assertLoginWrote(t *testing.T, cfgPath string) {
	t.Helper()
	data, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(data), "omnia.example.com") || !strings.Contains(string(data), "demo") {
		t.Errorf("config not merged with profile:\n%s", data)
	}
	if strings.Contains(string(data), "omnia_sk_secret") {
		t.Error("token must NOT be written into the config file")
	}
	if tok, ok := lookupDeployCredential("omnia", cfgPath); !ok || tok != "omnia_sk_secret" {
		t.Errorf("token not stored in credentials: (%q, %v)", tok, ok)
	}
}

func TestRunLoginFlow(t *testing.T) {
	cfgPath := loginTestEnv(t)
	if err := runLoginFlow(context.Background(), newTestLoginAdapter(), "omnia"); err != nil {
		t.Fatalf("runLoginFlow: %v", err)
	}
	assertLoginWrote(t, cfgPath)
}

func TestRunDeployLogin(t *testing.T) {
	cfgPath := loginTestEnv(t)

	origProv := deployLoginProvider
	deployLoginProvider = "omnia"
	defer func() { deployLoginProvider = origProv }()

	origConn := connectLoginAdapter
	connectLoginAdapter = func(_, _ string) (loginAdapter, func(), error) {
		return newTestLoginAdapter(), func() {}, nil
	}
	defer func() { connectLoginAdapter = origConn }()

	if err := runDeployLogin(nil, nil); err != nil {
		t.Fatalf("runDeployLogin: %v", err)
	}
	assertLoginWrote(t, cfgPath)
}
