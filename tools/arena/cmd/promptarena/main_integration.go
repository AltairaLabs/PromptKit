package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/app"
)

var rootCmd = &cobra.Command{
	Use:           "promptarena",
	Short:         "PromptKit Arena - Multi-turn conversation simulation and testing tool",
	Version:       GetVersion(),
	SilenceUsage:  true,  // Don't print usage on error
	SilenceErrors: false, // Do print errors
	Long: `PromptKit Arena is a testing framework for running multi-turn conversation
simulations across multiple LLM providers and system prompts.

It evaluates tone consistency, content quality, prompt adherence, and validates
conversation flows for various task types (customer support, code assistance, etc.).`,
	// RunE fires only when no sub-command is provided (bare `promptarena`).
	// Sub-commands registered via rootCmd.AddCommand() take precedence and
	// RunE is NOT called for them, so existing commands are unaffected.
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("could not determine working directory: %w", err)
		}

		ctx := &app.AppContext{
			Version:    GetVersion(),
			ResultsDir: filepath.Join(cwd, "out"),
		}

		if cfgPath, found := app.DiscoverConfig(cwd); found {
			if loadErr := ctx.LoadConfig(cfgPath); loadErr != nil {
				// Non-fatal: launch hub without a config rather than aborting.
				fmt.Fprintf(os.Stderr, "warning: could not load config %s: %v\n", cfgPath, loadErr)
			} else {
				// LoadConfig sets ResultsDir relative to the config file;
				// override only if it is still empty (defensive fallback — LoadConfig
				// always sets it in practice, but guard against future refactors).
				if ctx.ResultsDir == "" {
					ctx.ResultsDir = filepath.Join(cwd, "out")
				}
			}
		}

		return app.Run(ctx, app.NewHome(ctx, app.DefaultMenu(ctx)))
	},
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Initialize logger based on verbose flag if present
		// This runs before all subcommands
		if cmd.Flags().Changed("verbose") {
			verbose, err := cmd.Flags().GetBool("verbose")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error getting verbose flag: %v\n", err)
				return
			}
			logger.SetVerbose(verbose)
		}
	},
}

// setupVersion configures the version display
func setupVersion() {
	// Set custom version template to show detailed version info
	rootCmd.SetVersionTemplate(GetVersionInfo() + "\n")
}

func Execute() {
	setupVersion()
	err := rootCmd.Execute()
	if err != nil {
		// Error already printed by cobra
		os.Exit(1)
	}
}

// installCtrlCEscapeHatch is a last-resort guard against a wedged TUI
// (e.g. Bubble Tea blocked on a stuck pipeline goroutine) leaving the
// terminal unusable. It does NOT replace whatever signal handling the
// TUI / cobra commands already have — it sits behind them as a safety
// net.
//
// Behavior:
//   - First Ctrl-C: print a hint and start a 5-second watchdog. The
//     existing handlers (cobra context cancel, Bubble Tea quit) are
//     still notified by the runtime — signal.Notify does not consume
//     the signal, it only adds us as another listener — so they keep
//     trying to shut down cleanly.
//   - Second Ctrl-C OR 5s without exit: restore the terminal (cursor,
//     primary buffer, attributes) and force-exit 130.
//
// The terminal restore sequences cover the 99% case of a Bubble Tea
// program that left the alternate screen + cursor hidden + raw mode.
// If the terminal is hosed worse than that, the user can run `reset`
// or `stty sane` afterwards.
const (
	ctrlCWatchdogTimeout = 5 * time.Second
	// sigChanBuf is the buffer depth for the SIGINT/SIGTERM channel — large
	// enough to hold a duplicate "second Ctrl-C" arriving before the
	// goroutine drains the first.
	sigChanBuf = 2
	// exitCodeSIGINT is the standard "terminated by SIGINT" exit code (128 + 2).
	exitCodeSIGINT = 130
)

func installCtrlCEscapeHatch() {
	sigCh := make(chan os.Signal, sigChanBuf)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh // first signal
		fmt.Fprintln(os.Stderr,
			"\n[promptarena] shutdown requested. Press Ctrl-C again or wait 5s to force-exit.")
		select {
		case <-sigCh:
			fmt.Fprintln(os.Stderr, "[promptarena] force-exiting (second signal received)")
		case <-time.After(ctrlCWatchdogTimeout):
			fmt.Fprintln(os.Stderr,
				"[promptarena] force-exiting (graceful shutdown did not complete within 5s)")
		}
		// Best-effort terminal restore: show cursor, leave alt screen,
		// reset attributes, leave bracketed paste mode. These are the
		// usual things a TUI puts the terminal into.
		_, _ = os.Stderr.WriteString("\x1b[?25h\x1b[?1049l\x1b[?2004l\x1b[0m")
		os.Exit(exitCodeSIGINT)
	}()
}

func main() {
	installCtrlCEscapeHatch()
	Execute()
}
