package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

const (
	defaultServePort = 8080 // Default HTTP port for serve command
)

var serveCmd = &cobra.Command{
	Use:   "serve [directory]",
	Short: "Serve the output directory via HTTP for viewing reports with audio playback",
	Long: `Starts a local HTTP server to serve the output directory.
This enables audio playback in HTML reports, which is blocked when opening
files directly from the filesystem due to browser security restrictions.

Examples:
  promptarena serve              # Serve current directory on port 8080
  promptarena serve out          # Serve the 'out' directory
  promptarena serve -p 3000 out  # Serve on port 3000
  promptarena serve --open out   # Serve and open browser automatically`,
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
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}

	// Resolve to absolute path
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	// Check directory exists
	info, err := os.Stat(absDir)
	if err != nil {
		return fmt.Errorf("directory not found: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", absDir)
	}

	// Find an available port if the default is in use
	//nolint:noctx // Dev tool - context not needed for port check
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", servePort))
	if err != nil {
		return fmt.Errorf("port %d is in use, try a different port with -p", servePort)
	}
	actualPort := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	// Determine the report URL
	reportPath := filepath.Join(absDir, "report.html")
	reportURL := fmt.Sprintf("http://localhost:%d/report.html", actualPort)
	if _, err := os.Stat(reportPath); os.IsNotExist(err) {
		reportURL = fmt.Sprintf("http://localhost:%d/", actualPort)
	}

	fmt.Printf("Serving %s on http://localhost:%d\n", absDir, actualPort)
	fmt.Printf("Report URL: %s\n", reportURL)
	fmt.Println("Press Ctrl+C to stop")

	// Open browser if requested
	if serveOpen {
		go openBrowser(reportURL)
	}

	// Create file server
	fs := http.FileServer(http.Dir(absDir))
	http.Handle("/", fs)

	// Start server
	//nolint:gosec // G114: Dev tool - timeouts not needed for local server
	return http.ListenAndServe(fmt.Sprintf(":%d", actualPort), nil)
}

// openBrowser opens the default browser to the given URL
//
//nolint:noctx // Dev tool - context not needed for opening browser
func openBrowser(url string) {
	var cmd *exec.Cmd
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
