package templates

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	dirPerm  = 0o750
	filePerm = 0o600
	// DefaultGitHubIndex points to the community templates repo index.
	DefaultGitHubIndex = "https://raw.githubusercontent.com/AltairaLabs/promptkit-templates/main/index.yaml"
)

// DefaultIndex is the index location used for remote loads (overridable in tests).
var DefaultIndex = DefaultGitHubIndex
var defaultCacheDir string

// loadBytes loads a file from disk or HTTP.
func loadBytes(location string) ([]byte, error) {
	if strings.HasPrefix(location, "http://") || strings.HasPrefix(location, "https://") {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, location, http.NoBody)
		if err != nil {
			return nil, fmt.Errorf("http request %s: %w", location, err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("http get %s: %w", location, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("http get %s: status %d", location, resp.StatusCode)
		}
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read http body: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(location) //nolint:gosec // path from user/config
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", location, err)
	}
	return data, nil
}

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

// Index lists available templates using K8s-style metadata/spec.
type Index struct {
	APIVersion string            `yaml:"apiVersion"`
	Kind       string            `yaml:"kind"`
	Metadata   map[string]string `yaml:"metadata,omitempty"`
	Spec       IndexSpec         `yaml:"spec"`
}

// IndexSpec holds template entries.
type IndexSpec struct {
	Entries []IndexEntry `yaml:"entries"`
}

const (
	supportedIndexVersion     = "v1"
	supportedIndexVersionFull = "promptkit.altairalabs.ai/v1"
)

// TemplateFile is a single file in a template package.
type TemplateFile struct {
	Path    string `yaml:"path"`
	Content string `yaml:"content"`
	Source  string `yaml:"source,omitempty"`
}

// TemplatePackage holds files to render.
type TemplatePackage struct {
	Files []TemplateFile `yaml:"files"`
}

type templateCR struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Spec       struct {
		Variables []struct {
			Name     string `yaml:"name"`
			Required bool   `yaml:"required"`
			Default  string `yaml:"default"`
		} `yaml:"variables"`
		Files []TemplateFile `yaml:"files"`
	} `yaml:"spec"`
}

// LoadIndex loads an index from path.
func LoadIndex(path string) (*Index, error) {
	data, err := loadBytes(path)
	if err != nil {
		return nil, fmt.Errorf("load index: %w", err)
	}
	var idx Index
	if err := yaml.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parse index: %w", err)
	}
	if idx.APIVersion == "" || idx.Kind == "" {
		return nil, fmt.Errorf("index missing apiVersion or kind")
	}
	if !isSupportedIndexVersion(idx.APIVersion) {
		return nil, fmt.Errorf("unsupported index apiVersion %s", idx.APIVersion)
	}
	if !strings.EqualFold(idx.Kind, "TemplateIndex") {
		return nil, fmt.Errorf("unsupported index kind %s", idx.Kind)
	}
	if len(idx.Spec.Entries) == 0 {
		return nil, fmt.Errorf("index has no entries")
	}
	return &idx, nil
}

func isSupportedIndexVersion(v string) bool {
	return v == supportedIndexVersion || v == supportedIndexVersionFull
}

// FindEntry finds an entry by name (and optional version).
func (idx *Index) FindEntry(name, version string) (*IndexEntry, error) {
	if version == "" || version == "latest" {
		return idx.findLatest(name)
	}
	for i := range idx.Spec.Entries {
		e := idx.Spec.Entries[i]
		if e.Name == name {
			if e.Version == version {
				return &e, nil
			}
		}
	}
	return nil, fmt.Errorf("template %s@%s not found", name, version)
}

func (idx *Index) findLatest(name string) (*IndexEntry, error) {
	var latest *IndexEntry
	for i := range idx.Spec.Entries {
		e := idx.Spec.Entries[i]
		if e.Name != name {
			continue
		}
		if latest == nil || compareSemver(e.Version, latest.Version) > 0 {
			latest = &e
		}
	}
	if latest == nil {
		return nil, fmt.Errorf("template %s not found", name)
	}
	return latest, nil
}

// validateChecksumBytes compares sha256 to expected hex checksum (if provided).
func validateChecksumBytes(content []byte, expected string) error {
	if expected == "" {
		return nil
	}
	sum := sha256.Sum256(content)
	sumHex := hex.EncodeToString(sum[:])
	if sumHex != expected {
		return fmt.Errorf("checksum mismatch: got %s want %s", sumHex, expected)
	}
	return nil
}

