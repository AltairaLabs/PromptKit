package main

import (
	"fmt"
	"runtime/debug"
	"strings"
)

// Version information - can be overridden at build time with -ldflags
var (
	version   = "dev"
	gitCommit = ""
	buildDate = ""
)

// GetVersion returns the current version string
func GetVersion() string {
	if version != "dev" {
		return version
	}

	// Try to get version from build info (go modules)
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			return info.Main.Version
		}
	}

	return "dev"
}

// GetVersionInfo returns detailed version information
func GetVersionInfo() string {
	var b strings.Builder

	v := GetVersion()
	b.WriteString(fmt.Sprintf("promptarena version %s", v))

	if gitCommit != "" {
		b.WriteString(fmt.Sprintf("\ncommit: %s", gitCommit))
	}

	if buildDate != "" {
		b.WriteString(fmt.Sprintf("\nbuilt: %s", buildDate))
	}

	return b.String()
}
