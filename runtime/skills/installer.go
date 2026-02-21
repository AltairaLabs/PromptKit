package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// dirPerms is the permission mode for created directories.
const dirPerms = 0o750

// InstalledSkill describes a skill found in user-level or project-level directories.
type InstalledSkill struct {
	Org      string // e.g., "anthropic"
	Name     string // e.g., "pdf-processing"
	Location string // "user" or "project"
	Path     string // filesystem path to the skill directory
}

// Installer manages installing, removing, listing, and resolving shared skills.
type Installer struct {
	UserDir      string                       // ~/.config/promptkit/skills/
	ProjectDir   string                       // .promptkit/skills/
	GitCloneFunc func(url, dest string) error // injectable for testing
	GitCheckout  func(dir, ref string) error  // injectable for testing
	CopyDirFunc  func(src, dest string) error // injectable for testing
}

// NewInstaller creates an Installer with default XDG-compliant paths.
func NewInstaller() (*Installer, error) {
	userDir, err := DefaultSkillsUserDir()
	if err != nil {
		return nil, err
	}

	projectDir, err := DefaultSkillsProjectDir()
	if err != nil {
		return nil, err
	}

	return &Installer{
		UserDir:      userDir,
		ProjectDir:   projectDir,
		GitCloneFunc: defaultGitClone,
		GitCheckout:  defaultGitCheckout,
		CopyDirFunc:  defaultCopyDir,
	}, nil
}

// Install clones a skill from its Git repository into the appropriate directory.
// If projectLevel is true, installs to .promptkit/skills/; otherwise to user-level.
// Returns the installation path.
func (inst *Installer) Install(ref SkillRef, projectLevel bool) (string, error) {
	baseDir := inst.UserDir
	if projectLevel {
		baseDir = inst.ProjectDir
	}

	destDir := filepath.Join(baseDir, ref.Org, ref.Name)

	if _, err := os.Stat(destDir); err == nil {
		return "", fmt.Errorf("skill %s already installed at %s", ref.FullName(), destDir)
	}

	if err := os.MkdirAll(filepath.Dir(destDir), dirPerms); err != nil {
		return "", fmt.Errorf("creating parent directory: %w", err)
	}

	if err := inst.cloneAndVerify(ref, destDir); err != nil {
		return "", err
	}

	return destDir, nil
}

// InstallInto clones a skill from its Git repository directly into a target
// directory (e.g., a workflow stage directory like ./skills/billing/).
// The skill is placed at <targetDir>/<name>/. Returns the installation path.
func (inst *Installer) InstallInto(ref SkillRef, targetDir string) (string, error) {
	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		return "", fmt.Errorf("resolving target directory: %w", err)
	}

	destDir := filepath.Join(absTarget, ref.Name)

	if _, err := os.Stat(destDir); err == nil {
		return "", fmt.Errorf("skill %s already exists at %s", ref.FullName(), destDir)
	}

	if err := os.MkdirAll(absTarget, dirPerms); err != nil {
		return "", fmt.Errorf("creating target directory: %w", err)
	}

	if err := inst.cloneAndVerify(ref, destDir); err != nil {
		return "", err
	}

	return destDir, nil
}

// cloneAndVerify performs the common git clone, optional checkout, and SKILL.md verification.
// On any failure the destination directory is cleaned up.
func (inst *Installer) cloneAndVerify(ref SkillRef, destDir string) error {
	if err := inst.GitCloneFunc(ref.GitURL(), destDir); err != nil {
		_ = os.RemoveAll(destDir)
		return fmt.Errorf("cloning %s: %w", ref.GitURL(), err)
	}

	if ref.Version != "" {
		if err := inst.GitCheckout(destDir, ref.Version); err != nil {
			_ = os.RemoveAll(destDir)
			return fmt.Errorf("checking out %s: %w", ref.Version, err)
		}
	}

	if !hasSkillFile(destDir) {
		_ = os.RemoveAll(destDir)
		return fmt.Errorf("cloned repository does not contain a SKILL.md file")
	}

	return nil
}

// InstallLocalInto copies a skill from a local path directly into a target
// directory. Returns the installation path.
func (inst *Installer) InstallLocalInto(srcPath, targetDir string) (string, error) {
	absSrc, err := filepath.Abs(srcPath)
	if err != nil {
		return "", fmt.Errorf("resolving source path: %w", err)
	}

	info, err := os.Stat(absSrc)
	if err != nil {
		return "", fmt.Errorf("source path %s: %w", srcPath, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("source path %s is not a directory", srcPath)
	}

	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		return "", fmt.Errorf("resolving target directory: %w", err)
	}

	skillName := filepath.Base(absSrc)
	destDir := filepath.Join(absTarget, skillName)

	if _, err := os.Stat(destDir); err == nil {
		return "", fmt.Errorf("skill %s already exists at %s", skillName, destDir)
	}

	if err := os.MkdirAll(absTarget, dirPerms); err != nil {
		return "", fmt.Errorf("creating target directory: %w", err)
	}

	if err := inst.CopyDirFunc(absSrc, destDir); err != nil {
		_ = os.RemoveAll(destDir)
		return "", fmt.Errorf("copying skill: %w", err)
	}

	return destDir, nil
}

