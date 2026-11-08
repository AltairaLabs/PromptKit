package prompt

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/template"
)

// FragmentRepository interface for loading fragments (to avoid import cycles)
type FragmentRepository interface {
	LoadFragment(name, relativePath, baseDir string) (*Fragment, error)
}

// FragmentResolver handles fragment loading, resolution, and variable substitution using the repository pattern.
//
// It is safe for concurrent use: one Registry, and so one resolver, serves
// every conversation built on it, and a fragment chosen by a variable is first
// loaded, and cached, by whichever turn asks for it first.
type FragmentResolver struct {
	repository    FragmentRepository   // Required repository for loading fragments
	mu            sync.RWMutex         // guards fragmentCache
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
			// An optional fragment that does not exist renders empty, so its
			// placeholder does not fail the render. A variable of the same
			// name, when the caller set one, still fills it. Any other
			// failure (a file that won't parse, a path the repository can't
			// load) is not "missing": its placeholder is left, so the render
			// fails naming it rather than dropping it silently.
			if !errors.Is(err, fs.ErrNotExist) {
				logger.Warn("Optional fragment could not be loaded", "fragment", resolvedName, "error", err)
				continue
			}
			if _, set := vars[resolvedName]; !set {
				fragmentVars[resolvedName] = ""
			}
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
		if errors.Is(fallbackErr, fs.ErrNotExist) {
			return nil, err
		}
		// Found under the default base but unusable: that is the real error.
		return nil, fallbackErr
	}
	fr.store(cacheKey, fragment)
	return fragment, nil
}

// loadCached returns the fragment cached under cacheKey, loading and caching
// it from the repository on a miss.
// The repository is called outside the lock, since it may do I/O; two turns
// that miss at once both load, and the first to store wins.
func (fr *FragmentResolver) loadCached(cacheKey, name, relativePath, baseDir string) (*Fragment, error) {
	fr.mu.RLock()
	cached, ok := fr.fragmentCache[cacheKey]
	fr.mu.RUnlock()
	if ok {
		return cached, nil
	}
	fragment, err := fr.repository.LoadFragment(name, relativePath, baseDir)
	if err != nil {
		return nil, err
	}
	return fr.store(cacheKey, fragment), nil
}

// store caches fragment under cacheKey, unless another load cached one there
// first, and returns the cached fragment.
func (fr *FragmentResolver) store(cacheKey string, fragment *Fragment) *Fragment {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	if cached, ok := fr.fragmentCache[cacheKey]; ok {
		return cached
	}
	fr.fragmentCache[cacheKey] = fragment
	return fragment
}

// cachedKeys returns the cache keys of the loaded fragments, sorted.
func (fr *FragmentResolver) cachedKeys() []string {
	fr.mu.RLock()
	defer fr.mu.RUnlock()
	keys := make([]string, 0, len(fr.fragmentCache))
	for k := range fr.fragmentCache {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// clearCache drops every loaded fragment.
func (fr *FragmentResolver) clearCache() {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	fr.fragmentCache = make(map[string]*Fragment)
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
