package common

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/persistence"
)

// FragmentFilePath returns the file a fragment lookup reads, for a repository
// over basePath:
//
//   - an absolute path is used as it is, since a config wrote it;
//   - a relative path resolves against baseDir (the working directory when
//     baseDir is empty) and may climb only as far as its own leading "../"
//     segments go. Those are the config's: "../fragments/x.yaml" from a
//     prompts directory reaches its sibling fragments directory. A ".." after
//     a name, which climbs back past where the path started, is refused with
//     persistence.ErrFragmentPathOutsideBase. For the working-directory
//     fallback the error also satisfies fs.ErrNotExist (nothing it may read
//     is there). FragmentResolver keeps a variable from adding "..", so a
//     variable cannot move this point;
//   - no path is the default file, basePath/fragments/<name><ext>, which
//     must stay in basePath/fragments.
//
// Containment holds on the resolved file too: a symlink that points outside
// the point the path may reach is refused. This is a check on the files as
// they are now; ReadFragmentFile holds the same rules on the read itself.
func FragmentFilePath(basePath, name, relativePath, baseDir, ext string) (string, error) {
	path, anchor, err := lexicalFragmentPath(basePath, name, relativePath, baseDir, ext)
	if err != nil || filepath.IsAbs(relativePath) {
		return path, err
	}
	ref := relativePath
	if ref == "" {
		ref = name
	}
	return containResolved(path, ref, anchor)
}

// lexicalFragmentPath applies FragmentFilePath's rules to the path as
// written, without touching the filesystem. It returns the file and the
// anchor: the directory the lookup may reach (the climb anchor for a
// relative path, basePath for the default file, none for an absolute path).
func lexicalFragmentPath(basePath, name, relativePath, baseDir, ext string) (path, anchor string, err error) {
	switch {
	case filepath.IsAbs(relativePath):
		return relativePath, "", nil
	case relativePath == "":
		path = filepath.Join(basePath, "fragments", name+ext)
		if !within(filepath.Join(basePath, "fragments"), path) {
			return "", "", fmt.Errorf("%w: fragment name %q", persistence.ErrFragmentPathOutsideBase, name)
		}
		return path, basePath, nil
	}
	path = filepath.Join(baseDir, relativePath)
	anchor = climbAnchor(baseDir, relativePath)
	if !within(anchor, path) {
		err = fmt.Errorf("%w: %q climbs past %q", persistence.ErrFragmentPathOutsideBase, relativePath, anchor)
		if baseDir == "" {
			// The working-directory fallback FragmentResolver tries after a
			// miss: report it as not found, so the resolver reports that miss.
			err = fmt.Errorf("%w: %w", err, fs.ErrNotExist)
		}
		return "", "", err
	}
	return path, anchor, nil
}

// climbAnchor returns the directory a relative path may reach: baseDir (the
// working directory when empty) raised by the path's leading ".." segments.
func climbAnchor(baseDir, relativePath string) string {
	anchor := baseDir
	if anchor == "" {
		anchor = "."
	}
	for _, seg := range strings.FieldsFunc(relativePath, isPathSeparator) {
		switch seg {
		case "..":
			anchor = filepath.Join(anchor, "..")
		case ".":
		default:
			return anchor
		}
	}
	return anchor
}

func isPathSeparator(r rune) bool { return r == '/' || r == filepath.Separator }

// containResolved checks path once its symlinks are resolved: a link inside a
// root that points outside every root is refused, with
// persistence.ErrFragmentPathOutsideBase. A path that does not exist (or a
// link to nothing) is returned as it is, so reading it reports
// fs.ErrNotExist. The lexical check has already passed.
func containResolved(path, ref string, roots ...string) (string, error) {
	target, err := resolvedAbs(path)
	if err != nil {
		// Missing, or not resolvable: nothing outside is read through it, and
		// the read reports why.
		return path, nil //nolint:nilerr // the read reports the error
	}
	for _, root := range roots {
		resolvedRoot, rootErr := resolvedAbs(root)
		if rootErr != nil {
			continue
		}
		if within(resolvedRoot, target) {
			return path, nil
		}
	}
	return "", fmt.Errorf("%w: %q resolves through a symlink to %q", persistence.ErrFragmentPathOutsideBase, ref, target)
}