// InstallLocal copies a skill from a local path into the appropriate directory.
// Returns the installation path.
func (inst *Installer) InstallLocal(srcPath string, projectLevel bool) (string, error) {
	absSrc, err := filepath.Abs(srcPath)
	if err != nil {
		return "", fmt.Errorf("resolving source path: %w", err)
	}

	info, err := os.Stat(absSrc)
	if err != nil {
		return "", fmt.Errorf("source path %s: %w", srcPath, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("source path %s is not a directory", srcPath)
	}

	// Derive skill name from the directory name.
	skillName := filepath.Base(absSrc)
	baseDir := inst.UserDir
	if projectLevel {
		baseDir = inst.ProjectDir
	}

	destDir := filepath.Join(baseDir, "local", skillName)

	if _, err := os.Stat(destDir); err == nil {
		return "", fmt.Errorf("skill %s already installed at %s", skillName, destDir)
	}

	if err := os.MkdirAll(filepath.Dir(destDir), dirPerms); err != nil {
		return "", fmt.Errorf("creating parent directory: %w", err)
	}

	if err := inst.CopyDirFunc(absSrc, destDir); err != nil {
		_ = os.RemoveAll(destDir)
		return "", fmt.Errorf("copying skill: %w", err)
	}

	return destDir, nil
}

// Remove removes an installed skill. It checks project-level first, then user-level.
func (inst *Installer) Remove(ref SkillRef) error {
	path, err := inst.Resolve(ref)
	if err != nil {
		return err
	}

	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("removing skill %s: %w", ref.FullName(), err)
	}

	return nil
}

// List returns all installed skills from both user and project directories.
func (inst *Installer) List() ([]InstalledSkill, error) {
	var results []InstalledSkill

	projectSkills, err := inst.listDir(inst.ProjectDir, "project")
	if err != nil {
		return nil, err
	}
	results = append(results, projectSkills...)

	userSkills, err := inst.listDir(inst.UserDir, "user")
	if err != nil {
		return nil, err
	}
	results = append(results, userSkills...)

	sort.Slice(results, func(i, j int) bool {
		if results[i].Location != results[j].Location {
			return results[i].Location < results[j].Location
		}
		return results[i].Org+"/"+results[i].Name < results[j].Org+"/"+results[j].Name
	})

	return results, nil
}

// Resolve finds the installation path for a skill reference.
// Checks project-level first, then user-level.
func (inst *Installer) Resolve(ref SkillRef) (string, error) {
	// Check project-level first.
	projectPath := filepath.Join(inst.ProjectDir, ref.Org, ref.Name)
	if info, err := os.Stat(projectPath); err == nil && info.IsDir() {
		return projectPath, nil
	}

	// Check user-level.
	userPath := filepath.Join(inst.UserDir, ref.Org, ref.Name)
	if info, err := os.Stat(userPath); err == nil && info.IsDir() {
		return userPath, nil
	}

	return "", fmt.Errorf("skill %s is not installed", ref.FullName())
}

// listDir scans a base directory for installed skills (org/name structure).
func (inst *Installer) listDir(baseDir, location string) ([]InstalledSkill, error) {
	orgs, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s skills directory: %w", location, err)
	}

	var results []InstalledSkill
	for _, org := range orgs {
		if !org.IsDir() {
			continue
		}
		orgDir := filepath.Join(baseDir, org.Name())
		skills, err := os.ReadDir(orgDir)
		if err != nil {
			continue
		}
		for _, skill := range skills {
			if !skill.IsDir() {
				continue
			}
			results = append(results, InstalledSkill{
				Org:      org.Name(),
				Name:     skill.Name(),
				Location: location,
				Path:     filepath.Join(orgDir, skill.Name()),
			})
		}
	}

	return results, nil
}

// DefaultSkillsUserDir returns the XDG-compliant user-level skills directory.
func DefaultSkillsUserDir() (string, error) {
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine home directory: %w", err)
		}
		configDir = filepath.Join(home, ".config")
	}
	return filepath.Join(configDir, "promptkit", "skills"), nil
}

// DefaultSkillsProjectDir returns the project-level skills directory.
func DefaultSkillsProjectDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot determine working directory: %w", err)
	}
	return filepath.Join(cwd, ".promptkit", "skills"), nil
}

// hasSkillFile checks if a directory (or any subdirectory) contains a SKILL.md file.
func hasSkillFile(dir string) bool {
	found := false
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && info.Name() == "SKILL.md" {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// defaultCopyDir recursively copies a directory tree.
func defaultCopyDir(src, dest string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		destPath := filepath.Join(dest, relPath)

		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode())
		}

		data, err := os.ReadFile(path) //nolint:gosec // path is bounded by Walk
		if err != nil {
			return err
		}

		return os.WriteFile(destPath, data, info.Mode())
	})
}

// IsLocalPath returns true if the argument looks like a local filesystem path
// rather than a skill reference (starts with "./", "../", or "/").
func IsLocalPath(arg string) bool {
	return strings.HasPrefix(arg, "./") ||
		strings.HasPrefix(arg, "../") ||
		strings.HasPrefix(arg, "/")
}