// ValidateChecksum compares file sha256 to expected hex checksum (if provided).
func ValidateChecksum(path, expected string) error {
	if expected == "" {
		return nil
	}
	f, err := os.Open(path) //nolint:gosec // path is user-provided index entry
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

// DefaultCacheDir returns the default cache directory, preferring the user cache dir.
func DefaultCacheDir() string {
	if defaultCacheDir != "" {
		return defaultCacheDir
	}
	cacheDir, err := os.UserCacheDir()
	if err == nil && cacheDir != "" {
		defaultCacheDir = filepath.Join(cacheDir, "promptarena", "templates")
		return defaultCacheDir
	}
	// Fallback: create a unique temp dir to avoid predictable shared paths.
	tmp, err := os.MkdirTemp("", "promptarena-templates-")
	if err == nil {
		defaultCacheDir = tmp
		return defaultCacheDir
	}
	// Last resort: non-deterministic temp dir under system temp.
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	defaultCacheDir = filepath.Join(os.TempDir(), "promptarena-templates-"+suffix)
	return defaultCacheDir
}

// FetchTemplate copies the template source into cacheDir/<name>/<version>/template.yaml.
func FetchTemplate(entry *IndexEntry, cacheDir string) (string, error) {
	if entry == nil {
		return "", fmt.Errorf("entry is nil")
	}
	if entry.Source == "" {
		return "", fmt.Errorf("source missing for template %s", entry.Name)
	}
	data, err := loadBytes(entry.Source)
	if err != nil {
		return "", fmt.Errorf("load source: %w", err)
	}
	if err := validateChecksumBytes(data, entry.Checksum); err != nil {
		return "", err
	}
	destDir := filepath.Join(cacheDir, entry.Name, entry.Version)
	if err := os.MkdirAll(destDir, dirPerm); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}
	dest := filepath.Join(destDir, "template.yaml")
	if err := os.WriteFile(dest, data, filePerm); err != nil {
		return "", fmt.Errorf("write cache: %w", err)
	}
	return dest, nil
}

// LoadTemplatePackage loads a template package from path.
func LoadTemplatePackage(path string) (*TemplatePackage, error) {
	data, err := loadBytes(path)
	if err != nil {
		return nil, fmt.Errorf("read template: %w", err)
	}
	baseDir := filepath.Dir(path)

	if pkg, handled, err := tryParseTemplateCR(data, baseDir); handled {
		return pkg, err
	}

	var pkg TemplatePackage
	if err := yaml.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}
	if len(pkg.Files) == 0 {
		return nil, fmt.Errorf("template has no files")
	}
	return &pkg, nil
}

func tryParseTemplateCR(data []byte, baseDir string) (*TemplatePackage, bool, error) {
	var cr templateCR
	if err := yaml.Unmarshal(data, &cr); err != nil {
		return nil, false, nil
	}
	if len(cr.Spec.Files) == 0 {
		return nil, true, fmt.Errorf("template has no files")
	}

	pkg := &TemplatePackage{}
	for _, f := range cr.Spec.Files {
		if f.Path == "" {
			return nil, true, fmt.Errorf("template file path is empty")
		}
		content, err := resolveTemplateContent(f, baseDir)
		if err != nil {
			return nil, true, err
		}
		pkg.Files = append(pkg.Files, TemplateFile{
			Path:    f.Path,
			Content: content,
		})
	}
	return pkg, true, nil
}

func resolveTemplateContent(f TemplateFile, baseDir string) (string, error) {
	if f.Content != "" || f.Source == "" {
		return f.Content, nil
	}

	src := f.Source
	if !strings.HasPrefix(src, "http://") && !strings.HasPrefix(src, "https://") && !filepath.IsAbs(src) {
		src = filepath.Join(baseDir, src)
	}

	bytes, err := loadBytes(src)
	if err != nil {
		return "", fmt.Errorf("load source %s: %w", src, err)
	}
	return string(bytes), nil
}

// RenderDryRun renders the package with vars and writes to outDir.
func RenderDryRun(pkg *TemplatePackage, vars map[string]string, outDir string) error {
	if pkg == nil {
		return fmt.Errorf("template package is nil")
	}
	if err := os.MkdirAll(outDir, dirPerm); err != nil {
		return fmt.Errorf("make out dir: %w", err)
	}
	for _, f := range pkg.Files {
		if f.Path == "" {
			return fmt.Errorf("file path is empty")
		}
		destPath := filepath.Join(outDir, f.Path)
		if err := os.MkdirAll(filepath.Dir(destPath), dirPerm); err != nil {
			return fmt.Errorf("make parent dirs: %w", err)
		}
		tmpl, err := template.New("file").Parse(f.Content)
		if err != nil {
			return fmt.Errorf("parse template %s: %w", f.Path, err)
		}
		fp, err := os.Create(destPath) //nolint:gosec // destPath derived from template file path
		if err != nil {
			return fmt.Errorf("create file: %w", err)
		}
		if err := tmpl.Execute(fp, vars); err != nil {
			_ = fp.Close()
			return fmt.Errorf("render %s: %w", f.Path, err)
		}
		if err := fp.Close(); err != nil {
			return fmt.Errorf("close file: %w", err)
		}
	}
	return nil
}
