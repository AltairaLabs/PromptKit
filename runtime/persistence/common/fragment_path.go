package common

import (
	"fmt"
	"io/fs"
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
// the point the path may reach is refused.
func FragmentFilePath(basePath, name, relativePath, baseDir, ext string) (string, error) {
	switch {
	case filepath.IsAbs(relativePath):
		return relativePath, nil
	case relativePath == "":
		path := filepath.Join(basePath, "fragments", name+ext)
		if !within(filepath.Join(basePath, "fragments"), path) {
			return "", fmt.Errorf("%w: fragment name %q", persistence.ErrFragmentPathOutsideBase, name)
		}
		return containResolved(path, name, basePath)
	}
	path := filepath.Join(baseDir, relativePath)
	anchor := climbAnchor(baseDir, relativePath)
	if !within(anchor, path) {
		err := fmt.Errorf("%w: %q climbs past %q", persistence.ErrFragmentPathOutsideBase, relativePath, anchor)
		if baseDir == "" {
			// The working-directory fallback FragmentResolver tries after a
			// miss: report it as not found, so the resolver reports that miss.
			err = fmt.Errorf("%w: %w", err, fs.ErrNotExist)
		}
		return "", err
	}
	return containResolved(path, relativePath, anchor)
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
