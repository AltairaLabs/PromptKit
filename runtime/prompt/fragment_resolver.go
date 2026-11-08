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
		resolvedName, resolvedPath, err := fr.resolveRef(fragRef, vars)
		if err != nil {
			return nil, err
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

// resolveRef fills a reference's name and path from vars (e.g.
// "persona_support_{{region}}" → "persona_support_us"). A variable's value
// chooses a fragment, never a file elsewhere, so an unsafe one is an error.
func (fr *FragmentResolver) resolveRef(ref FragmentRef, vars map[string]string) (name, path string, err error) {
	if err := checkFragmentRefValues(ref, vars); err != nil {
		return "", "", err
	}
	name = fr.resolveVariables(ref.Name, vars)
	if ref.Path != "" {
		path = fr.resolveVariables(ref.Path, vars)
	}
	return name, path, nil
}

// ErrUnsafeFragmentValue reports that a variable's value, filled into a
// fragment's name or path, would leave the fragment's directory: it holds a
// path separator or "..", or is an absolute path. The fragment is not loaded.
var ErrUnsafeFragmentValue = errors.New("variable value cannot be used in a fragment name or path")

// checkFragmentRefValues rejects a value that ref's name or path would take
// from vars and that is not a plain name: one with a path separator, "..",
// or an absolute path. A path written literally in the config is not checked
// here; only what a variable puts into it.
func checkFragmentRefValues(ref FragmentRef, vars map[string]string) error {
	for key, value := range vars {
		placeholder := "{{" + key + "}}"
		if !strings.Contains(ref.Name, placeholder) && !strings.Contains(ref.Path, placeholder) {
			continue
		}
		if strings.ContainsAny(value, `/\`) || strings.Contains(value, "..") || filepath.IsAbs(value) {
			return fmt.Errorf("%w: fragment %q, variable %q", ErrUnsafeFragmentValue, ref.Name, key)
		}
	}
	return nil
}

// GetUsedVars returns a list of variable names that had non-empty values
//
// Deprecated: Use template.GetUsedVars instead
func GetUsedVars(vars map[string]string) []string {
	return template.GetUsedVars(vars)
}
