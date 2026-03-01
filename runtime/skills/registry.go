package skills

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// skillMDFile is the filename expected in every skill directory.
const skillMDFile = "SKILL.md"

// ErrPathTraversal is returned when a resource path attempts to escape the skill directory.
var ErrPathTraversal = errors.New("resource path escapes skill directory")

// Registry holds discovered skills and provides access by name and directory.
type Registry struct {
	mu     sync.RWMutex
	skills map[string]*registeredSkill // keyed by skill name
}

type registeredSkill struct {
	metadata SkillMetadata
	path     string // filesystem path to skill directory (empty for inline)
	inline   *Skill // non-nil for inline skills
	preload  bool
}

// NewRegistry creates a new empty skill registry.
func NewRegistry() *Registry {
	return &Registry{
		skills: make(map[string]*registeredSkill),
	}
}

// Discover scans skill sources and registers all found skills.
// Sources are processed in order — later sources do NOT override earlier ones (first wins).
func (r *Registry) Discover(sources []SkillSource) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, src := range sources {
		if dir := src.EffectiveDir(); dir != "" {
			resolved, err := r.resolveSource(&src)
			if err != nil {
				return err
			}
			if err := r.discoverDirectory(resolved); err != nil {
				return fmt.Errorf("discovering skills in %s: %w", resolved.EffectiveDir(), err)
			}
		} else if src.Name != "" {
			r.registerInline(src)
		}
	}
	return nil
}

// resolveSource resolves @org/name references in a SkillSource to a local directory path.
// Returns the source unchanged if it's not an @-ref.
func (r *Registry) resolveSource(src *SkillSource) (SkillSource, error) {
	dir := src.EffectiveDir()
	if !strings.HasPrefix(dir, "@") {
		return *src, nil
	}
	ref, err := ParseSkillRef(dir)
	if err != nil {
		return SkillSource{}, fmt.Errorf("parsing skill reference %s: %w", dir, err)
	}
	resolved, err := r.ResolveRef(ref)
	if err != nil {
		return SkillSource{}, fmt.Errorf("resolving skill reference %s: %w", dir, err)
	}
	return SkillSource{Dir: resolved, Preload: src.Preload}, nil
}

// discoverDirectory walks a directory looking for SKILL.md files and registers each skill found.
// Must be called with r.mu held.
func (r *Registry) discoverDirectory(src SkillSource) error {
	absDir, err := filepath.Abs(src.EffectiveDir())
	if err != nil {
		return fmt.Errorf("resolving directory path: %w", err)
	}

	return filepath.Walk(absDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || info.Name() != skillMDFile {
			return nil
		}

		meta, parseErr := ParseSkillMetadata(path)
		if parseErr != nil {
			return fmt.Errorf("parsing %s: %w", path, parseErr)
		}

		skillDir := filepath.Dir(path)
		if _, exists := r.skills[meta.Name]; exists {
			logger.Warn("skills: duplicate skill ignored (already registered)", "skill", meta.Name)
			return nil
		}

		r.skills[meta.Name] = &registeredSkill{
			metadata: *meta,
			path:     skillDir,
			preload:  src.Preload,
		}
		return nil
	})
}

// registerInline registers an inline skill source directly.
// Must be called with r.mu held.
func (r *Registry) registerInline(src SkillSource) {
	if _, exists := r.skills[src.Name]; exists {
		logger.Warn("skills: duplicate skill ignored (already registered)", "skill", src.Name)
		return
	}

	skill := &Skill{
		SkillMetadata: SkillMetadata{
			Name:        src.Name,
			Description: src.Description,
		},
		Instructions: src.Instructions,
	}

	r.skills[src.Name] = &registeredSkill{
		metadata: skill.SkillMetadata,
		inline:   skill,
		preload:  src.Preload,
	}
}

