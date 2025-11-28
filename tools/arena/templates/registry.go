package templates

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/template"

	"gopkg.in/yaml.v3"
)

// IndexEntry describes a remote template.
type IndexEntry struct {
	Name        string   `yaml:"name"`
	Version     string   `yaml:"version"`
	Description string   `yaml:"description,omitempty"`
	Tags        []string `yaml:"tags,omitempty"`
	Source      string   `yaml:"source"` // path or URL (file-only for now)
	Checksum    string   `yaml:"checksum,omitempty"`
	Providers   []string `yaml:"providers,omitempty"`
	Author      string   `yaml:"author,omitempty"`
}

// Index lists available templates.
type Index struct {
	Entries []IndexEntry `yaml:"entries"`
}

// TemplateFile is a single file in a template package.
type TemplateFile struct {
	Path    string `yaml:"path"`
	Content string `yaml:"content"`
}

// TemplatePackage holds files to render.
type TemplatePackage struct {
	Files []TemplateFile `yaml:"files"`
}

// LoadIndex loads an index from path.
func LoadIndex(path string) (*Index, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read index: %w", err)
	}
	var idx Index
	if err := yaml.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parse index: %w", err)
	}
	return &idx, nil
}

// FindEntry finds an entry by name (and optional version).
func (idx *Index) FindEntry(name, version string) (*IndexEntry, error) {
	for i := range idx.Entries {
		e := idx.Entries[i]
		if e.Name == name {
			if version == "" || e.Version == version {
				return &e, nil
			}
		}
	}
	return nil, fmt.Errorf("template %s@%s not found", name, version)
}

// ValidateChecksum compares file sha256 to expected hex checksum (if provided).
func ValidateChecksum(path, expected string) error {
	if expected == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open for checksum: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash file: %w", err)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if sum != expected {
		return fmt.Errorf("checksum mismatch: got %s want %s", sum, expected)
	}
	return nil
}

// FetchTemplate copies the template source into cacheDir/<name>/<version>/template.yaml.
func FetchTemplate(entry IndexEntry, cacheDir string) (string, error) {
	if entry.Source == "" {
		return "", fmt.Errorf("source missing for template %s", entry.Name)
	}
	if err := ValidateChecksum(entry.Source, entry.Checksum); err != nil {
		return "", err
	}
	destDir := filepath.Join(cacheDir, entry.Name, entry.Version)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}
	dest := filepath.Join(destDir, "template.yaml")
	data, err := os.ReadFile(entry.Source)
	if err != nil {
		return "", fmt.Errorf("read source: %w", err)
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return "", fmt.Errorf("write cache: %w", err)
	}
	return dest, nil
}

// LoadTemplatePackage loads a template package from path.
func LoadTemplatePackage(path string) (*TemplatePackage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read template: %w", err)
	}
	var pkg TemplatePackage
	if err := yaml.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}
	return &pkg, nil
}

// RenderDryRun renders the package with vars and writes to outDir.
func RenderDryRun(pkg *TemplatePackage, vars map[string]string, outDir string) error {
	if pkg == nil {
		return fmt.Errorf("template package is nil")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("make out dir: %w", err)
	}
	for _, f := range pkg.Files {
		if f.Path == "" {
			return fmt.Errorf("file path is empty")
		}
		destPath := filepath.Join(outDir, f.Path)
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return fmt.Errorf("make parent dirs: %w", err)
		}
		tmpl, err := template.New("file").Parse(f.Content)
		if err != nil {
			return fmt.Errorf("parse template %s: %w", f.Path, err)
		}
		fp, err := os.Create(destPath)
		if err != nil {
			return fmt.Errorf("create file: %w", err)
		}
		if err := tmpl.Execute(fp, vars); err != nil {
			fp.Close()
			return fmt.Errorf("render %s: %w", f.Path, err)
		}
		fp.Close()
	}
	return nil
}
