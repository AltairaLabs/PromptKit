package sdk

import (
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// validateSemanticVersion validates that a version string follows semantic versioning 2.0.0.
// It accepts versions with or without a 'v' prefix (e.g., "1.0.0" or "v1.0.0").
// Returns an error if the version is not a valid semantic version.
func validateSemanticVersion(version string) error {
	if version == "" {
		return fmt.Errorf("version cannot be empty")
	}

	// Strip 'v' prefix if present to support both "1.0.0" and "v1.0.0"
	version = strings.TrimPrefix(version, "v")

	// Use StrictNewVersion to enforce MAJOR.MINOR.PATCH format
	// (NewVersion would auto-complete "1.0" to "1.0.0", which we don't want)
	_, err := semver.StrictNewVersion(version)
	if err != nil {
		return fmt.Errorf("invalid semantic version: %w", err)
	}

	return nil
}
