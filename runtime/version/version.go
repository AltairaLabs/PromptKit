// Package version provides version information for the PromptKit runtime.
// Version variables can be overridden at build time using ldflags:
//
//	go build -ldflags "-X github.com/AltairaLabs/PromptKit/runtime/version.version=1.0.0"
package version

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"strings"
)

const (
	// devVersion is the default version when not set via ldflags
	devVersion = "dev"
	// shortCommitLen is the length of the short commit hash
	shortCommitLen = 7
	// vcsRevisionKey is the build info key for git commit
	vcsRevisionKey = "vcs.revision"
	// vcsModifiedKey is the build info key for dirty state
	vcsModifiedKey = "vcs.modified"
)

// Build-time variables - can be overridden with -ldflags
// Uses same naming convention as promptarena for consistency
var (
	version   = devVersion
	gitCommit = ""
	buildDate = ""
)

// GetVersion returns the current version string.
// Falls back to build info from go modules if version is "dev".
func GetVersion() string {
	if version != devVersion {
		return version
	}

	// Try to get version from build info (go modules)
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			return info.Main.Version
		}
	}

	return devVersion
}

// getCommitFromBuildInfo extracts the git commit hash from build info.
func getCommitFromBuildInfo() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}

	for _, setting := range info.Settings {
		if setting.Key == vcsRevisionKey && setting.Value != "" {
			return setting.Value[:min(shortCommitLen, len(setting.Value))]
		}
	}
	return ""
}

// isDirtyFromBuildInfo checks if the build has uncommitted changes.
func isDirtyFromBuildInfo() bool {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return false
	}

	for _, setting := range info.Settings {
		if setting.Key == vcsModifiedKey && setting.Value == "true" {
			return true
		}
	}
	return false
}

// GetVersionInfo returns detailed version information in the same format as promptarena.
func GetVersionInfo() string {
	var b strings.Builder

	v := GetVersion()
	fmt.Fprintf(&b, "PromptKit runtime version %s", v)

	commit := gitCommit
	if commit == "" {
		commit = getCommitFromBuildInfo()
	}

	if commit != "" {
		fmt.Fprintf(&b, "\ncommit: %s", commit)
	}

	if buildDate != "" {
		fmt.Fprintf(&b, "\nbuilt: %s", buildDate)
	}

	return b.String()
}

// GetBuildInfo returns version details as structured slog attributes.
// This is useful for including version info in log messages.
func GetBuildInfo() []any {
	attrs := []any{
		"version", GetVersion(),
	}

	commit := gitCommit
	if commit == "" {
		commit = getCommitFromBuildInfo()
	}

	if commit != "" {
		attrs = append(attrs, "commit", commit)
	}

	if gitCommit == "" && isDirtyFromBuildInfo() {
		attrs = append(attrs, "dirty", true)
	}

	if buildDate != "" {
		attrs = append(attrs, "built", buildDate)
	}

	return attrs
}

// LogStartup logs version information at debug level.
// This is called by the logger package after initialization.
func LogStartup() {
	// Only log at debug level to avoid noise in production
	level := slog.LevelDebug

	// Check if debug logging is enabled
	if envLevel := os.Getenv("LOG_LEVEL"); envLevel != "" {
		switch strings.ToLower(envLevel) {
		case "debug", "trace":
			// Continue to log
		default:
			// Skip logging if not debug/trace
			return
		}
	} else {
		// Default is info, so skip debug logging
		return
	}

	attrs := GetBuildInfo()
	slog.Log(context.Background(), level, "PromptKit runtime starting", attrs...)
}
