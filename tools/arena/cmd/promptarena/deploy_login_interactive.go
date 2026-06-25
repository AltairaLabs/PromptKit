package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/pkg/browser"
	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/runtime/deploy"
)

var deployLoginProvider string

// openBrowserFunc opens a URL in the user's browser. A package var so tests can
// simulate the browser without launching one.
var openBrowserFunc = browser.OpenURL

// connectLoginAdapter discovers and starts the adapter for login, returning it
// as the minimal loginAdapter plus a close func. A package var so tests can
// inject a fake without launching a subprocess.
var connectLoginAdapter = func(provider, projectDir string) (loginAdapter, func(), error) {
	c, err := connectAdapter(provider, projectDir)
	if err != nil {
		return nil, nil, err
	}
	return c, func() { _ = c.Close() }, nil
}

// loginAdapter is the slice of the adapter client the login flow needs. It lets
// the flow be tested with a fake, without launching an adapter subprocess.
type loginAdapter interface {
	GetProviderInfo(ctx context.Context) (*deploy.ProviderInfo, error)
	GetLoginURL(ctx context.Context, req *deploy.LoginURLRequest) (*deploy.LoginURLResponse, error)
	CompleteLogin(ctx context.Context, req *deploy.CompleteLoginRequest) (*deploy.CompleteLoginResponse, error)
}

// loginTimeout bounds how long the CLI waits for the browser callback.
const loginTimeout = 5 * time.Minute

// loginStateBytes is the number of random bytes in the CSRF state nonce.
const loginStateBytes = 16

// loopbackReadHeaderTimeout bounds header reads on the loopback callback server.
const loopbackReadHeaderTimeout = 10 * time.Second

var deployLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate in the browser and write the deploy config",
	Long: `Open a browser to the provider, authenticate, and write the resulting
deploy profile (endpoint, workspace, providers, skills) into the arena config.
The scoped token is stored in ~/.promptarena/credentials — never in the config
file.

The provider must ship an adapter that supports login. The provider is taken
from --provider, or from the existing deploy section of the config.

Examples:
  promptarena deploy login --provider omnia
  promptarena deploy login                 # provider read from arena.yaml`,
	RunE: runDeployLogin,
}

func init() {
	deployCmd.AddCommand(deployLoginCmd)
	deployLoginCmd.Flags().StringVar(&deployLoginProvider, "provider", "",
		"Adapter provider to log in with (default: from the config's deploy section)")
}

// resolveLoginProvider returns the provider to log in with: the --provider flag
// wins; otherwise the existing deploy.provider in the config is used.
func resolveLoginProvider() (string, error) {
	if deployLoginProvider != "" {
		return deployLoginProvider, nil
	}
	if _, err := os.Stat(deployConfig); err == nil {
		if cfg, derr := loadDeployConfig(); derr == nil && cfg.Provider != "" {
			return cfg.Provider, nil
		}
	}
	return "", fmt.Errorf("no provider specified; pass --provider (e.g. --provider omnia)")
}

// loginConfigJSON returns the current deploy config as JSON for the adapter to
// read provider coordinates from (e.g. api_endpoint). Best-effort: an absent or
// deploy-less config yields "" and the adapter reports what it still needs.
func loginConfigJSON() string {
	deployCfg, err := loadDeployConfig()
	if err != nil || deployCfg == nil {
		return ""
	}
	j, err := mergedDeployConfigJSON(deployCfg, resolveEnvironment())
	if err != nil {
		return ""
	}
	return j
}

// newLoginState returns a random CSRF state nonce.
func newLoginState() (string, error) {
	b := make([]byte, loginStateBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate state: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// loginCallback carries the captured loopback callback query params.
type loginCallback struct {
	params map[string]string
}

// startLoopbackServer binds a loopback listener and serves the callback. It
// returns the callback URL, a channel that receives the captured params once
// the browser redirects, and a shutdown func.
func startLoopbackServer(
	ctx context.Context, state string,
) (callbackURL string, resultCh <-chan loginCallback, shutdown func(), err error) {
	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, nil, fmt.Errorf("failed to start loopback server: %w", err)
	}
	ch := make(chan loginCallback, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		params := map[string]string{}
		for k, v := range r.URL.Query() {
			if len(v) > 0 {
				params[k] = v[0]
			}
		}
		if params["state"] != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><h3>Login complete.</h3>" +
			"<p>You can close this tab and return to the terminal.</p></body></html>"))
		select {
		case ch <- loginCallback{params: params}:
		default:
		}
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: loopbackReadHeaderTimeout}
	go func() { _ = srv.Serve(listener) }()

	port := listener.Addr().(*net.TCPAddr).Port
	return fmt.Sprintf("http://127.0.0.1:%d/callback", port), ch,
		func() { _ = srv.Close() }, nil
}

