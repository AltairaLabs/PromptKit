package config

import (
	"path/filepath"
)

// ResolveOutputPath resolves an output file path relative to the output directory.
// If the filename is an absolute path, it is returned as-is.
// If the filename is empty, an empty string is returned.
// Otherwise, the filename is joined with the output directory.
func ResolveOutputPath(outDir, filename string) string {
	if filename == "" {
		return ""
	}

	if filepath.IsAbs(filename) {
		return filename
	}

	return filepath.Join(outDir, filename)
}

// ResolveFilePath resolves a file path relative to a base directory
func ResolveFilePath(basePath, filePath string) string {
	if filepath.IsAbs(filePath) {
		return filePath
	}
	return filepath.Join(filepath.Dir(basePath), filePath)
}
