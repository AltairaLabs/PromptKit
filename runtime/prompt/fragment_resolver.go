package prompt

import (
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/template"
)

// FragmentRepository interface for loading fragments (to avoid import cycles)
type FragmentRepository interface {
	LoadFragment(name string, relativePath string, baseDir string) (*Fragment, error)
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
// Uses name as cache key, or path if provided.
func (fr *FragmentResolver) LoadFragment(name, relativePath, configFilePath string) (*Fragment, error) {
	if fr.repository == nil {
		return nil, fmt.Errorf("fragment resolver requires repository")
	}

	// Determine cache key (prefer path if provided)
	cacheKey := name
	if relativePath != "" {
		cacheKey = relativePath
	}

	// Check cache first
	if cached, ok := fr.fragmentCache[cacheKey]; ok {
		return cached, nil
	}

	// Load from repository
	fragment, err := fr.repository.LoadFragment(name, relativePath, configFilePath)
	if err != nil {
		return nil, err
	}

	// Cache the fragment
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
