package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AltairaLabs/PromptKit/tools/arena/agentkb"
	"github.com/AltairaLabs/PromptKit/tools/arena/templates"
)

const agentBriefMarker = "<!-- promptarena-authoring -->"

const (
	skillDirPerm  = 0o750
	briefFilePerm = 0o600
)

var skillRelPath = filepath.Join(".claude", "skills", "promptarena-authoring", "SKILL.md")

// writeAgentBrief drops the PromptArena authoring skill and an AGENTS.md shim into
// a freshly scaffolded project so an AI coding agent starts briefed. Relative paths
// of files actually written are appended to result.FilesCreated.
func writeAgentBrief(result *templates.GenerationResult) error {
	skill, err := agentkb.Skill()
	if err != nil {
		return fmt.Errorf("assemble skill: %w", err)
	}
	skillPath := filepath.Join(result.ProjectPath, skillRelPath)
	if err = os.MkdirAll(filepath.Dir(skillPath), skillDirPerm); err != nil {
		return fmt.Errorf("create skill dir: %w", err)
	}
	if err = os.WriteFile(skillPath, skill, briefFilePerm); err != nil {
		return fmt.Errorf("write skill: %w", err)
	}
	result.FilesCreated = append(result.FilesCreated, skillRelPath)

	rel, err := writeAgentsFile(result.ProjectPath)
	if err != nil {
		return err
	}
	if rel != "" {
		result.FilesCreated = append(result.FilesCreated, rel)
	}
	return nil
}

// writeAgentsFile creates AGENTS.md, or appends the brief to an existing one. It
// returns the relative path when it wrote, or "" when an existing file already
// carried the brief. It never clobbers hand-written content.
func writeAgentsFile(projectPath string) (string, error) {
	const rel = "AGENTS.md"
	path := filepath.Join(projectPath, rel)
	brief := agentkb.AgentsBrief()

	existing, err := os.ReadFile(path) //nolint:gosec // path is under the just-created project dir
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err = os.WriteFile(path, brief, briefFilePerm); err != nil {
			return "", fmt.Errorf("write AGENTS.md: %w", err)
		}
		return rel, nil
	case err != nil:
		return "", fmt.Errorf("read AGENTS.md: %w", err)
	default:
		if bytes.Contains(existing, []byte(agentBriefMarker)) {
			return "", nil
		}
		merged := append(append([]byte{}, existing...), '\n')
		merged = append(merged, brief...)
		//nolint:gosec // path is under the just-created project dir
		if err = os.WriteFile(path, merged, briefFilePerm); err != nil {
			return "", fmt.Errorf("append AGENTS.md: %w", err)
		}
		return rel, nil
	}
}
