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

	"log"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	arenaaudio "github.com/AltairaLabs/PromptKit/tools/arena/audio"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
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
	serveCmd.Flags().String("audio-monitor", string(arenaaudio.ModeAuto),
		"Audio monitoring: auto, on, off")
	serveCmd.Flags().Int("audio-rate", arenaaudio.Rate24k,
		"Audio canonical sample rate: 16000, 24000, or 48000")
	serveCmd.Flags().Bool("mock-provider", false,
		"Replace ALL configured providers with mocks (assistant, self-play user, "+
			"and any others) so no real API calls are made. Useful for free demos. "+
			"Pair with --mock-config to use canned scenario responses.")
	serveCmd.Flags().String("mock-config", "",
		"Path to a mock responses YAML file. Only meaningful with --mock-provider.")
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

	// --mock-provider replaces every provider in the registry with a
	// generic mock, including the self-play user role and any TTS path
	// that resolves through providers. Without this, picking the
	// "mock-duplex" assistant in the UI still leaves self-play and TTS
	// hitting real APIs for $$$ — surprising for a demo.
	mockAll, _ := cmd.Flags().GetBool("mock-provider")
	mockConfig, _ := cmd.Flags().GetString("mock-config")
	if mockAll {
		if mockErr := eng.EnableMockProviderMode(mockConfig); mockErr != nil {
			return fmt.Errorf("failed to enable mock provider mode: %w", mockErr)
		}
		log.Printf("Mock provider mode: ALL providers replaced with mocks (no API calls)")
	}

	// Wire audio monitor
	mode, err := cmd.Flags().GetString("audio-monitor")
	if err != nil {
		return fmt.Errorf("failed to get audio-monitor flag: %w", err)
	}
	rate, err := cmd.Flags().GetInt("audio-rate")
	if err != nil {
		return fmt.Errorf("failed to get audio-rate flag: %w", err)
	}
	if monitorErr := eng.EnableAudioMonitor(arenaaudio.Options{
		Mode:        arenaaudio.MonitorMode(mode),
		Rate:        rate,
		LocalSink:   true,
		SSEPlayback: true,
		LevelMeter:  true,
	}); monitorErr != nil {
		return fmt.Errorf("audio monitor: %w", monitorErr)
	}

	// Wire event bus
	eventBus := events.NewEventBus()
	eng.SetEventBus(eventBus, engine.WithMessageEvents())

	// Create web adapter and subscribe to bus
	adapter := web.NewEventAdapter()
	adapter.Subscribe(eventBus)

	// Get state store for results API
	arenaStore, _ := eng.GetStateStore().(*statestore.ArenaStateStore)

	// Resolve output directory
	outDir := cfg.Defaults.Output.Dir
	if outDir == "" {
		outDir = "out"
	}
	if !filepath.IsAbs(outDir) {
		outDir = filepath.Join(cfg.Defaults.ConfigDir, outDir)
	}

	// Load existing results from the output directory (if any)
	if arenaStore != nil {
		if n := web.LoadResultsIntoStore(outDir, arenaStore); n > 0 {
			log.Printf("Loaded %d existing run result(s) from %s", n, outDir)
		}
	}

	// Create web server
	srv := web.NewServer(adapter, eng, arenaStore, outDir)

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
