package prompt

import (
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// validateSemanticVersion validates that a version string follows Semantic Versioning 2.0.0.
// It accepts versions with or without the 'v' prefix and requires MAJOR.MINOR.PATCH format.
//
// Valid examples:
//   - "1.0.0"
//   - "v2.1.3"
//   - "1.0.0-alpha"
//   - "1.0.0+build"
//
// Invalid examples:
//   - "1.0" (missing patch)
//   - "v1" (missing minor and patch)
//   - "latest" (not a version number)
//   - "" (empty)
func validateSemanticVersion(version string) error {
	if version == "" {
		return fmt.Errorf("version is empty")
	}

	// Strip 'v' prefix if present (e.g., "v1.0.0" -> "1.0.0")
	cleanVersion := strings.TrimPrefix(version, "v")

	// Use StrictNewVersion to require MAJOR.MINOR.PATCH format
	// (NewVersion would auto-complete "1.0" to "1.0.0")
	_, err := semver.StrictNewVersion(cleanVersion)
	if err != nil {
		return fmt.Errorf("invalid semantic version: %w", err)
	}

	return nil
}