// List returns metadata for all registered skills, sorted by name.
func (r *Registry) List() []SkillMetadata {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]SkillMetadata, 0, len(r.skills))
	for _, rs := range r.skills {
		result = append(result, rs.metadata)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// ListForDir returns metadata for skills whose path is under the given directory.
// If dir is empty, returns all skills. Results are sorted by name.
func (r *Registry) ListForDir(dir string) []SkillMetadata {
	if dir == "" {
		return r.List()
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil
	}
	absDir = filepath.Clean(absDir) + string(filepath.Separator)

	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []SkillMetadata
	for _, rs := range r.skills {
		if rs.path == "" {
			continue
		}
		absPath, err := filepath.Abs(rs.path)
		if err != nil {
			continue
		}
		absPath = filepath.Clean(absPath)
		if strings.HasPrefix(absPath+string(filepath.Separator), absDir) {
			result = append(result, rs.metadata)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// Load returns the full skill by name, reading instructions from disk on demand.
func (r *Registry) Load(name string) (*Skill, error) {
	r.mu.RLock()
	rs, exists := r.skills[name]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("skill %q not found", name)
	}

	if rs.inline != nil {
		return rs.inline, nil
	}

	skillPath := filepath.Join(rs.path, skillMDFile)
	skill, err := ParseSkillFile(skillPath)
	if err != nil {
		return nil, fmt.Errorf("loading skill %q: %w", name, err)
	}
	skill.Path = rs.path
	return skill, nil
}

// ReadResource reads a file from within a skill's directory.
// Returns error if the path escapes the skill directory (path traversal prevention).
func (r *Registry) ReadResource(name, resourcePath string) ([]byte, error) {
	r.mu.RLock()
	rs, exists := r.skills[name]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("skill %q not found", name)
	}

	if rs.path == "" {
		return nil, fmt.Errorf("skill %q is inline and has no directory", name)
	}

	// Resolve the skill directory to its canonical absolute path.
	baseDir, err := filepath.EvalSymlinks(rs.path)
	if err != nil {
		return nil, fmt.Errorf("resolving skill directory: %w", err)
	}
	baseDir, err = filepath.Abs(baseDir)
	if err != nil {
		return nil, fmt.Errorf("resolving skill directory: %w", err)
	}

	// Resolve the requested resource path.
	target := filepath.Join(baseDir, resourcePath)
	target = filepath.Clean(target)

	// Before EvalSymlinks, check that the cleaned path is still under the base directory.
	// This catches obvious traversal attempts even if the target doesn't exist yet.
	if !strings.HasPrefix(target, baseDir+string(filepath.Separator)) && target != baseDir {
		return nil, fmt.Errorf("%w: %s", ErrPathTraversal, resourcePath)
	}

	// Read the file — if it doesn't exist, os.ReadFile returns an os.ErrNotExist-wrapping error.
	data, err := os.ReadFile(target)
	if err != nil {
		return nil, err
	}

	// After reading, resolve symlinks on the actual file and verify again.
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return nil, err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(resolved, baseDir+string(filepath.Separator)) && resolved != baseDir {
		return nil, fmt.Errorf("%w: %s", ErrPathTraversal, resourcePath)
	}

	return data, nil
}

// PreloadedSkills returns fully loaded skills marked with preload: true.
func (r *Registry) PreloadedSkills() []*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*Skill
	for name, rs := range r.skills {
		if !rs.preload {
			continue
		}
		if rs.inline != nil {
			result = append(result, rs.inline)
			continue
		}
		skillPath := filepath.Join(rs.path, skillMDFile)
		skill, err := ParseSkillFile(skillPath)
		if err != nil {
			logger.Error("skills: failed to preload skill", "skill", name, "error", err)
			continue
		}
		skill.Path = rs.path
		result = append(result, skill)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// Has returns true if a skill with the given name is registered.
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.skills[name]
	return exists
}

// ResolveRef resolves an @org/name skill reference to its installed filesystem path.
// It delegates to the Installer's Resolve method which checks project-level first,
// then user-level directories.
func (r *Registry) ResolveRef(ref SkillRef) (string, error) {
	inst, err := NewInstaller()
	if err != nil {
		return "", err
	}
	return inst.Resolve(ref)
}
