package prompt

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
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
		if err := fr.assembleOne(fragRef, vars, configFilePath, fragmentVars); err != nil {
			return nil, err
		}
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
//
// It is one pass over text: a value is never scanned again, so a value that
// holds a placeholder is not expanded by a later variable. A placeholder with
// no variable is left as it is.
func (fr *FragmentResolver) resolveVariables(text string, vars map[string]string) string {
	return substitutePlaceholders(text, vars)
}

// substitutePlaceholders replaces each {{name}} in text with its value from
// vars, in one pass; see resolveVariables.
func substitutePlaceholders(text string, vars map[string]string) string {
	return fragmentRefPlaceholder.ReplaceAllStringFunc(text, func(m string) string {
		if value, ok := vars[m[2:len(m)-2]]; ok {
			return value
		}
		return m
	})
}

// fragmentRefPlaceholder matches a {{name}} placeholder in a fragment
// reference's name or path, exactly as written ({{name}}, no spaces).
var fragmentRefPlaceholder = regexp.MustCompile(`\{\{[^{}]+\}\}`)

// assembleOne loads one reference into fragmentVars under its resolved name.
//
// A path refused as unsafe fails the prompt, required or not. An optional
// fragment that does not exist (including a path its variables leave empty)
// renders empty, so its placeholder does not fail the render; a variable of
// the same name, when the caller set one, still fills it. Any other failure
// of an optional fragment (a file that won't parse, a path the repository
// can't load) is not "missing": its placeholder is left, so the render fails
// naming it rather than dropping it silently.
func (fr *FragmentResolver) assembleOne(
	ref FragmentRef, vars map[string]string, baseDir string, fragmentVars map[string]string,
) error {
	name, fragment, err := fr.loadRef(ref, vars, baseDir)
	var unsafe *unsafeRefError
	switch {
	case err == nil:
		fragmentVars[name] = fragment.Content
	case errors.As(err, &unsafe):
		return unsafe.err
	case ref.Required:
		return fmt.Errorf("required fragment '%s' (resolved from '%s') not found: %w", name, ref.Name, err)
	case !errors.Is(err, fs.ErrNotExist):
		logger.Warn("Optional fragment could not be loaded", "fragment", name, "error", err)
	default:
		if _, set := vars[name]; !set {
			fragmentVars[name] = ""
		}
	}
	return nil
}

// loadRef resolves ref and loads the fragment it names. An unsafe path is
// returned as an error that does not satisfy fs.ErrNotExist, so it fails
// even an optional fragment.
func (fr *FragmentResolver) loadRef(
	ref FragmentRef, vars map[string]string, baseDir string,
) (string, *Fragment, error) {
	name, path, err := fr.resolveRef(ref, vars)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return name, nil, &unsafeRefError{err}
		}
		return name, nil, err
	}
	fragment, err := fr.LoadFragment(name, path, baseDir)
	return name, fragment, err
}

// unsafeRefError marks a reference refused before any load, so the caller
// fails on it whether the fragment is required or not.
type unsafeRefError struct{ err error }

func (e *unsafeRefError) Error() string { return e.err.Error() }
func (e *unsafeRefError) Unwrap() error { return e.err }

// resolveRef fills a reference's name and path from vars (e.g.
// "persona_support_{{region}}" → "persona_support_us").
//
// The name is not checked: it is a key, which a repository that reads files
// keeps inside its fragments directory and one that reads none (a pack's
// fragments) uses as it is. The path is checked segment by segment, see
// resolveFragmentPath.
func (fr *FragmentResolver) resolveRef(ref FragmentRef, vars map[string]string) (name, path string, err error) {
	name = fr.resolveVariables(ref.Name, vars)
	if ref.Path == "" {
		return name, "", nil
	}
	path, err = fr.resolveFragmentPath(ref, vars)
	return name, path, err
}

// ErrUnsafeFragmentValue reports that variables, filled into a fragment's
// path, would make it climb or leave its directory: a value holding a path
// separator, "..", a drive colon or a placeholder, or values that together
// make a ".." segment. The fragment is not loaded.
var ErrUnsafeFragmentValue = errors.New("variable value cannot be used in a fragment path")

// errUnresolvableFragmentPath reports a path segment that empty variables
// leave empty ("{{tone}}/x.md" with tone ""): it names no file. It satisfies
// fs.ErrNotExist, so an optional fragment renders empty, as a missing one
// does, and a required one fails as not found.
var errUnresolvableFragmentPath = fmt.Errorf("fragment path segment is empty after its variables: %w", fs.ErrNotExist)

// resolveFragmentPath fills ref's path from vars; see fillFragmentPath.
func (fr *FragmentResolver) resolveFragmentPath(ref FragmentRef, vars map[string]string) (string, error) {
	return fillFragmentPath(ref, vars)
}

// fillFragmentPath fills ref's path from vars, one segment at a time, split
// on "/" and "\\" alike (both separate on Windows). What the config writes
// literally is the author's and is left as it is. In a segment a variable
// fills, every non-empty value must be a plain name (IsPlainFragmentValue),
// and the filled segment must not be made of dots and spaces only (".." from
// two "." values side by side; Windows also drops trailing dots and spaces)
// nor be empty.
func fillFragmentPath(ref FragmentRef, vars map[string]string) (string, error) {
	segments := strings.Split(strings.ReplaceAll(ref.Path, "\\", "/"), "/")
	for i, seg := range segments {
		if !fragmentRefPlaceholder.MatchString(seg) {
			continue
		}
		for _, m := range fragmentRefPlaceholder.FindAllString(seg, -1) {
			key := m[2 : len(m)-2]
			if value, ok := vars[key]; ok && value != "" && !IsPlainFragmentValue(value) {
				return "", fmt.Errorf("%w: fragment %q, variable %q", ErrUnsafeFragmentValue, ref.Name, key)
			}
		}
		filled := substitutePlaceholders(seg, vars)
		if filled == "" {
			return "", fmt.Errorf("fragment %q, path %q: %w", ref.Name, ref.Path, errUnresolvableFragmentPath)
		}
		if strings.Trim(filled, ". ") == "" {
			return "", fmt.Errorf("%w: fragment %q, path %q", ErrUnsafeFragmentValue, ref.Name, ref.Path)
		}
		segments[i] = filled
	}
	return strings.Join(segments, "/"), nil
}

// IsPlainFragmentValue reports whether value may fill a fragment's name or
// path: non-empty, with no path separator, "..", drive colon or placeholder,
// and not an absolute path.
func IsPlainFragmentValue(value string) bool {
	return value != "" && !strings.ContainsAny(value, `/\:{}`) && !strings.Contains(value, "..") &&
		!filepath.IsAbs(value)
}

// GetUsedVars returns a list of variable names that had non-empty values
//
// Deprecated: Use template.GetUsedVars instead
func GetUsedVars(vars map[string]string) []string {
	return template.GetUsedVars(vars)
}
