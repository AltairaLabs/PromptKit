package prompt

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/template"
)

// FragmentRepository interface for loading fragments (to avoid import cycles)
type FragmentRepository interface {
	LoadFragment(name, relativePath, baseDir string) (*Fragment, error)
}

// FragmentResolver handles fragment loading, resolution, and variable substitution using the repository pattern
type FragmentResolver struct {
	repository    FragmentRepository   // Required repository for loading fragments
	fragmentCache map[string]*Fragment // Cache for loaded fragments
}

// NewFragmentResolverWithRepository creates a new fragment resolver with a repository
func NewFragmentResolverWithRepository(repository FragmentRepository) *FragmentResolver {
	return &FragmentResolver{
		repository:    repository,
		fragmentCache: make(map[string]*Fragment),
	}
}

// AssembleFragments loads and assembles prompt fragments into variables.
// Resolves dynamic names and paths using the provided variable map.
func (fr *FragmentResolver) AssembleFragments(
	fragments []FragmentRef,
	vars map[string]string,
	configFilePath string,
) (map[string]string, error) {
	fragmentVars := make(map[string]string, len(fragments))

	for _, fragRef := range fragments {
		// Resolve dynamic fragment names (e.g., "persona_support_{{region}}" → "persona_support_us")
		resolvedName := fr.resolveVariables(fragRef.Name, vars)

		// Resolve dynamic paths if specified
		resolvedPath := ""
		if fragRef.Path != "" {
			resolvedPath = fr.resolveVariables(fragRef.Path, vars)
		}

		fragment, err := fr.LoadFragment(resolvedName, resolvedPath, configFilePath)
		if err != nil {
			if fragRef.Required {
				return nil, fmt.Errorf("required fragment '%s' (resolved from '%s') not found: %w",
					resolvedName, fragRef.Name, err)
			}
			// Optional fragment missing - skip silently
			continue
		}

		// Use resolved fragment name as variable key for template substitution
		fragmentVars[resolvedName] = fragment.Content
	}

	return fragmentVars, nil
}

// LoadFragment loads a fragment from the repository with caching.
//
// A reference without a path is loaded and cached by name. A path is resolved
// as follows:
//   - an absolute path names one file and is loaded as it is, with no base dir;
//   - a relative path is loaded against baseDir first and, when no file exists
//     there, against the repository's own default base (an empty base dir: for
//     the file repositories, the process's working directory). Any other
//     failure under baseDir is returned as it is.
func (fr *FragmentResolver) LoadFragment(name, relativePath, baseDir string) (*Fragment, error) {
	if fr.repository == nil {
		return nil, fmt.Errorf("fragment resolver requires repository")
	}
	if relativePath == "" {
		return fr.loadCached(name, name, "", "")
	}
	if filepath.IsAbs(relativePath) {
		return fr.loadCached(filepath.Clean(relativePath), name, relativePath, "")
	}
	// A path is relative to its base dir, so the same path under two base dirs
	// is two fragments.
	cacheKey := filepath.Join(baseDir, relativePath)
	fragment, err := fr.loadCached(cacheKey, name, relativePath, baseDir)
	if err == nil || baseDir == "" || !errors.Is(err, fs.ErrNotExist) {
		return fragment, err
	}
	fragment, fallbackErr := fr.loadCached(filepath.Clean(relativePath), name, relativePath, "")
	if fallbackErr != nil {
		return nil, err
	}
	fr.fragmentCache[cacheKey] = fragment
	return fragment, nil
}

// loadCached returns the fragment cached under cacheKey, loading and caching
// it from the repository on a miss.
func (fr *FragmentResolver) loadCached(cacheKey, name, relativePath, baseDir string) (*Fragment, error) {
	if cached, ok := fr.fragmentCache[cacheKey]; ok {
		return cached, nil
	}
	fragment, err := fr.repository.LoadFragment(name, relativePath, baseDir)
	if err != nil {
		return nil, err
	}
	fr.fragmentCache[cacheKey] = fragment
	return fragment, nil
}

// resolveVariables resolves {{variable}} placeholders in strings
func (fr *FragmentResolver) resolveVariables(text string, vars map[string]string) string {
	result := text
	for key, value := range vars {
		placeholder := fmt.Sprintf("{{%s}}", key)
		result = strings.ReplaceAll(result, placeholder, value)
	}
	return result
}

// GetUsedVars returns a list of variable names that had non-empty values
//
// Deprecated: Use template.GetUsedVars instead
func GetUsedVars(vars map[string]string) []string {
	return template.GetUsedVars(vars)
}
