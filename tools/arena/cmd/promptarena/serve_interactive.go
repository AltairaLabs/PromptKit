package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"context"
	"log"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	runtimestore "github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	jsonresults "github.com/AltairaLabs/PromptKit/tools/arena/results/json"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/web"
)

const (
	serverReadTimeout = 30 * time.Second
	defaultServePort  = 8080
)

// serveAddr returns the listen address for the given port, binding to localhost only.
func serveAddr(port int) string {
	return fmt.Sprintf("127.0.0.1:%d", port)
}

var serveCmd = &cobra.Command{
	Use:   "serve [config-path]",
	Short: "Start the Arena web UI with live run streaming",
	Long: `Starts a local web server with the Arena UI. Streams run events
to the browser via SSE and provides a REST API for starting runs
and viewing results.

If config-path is a directory, looks for config.arena.yaml inside it.

Examples:
  promptarena serve                    # Load config.arena.yaml from current dir
  promptarena serve ./my-scenario      # Load from specific directory
  promptarena serve -p 3000            # Serve on port 3000
  promptarena serve --open             # Open browser automatically`,
	Args: cobra.MaximumNArgs(1),
	RunE: runServe,
}

var (
	servePort int
	serveOpen bool
)

func init() {
	serveCmd.Flags().IntVarP(&servePort, "port", "p", defaultServePort, "Port to serve on")
	serveCmd.Flags().BoolVarP(&serveOpen, "open", "o", false, "Open browser automatically")
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	configPath := "."
	if len(args) > 0 {
		configPath = args[0]
	}

	// Resolve config file path
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}
	if info, statErr := os.Stat(absPath); statErr == nil && info.IsDir() {
		absPath = filepath.Join(absPath, "config.arena.yaml")
	}
	if _, statErr := os.Stat(absPath); statErr != nil {
		return fmt.Errorf("config not found: %s", absPath)
	}

	// Load config and create engine
	cfg, err := config.LoadConfig(absPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	eng, err := engine.NewEngineFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create engine: %w", err)
	}

	// Wire event bus
	eventBus := events.NewEventBus()
	eng.SetEventBus(eventBus, engine.WithMessageEvents())

	// Create web adapter and subscribe to bus
	adapter := web.NewEventAdapter()
	adapter.Subscribe(eventBus)

	// Get state store for results API
	arenaStore, _ := eng.GetStateStore().(*statestore.ArenaStateStore)

	// Load existing results from the output directory (if any)
	if arenaStore != nil {
		loadExistingResults(cfg, arenaStore)
	}

	// Create web server
	srv := web.NewServer(adapter, eng, arenaStore)

	// Check port availability
	//nolint:noctx // Dev tool - context not needed for port check
	listener, err := net.Listen("tcp", serveAddr(servePort))
	if err != nil {
		return fmt.Errorf("port %d is in use, try a different port with -p", servePort)
	}
	actualPort := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	url := fmt.Sprintf("http://localhost:%d", actualPort)
	fmt.Printf("Arena Web UI: %s\n", url)
	fmt.Printf("Config: %s\n", absPath)
	fmt.Println("Press Ctrl+C to stop")

	if serveOpen {
		go openBrowser(url)
	}

	// NOSONAR: TLS not required - local development tool, binds to localhost only
	httpServer := &http.Server{
		Addr:        serveAddr(actualPort),
		Handler:     srv.Handler(),
		ReadTimeout: serverReadTimeout,
		// WriteTimeout intentionally omitted: SSE requires long-lived connections;
		// a non-zero write timeout would kill active SSE streams.
	}
	return httpServer.ListenAndServe()
}

// loadExistingResults loads previously completed run results from the output directory
// into the state store so they're available via the REST API on startup.
func loadExistingResults(cfg *config.Config, store *statestore.ArenaStateStore) {
	outDir := cfg.Defaults.Output.Dir
	if outDir == "" {
		outDir = "out"
	}
	// Resolve relative to config directory
	if !filepath.IsAbs(outDir) {
		outDir = filepath.Join(cfg.Defaults.ConfigDir, outDir)
	}

	repo := jsonresults.NewJSONResultRepository(outDir)
	results, err := repo.LoadResults()
	if err != nil {
		// No results to load — not an error, just nothing to show
		return
	}

	ctx := context.Background()
	loaded := 0
	for i := range results {
		r := &results[i]
		// Save conversation state (messages)
		convState := &runtimestore.ConversationState{
			ID:       r.RunID,
			Messages: r.Messages,
			Metadata: make(map[string]interface{}),
		}
		if saveErr := store.Save(ctx, convState); saveErr != nil {
			continue
		}
		// Save run metadata
		meta := &statestore.RunMetadata{
			RunID:                        r.RunID,
			PromptPack:                   r.PromptPack,
			Region:                       r.Region,
			ScenarioID:                   r.ScenarioID,
			ProviderID:                   r.ProviderID,
			Params:                       r.Params,
			Commit:                       r.Commit,
			StartTime:                    r.StartTime,
			EndTime:                      r.EndTime,
			Duration:                     r.Duration,
			Error:                        r.Error,
			SelfPlay:                     r.SelfPlay,
			PersonaID:                    r.PersonaID,
			ConversationAssertionResults: r.ConversationAssertions.Results,
		}
		if saveErr := store.SaveMetadata(ctx, r.RunID, meta); saveErr != nil {
			continue
		}
		loaded++
	}
	if loaded > 0 {
		log.Printf("Loaded %d existing run result(s) from %s", loaded, outDir)
	}
}

// openBrowser opens the default browser to the given URL.
//
//nolint:noctx // Dev tool - context not needed for opening browser
func openBrowser(url string) {
	var cmd *exec.Cmd
	// NOSONAR: Command injection safe - url is internally generated (localhost URL), not user input
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		fmt.Printf("Open %s in your browser\n", url)
		return
	}
	_ = cmd.Run()
}