func runDeployLogin(_ *cobra.Command, _ []string) error {
	provider, err := resolveLoginProvider()
	if err != nil {
		return err
	}
	projectDir, _ := os.Getwd()
	ctx, cancel := context.WithTimeout(context.Background(), loginTimeout)
	defer cancel()

	client, closeFn, err := connectLoginAdapter(provider, projectDir)
	if err != nil {
		return err
	}
	defer closeFn()

	return runLoginFlow(ctx, client, provider)
}

// runLoginFlow drives the browser login against an adapter client: capability
// check, loopback callback, authorize, complete, and write. Separated from
// runDeployLogin so it can be tested with a fake client and no subprocess.
func runLoginFlow(ctx context.Context, client loginAdapter, provider string) error {
	if err := requireLoginCapability(ctx, client, provider); err != nil {
		return err
	}

	state, err := newLoginState()
	if err != nil {
		return err
	}
	callbackURL, resultCh, shutdown, err := startLoopbackServer(ctx, state)
	if err != nil {
		return err
	}
	defer shutdown()

	configJSON := loginConfigJSON()
	urlResp, err := client.GetLoginURL(ctx, &deploy.LoginURLRequest{
		CallbackURL: callbackURL, State: state, Config: configJSON,
	})
	if err != nil {
		return fmt.Errorf("failed to get login URL: %w", err)
	}

	fmt.Println("Opening your browser to authenticate...")
	fmt.Printf("  If it doesn't open, visit:\n  %s\n", urlResp.AuthorizeURL)
	if openErr := openBrowserFunc(urlResp.AuthorizeURL); openErr != nil {
		fmt.Fprintf(os.Stderr, "warning: could not open browser automatically: %v\n", openErr)
	}

	cb, err := waitForCallback(ctx, resultCh)
	if err != nil {
		return err
	}

	resp, err := client.CompleteLogin(ctx, &deploy.CompleteLoginRequest{Params: cb.params, Config: configJSON})
	if err != nil {
		return fmt.Errorf("failed to complete login: %w", err)
	}

	return writeLoginResult(provider, resp)
}

// requireLoginCapability fails unless the adapter advertises the login capability.
func requireLoginCapability(ctx context.Context, client loginAdapter, provider string) error {
	info, err := client.GetProviderInfo(ctx)
	if err != nil {
		return fmt.Errorf("failed to query adapter: %w", err)
	}
	for _, c := range info.Capabilities {
		if c == deploy.LoginCapability {
			return nil
		}
	}
	return fmt.Errorf("adapter %q does not support login; configure manually — see %s",
		provider, deployConfigureDocsURL)
}

// waitForCallback blocks until the browser redirect arrives or the context expires.
func waitForCallback(ctx context.Context, resultCh <-chan loginCallback) (loginCallback, error) {
	fmt.Println("Waiting for you to finish in the browser...")
	select {
	case cb := <-resultCh:
		return cb, nil
	case <-ctx.Done():
		return loginCallback{}, fmt.Errorf("login timed out waiting for the browser callback")
	}
}

// writeLoginResult merges the returned profile into the config and stores the
// token in the credentials file (never in the config).
func writeLoginResult(provider string, resp *deploy.CompleteLoginResponse) error {
	if _, statErr := os.Stat(deployConfig); os.IsNotExist(statErr) {
		return fmt.Errorf(
			"config file not found: %s\nCreate one first (e.g. 'promptarena init'), then re-run login",
			deployConfig,
		)
	}
	doc, err := os.ReadFile(deployConfig)
	if err != nil {
		return fmt.Errorf("failed to read config %s: %w", deployConfig, err)
	}
	merged, err := mergeProfileIntoConfigDoc(doc, resp.Profile, provider)
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

	if resp.Token != "" {
		cred := deployCredential{Token: resp.Token}
		if v, ok := resp.Profile["api_endpoint"].(string); ok {
			cred.Endpoint = v
		}
		if v, ok := resp.Profile["workspace"].(string); ok {
			cred.Workspace = v
		}
		if err := storeDeployCredential(provider, deployConfig, cred); err != nil {
			return fmt.Errorf("config written, but failed to store token: %w", err)
		}
	}

	fmt.Printf("\nLogin complete. Wrote the deploy profile to %s\n", deployConfig)
	fmt.Println("The token was stored in ~/.promptarena/credentials (not in the config file).")
	return nil
}