// resolvedAbs returns p made absolute, then with its symlinks resolved, so a
// relative root ("." or a relative base) and an absolute link target compare
// in the same, physical, form.
func resolvedAbs(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// within reports whether path is root or under it, comparing absolute forms.
func within(root, path string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// beforeRootedRead, when set (by tests), runs inside a read once the lookup
// has checked the path and opened the anchor, before it reads, so a test can
// change the files in that window.
var beforeRootedRead func()

// ReadFragmentFile reads the fragment file a lookup names (see
// FragmentFilePath for the rules) and returns its content and path.
//
// A relative or default path is read through an os.Root opened at the
// anchor, the point the path may reach (the climb anchor, or basePath). The
// anchor is resolved once, when the root is opened: symlinks in it or in its
// parent directories are followed then, so a pack directory that is a link to
// elsewhere is read where it really is. Every read of the lookup goes through
// that root, so nothing changed afterwards moves it: a fragment link swapped
// to point outside, or a parent directory swapped for a link, is not
// followed. A path or link that leaves the anchor is
// persistence.ErrFragmentPathOutsideBase, which also satisfies fs.ErrNotExist
// for the working-directory fallback. A missing file is fs.ErrNotExist. An
// absolute path, which the config wrote, is read as it is.
func ReadFragmentFile(basePath, name, relativePath, baseDir, ext string) ([]byte, string, error) {
	path, anchor, err := lexicalFragmentPath(basePath, name, relativePath, baseDir, ext)
	if err != nil {
		return nil, "", err
	}
	if filepath.IsAbs(relativePath) {
		data, readErr := os.ReadFile(path) //nolint:gosec // G304: an absolute path the config wrote
		return data, path, readErr
	}
	data, err := readInAnchor(anchor, path)
	if errors.Is(err, errPathEscapesRoot) {
		ref := relativePath
		if ref == "" {
			ref = name
		}
		err = fmt.Errorf("%w: %q leaves %q", persistence.ErrFragmentPathOutsideBase, ref, anchor)
		if relativePath != "" && baseDir == "" {
			err = fmt.Errorf("%w: %w", err, fs.ErrNotExist)
		}
	}
	return data, path, err
}

// errPathEscapesRoot marks a read os.Root refused because the path, or a
// link on it, leaves the root.
var errPathEscapesRoot = errors.New("path escapes the root")

// readInAnchor reads path, which is lexically inside anchor, through one
// os.Root opened at anchor. Opening it is the anchor's one resolution: the
// anchor is opened as written, so a relative one binds to the working
// directory itself, not to a path string taken from it.
func readInAnchor(anchor, path string) ([]byte, error) {
	rel, err := filepath.Rel(anchor, path)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(anchor)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	if beforeRootedRead != nil {
		beforeRootedRead()
	}
	data, err := root.ReadFile(rel)
	if err == nil || !isRootEscape(err) {
		return data, err
	}
	// os.Root refuses every absolute link target. The anchor's real path
	// names such a target's file inside the root, and that file is read
	// through root, so a stale real path can refuse a read but never move it.
	realAnchor, realErr := resolvedAbs(anchor)
	if realErr != nil {
		// The anchor no longer resolves: nothing can be named inside it.
		return nil, fmt.Errorf("%w: %w", errPathEscapesRoot, err)
	}
	data, inside, readErr := readThroughResolvedLink(root, realAnchor, rel)
	if errors.Is(readErr, fs.ErrNotExist) && !inside {
		// A link to nothing names no file, wherever it points: not found.
		return nil, readErr
	}
	if inside && (readErr == nil || !isRootEscape(readErr)) {
		return data, readErr
	}
	return nil, fmt.Errorf("%w: %w", errPathEscapesRoot, err)
}

// readThroughResolvedLink handles os.Root refusing every absolute link
// target, including one inside the root: it resolves rel under realAnchor,
// and when the file lands inside reads it through root again by its path
// there, so the read stays confined (a link swapped meanwhile is refused by
// that read). inside reports whether the file resolved inside the anchor;
// the read's own error (permission denied, a directory) is then returned as
// it is.
func readThroughResolvedLink(root *os.Root, realAnchor, rel string) (data []byte, inside bool, err error) {
	target, err := resolvedAbs(filepath.Join(realAnchor, rel))
	if err != nil {
		return nil, false, err
	}
	if !within(realAnchor, target) {
		return nil, false, nil
	}
	inner, err := filepath.Rel(realAnchor, target)
	if err != nil {
		return nil, false, err
	}
	data, err = root.ReadFile(inner)
	return data, true, err
}

// isRootEscape reports whether err is os.Root refusing a path that leaves
// it. The os package does not export that error, so it is matched by its
// text, on a *fs.PathError that is not a missing file.
func isRootEscape(err error) bool {
	var pe *fs.PathError
	return errors.As(err, &pe) && !errors.Is(err, fs.ErrNotExist) &&
		strings.Contains(pe.Err.Error(), "escapes from parent")
}
