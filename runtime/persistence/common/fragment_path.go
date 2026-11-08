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
//   - a relative path resolves against baseDir and must stay inside basePath,
//     or the lookup fails with persistence.ErrFragmentPathOutsideBase. With no
//     baseDir it resolves against the working directory and must stay inside
//     it; outside, the error also satisfies fs.ErrNotExist (nothing it may
//     read is there);
//   - no path is the default file, basePath/fragments/<name><ext>.
func FragmentFilePath(basePath, name, relativePath, baseDir, ext string) (string, error) {
	switch {
	case filepath.IsAbs(relativePath):
		return relativePath, nil
	case relativePath == "":
		return filepath.Join(basePath, "fragments", name+ext), nil
	}
	path := filepath.Join(baseDir, relativePath)
	if baseDir == "" {
		// The working-directory fallback FragmentResolver tries after a miss
		// under the prompt's directory. A path that leaves the working
		// directory is not read, and finds nothing: the error also satisfies
		// fs.ErrNotExist, so the resolver reports the miss it fell back from.
		if !within(".", path) {
			return "", fmt.Errorf("%w: %q (working directory): %w",
				persistence.ErrFragmentPathOutsideBase, relativePath, fs.ErrNotExist)
		}
		return path, nil
	}
	if !within(basePath, path) {
		return "", fmt.Errorf("%w: %q (base %q)", persistence.ErrFragmentPathOutsideBase, relativePath, basePath)
	}
	return path, nil
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
