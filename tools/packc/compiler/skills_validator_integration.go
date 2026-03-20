package compiler

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/skills"
)

const skillMDFilename = "SKILL.md"

// errPathTraversal is returned when a YAML-sourced path escapes the pack directory.
var errPathTraversal = fmt.Errorf("path traversal detected")

// validatePathContainment checks that resolvedAbs is within or equal to baseDir.
func validatePathContainment(rawPath, baseDir string) error {
	absPath := rawPath
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(baseDir, absPath)
	}
	absPath = filepath.Clean(absPath)
	cleanBase := filepath.Clean(baseDir)

	if absPath != cleanBase && !strings.HasPrefix(absPath, cleanBase+string(filepath.Separator)) {
		return fmt.Errorf("%w: %q resolves outside pack directory %q", errPathTraversal, rawPath, cleanBase)
	}
	return nil
}

// ValidateSkillErrors returns blocking errors for skill configuration in the pack.
func ValidateSkillErrors(pack *prompt.Pack, packDir string) []string {
	var errs []string

	seen := make(map[string]bool)

	for i := range pack.Skills {
		src := &pack.Skills[i]
		dir := src.EffectiveDir()
		if dir != "" {
			errs = append(errs, validateSkillDirectory(dir, packDir, i, seen)...)
		} else if src.Name != "" {
			errs = append(errs, validateInlineSkill(src, i, seen)...)
		}
	}

	if pack.Workflow != nil {
		errs = append(errs, validateWorkflowStateSkills(pack.Workflow, packDir)...)
	}

	return errs
}

// ValidateSkills returns non-blocking warnings for skill configuration in the pack.
func ValidateSkills(pack *prompt.Pack, packDir string) []string {
	var warnings []string

	packToolNames := make(map[string]bool)
	for name := range pack.Tools {
		packToolNames[name] = true
	}

	for i := range pack.Skills {
		dir := pack.Skills[i].EffectiveDir()
		if dir == "" {
			continue
		}
		if err := validatePathContainment(dir, packDir); err != nil {
			continue
		}
		absDir := dir
		if !filepath.IsAbs(absDir) {
			absDir = filepath.Join(packDir, absDir)
		}

		warnings = append(warnings, checkAllowedToolsCrossRef(absDir, packToolNames)...)
	}

	return warnings
}

// validateSkillDirectory validates a directory-based skill source.
func validateSkillDirectory(dir, packDir string, idx int, seen map[string]bool) []string {
	var errs []string

	if err := validatePathContainment(dir, packDir); err != nil {
		errs = append(errs, fmt.Sprintf("skills[%d]: %v", idx, err))
		return errs
	}

	absDir := dir
	if !filepath.IsAbs(absDir) {
		absDir = filepath.Join(packDir, absDir)
	}

	info, err := os.Stat(absDir)
	if err != nil || !info.IsDir() {
		errs = append(errs, fmt.Sprintf("skills[%d]: directory %q does not exist", idx, dir))
		return errs
	}

	err = filepath.WalkDir(absDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || d.Name() != skillMDFilename {
			return nil
		}

		meta, parseErr := skills.ParseSkillMetadata(path)
		if parseErr != nil {
			errs = append(errs, fmt.Sprintf("skills[%d]: failed to parse %s: %v", idx, path, parseErr))
			return nil
		}

		if seen[meta.Name] {
			errs = append(errs, fmt.Sprintf("skills[%d]: duplicate skill name %q", idx, meta.Name))
		}
		seen[meta.Name] = true

		return nil
	})
	if err != nil {
		errs = append(errs, fmt.Sprintf("skills[%d]: error walking directory %q: %v", idx, dir, err))
	}

	return errs
}

// validateInlineSkill validates an inline skill definition.
func validateInlineSkill(src *prompt.SkillSourceConfig, idx int, seen map[string]bool) []string {
	var errs []string

	if src.Name == "" {
		errs = append(errs, fmt.Sprintf("skills[%d]: inline skill missing required field: name", idx))
	}
	if src.Description == "" {
		errs = append(errs, fmt.Sprintf("skills[%d]: inline skill %q missing required field: description", idx, src.Name))
	}
	if src.Instructions == "" {
		errs = append(errs, fmt.Sprintf("skills[%d]: inline skill %q missing required field: instructions", idx, src.Name))
	}

	if src.Name != "" {
		if seen[src.Name] {
			errs = append(errs, fmt.Sprintf("skills[%d]: duplicate skill name %q", idx, src.Name))
		}
		seen[src.Name] = true
	}

	return errs
}

// validateWorkflowStateSkills validates skill paths referenced by workflow states.
func validateWorkflowStateSkills(wf *prompt.WorkflowConfig, packDir string) []string {
	var errs []string

	for name, state := range wf.States {
		if state.Skills == "" || strings.EqualFold(state.Skills, "none") {
			continue
		}

		if err := validatePathContainment(state.Skills, packDir); err != nil {
			errs = append(errs, fmt.Sprintf("workflow state %q: %v", name, err))
			continue
		}

		absPath := state.Skills
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Join(packDir, absPath)
		}

		info, err := os.Stat(absPath)
		if err != nil || !info.IsDir() {
			errs = append(errs, fmt.Sprintf(
				"workflow state %q: skills path %q does not exist", name, state.Skills))
		}
	}

	return errs
}

// checkAllowedToolsCrossRef checks if skill allowed-tools reference tools defined in the pack.
func checkAllowedToolsCrossRef(absDir string, packToolNames map[string]bool) []string {
	var warnings []string

	_ = filepath.WalkDir(absDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != skillMDFilename {
			return nil //nolint:nilerr // skip non-SKILL.md files
		}

		meta, parseErr := skills.ParseSkillMetadata(path)
		if parseErr != nil || meta == nil {
			return nil
		}

		for _, tool := range meta.AllowedTools {
			if !packToolNames[tool] {
				warnings = append(warnings, fmt.Sprintf(
					"skill %q: allowed-tool %q is not defined in pack tools", meta.Name, tool))
			}
		}
		return nil
	})

	return warnings
}
