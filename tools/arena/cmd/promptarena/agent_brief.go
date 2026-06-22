package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/tools/arena/agentkb"
	"github.com/AltairaLabs/PromptKit/tools/arena/templates"
)

const agentBriefMarker = "<!-- promptarena-authoring -->"

const (
	skillDirPerm  = 0o750
	briefFilePerm = 0o600
)

var skillRelPath = filepath.Join(".claude", "skills", "promptarena-authoring", "SKILL.md")

var referenceRelDir = filepath.Join(".claude", "skills", "promptarena-authoring", "reference")

// agentBriefCmd installs the authoring brief — an AGENTS.md shim plus the full
// promptarena-authoring skill — into an existing project so an AI coding agent
// starts briefed. Unlike `init`, it scaffolds no sample kit; it only briefs the
// agent. This is what equips a real coding agent with PromptArena's tooling.
var agentBriefCmd = &cobra.Command{
	Use:   "agent-brief [dir]",
	Short: "Write the authoring brief (AGENTS.md + .claude/skills) into a project",
	Long: `Install the PromptArena authoring brief into a project directory:

  AGENTS.md                                        a shim pointing the agent at the skill + CLI
  .claude/skills/promptarena-authoring/SKILL.md    the full authoring skill

Unlike 'init', this scaffolds no sample kit — it only briefs the agent, so it is
safe to run in an existing or empty project. Hand-written AGENTS.md content is
preserved (the brief is appended once, idempotently).

  promptarena agent-brief            # brief the current directory
  promptarena agent-brief ./myproj   # brief ./myproj`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := "."
		if len(args) == 1 {
			dir = args[0]
		}
		written, err := writeAgentBriefTo(dir)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		for _, f := range written {
			_, _ = fmt.Fprintf(out, "wrote %s\n", filepath.Join(dir, f))
		}
		_, _ = fmt.Fprintln(out, "Agent briefed. Discover idioms with `promptarena explain --list`.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(agentBriefCmd)
}

// writeAgentBriefTo drops the PromptArena authoring skill and an AGENTS.md shim
// into projectPath so an AI coding agent starts briefed. The skill is always
// (idempotently) rewritten; AGENTS.md is created or has the brief appended once.
// It returns the relative paths actually written.
func writeAgentBriefTo(projectPath string) ([]string, error) {
	var written []string
	skill, err := agentkb.Skill()
	if err != nil {
		return nil, fmt.Errorf("assemble skill: %w", err)
	}
	skillPath := filepath.Join(projectPath, skillRelPath)
	if err = os.MkdirAll(filepath.Dir(skillPath), skillDirPerm); err != nil {
		return nil, fmt.Errorf("create skill dir: %w", err)
	}
	if err = os.WriteFile(skillPath, skill, briefFilePerm); err != nil {
		return nil, fmt.Errorf("write skill: %w", err)
	}
	written = append(written, skillRelPath)

	refs, err := writeReferenceDocs(projectPath)
	if err != nil {
		return nil, err
	}
	written = append(written, refs...)

	rel, err := writeAgentsFile(projectPath)
	if err != nil {
		return nil, err
	}
	if rel != "" {
		written = append(written, rel)
	}
	return written, nil
}

// writeReferenceDocs writes the bundled reference catalogs (eval/assertion,
// config-fields, CLI) into the skill's reference/ directory so the agent reads
// them locally instead of shelling out to explain/schema repeatedly. Returns
// the relative paths written.
func writeReferenceDocs(projectPath string) ([]string, error) {
	names, err := agentkb.ReferenceNames()
	if err != nil {
		return nil, fmt.Errorf("list reference docs: %w", err)
	}
	refDir := filepath.Join(projectPath, referenceRelDir)
	if err = os.MkdirAll(refDir, skillDirPerm); err != nil {
		return nil, fmt.Errorf("create reference dir: %w", err)
	}
	var written []string
	for _, name := range names {
		content, refErr := agentkb.Reference(name)
		if refErr != nil {
			return nil, refErr
		}
		if refErr = os.WriteFile(filepath.Join(refDir, name), content, briefFilePerm); refErr != nil {
			return nil, fmt.Errorf("write reference %s: %w", name, refErr)
		}
		written = append(written, filepath.Join(referenceRelDir, name))
	}
	return written, nil
}

// writeAgentBrief drops the authoring brief into a freshly scaffolded project and
// records the files on result.FilesCreated. Used by `init`.
func writeAgentBrief(result *templates.GenerationResult) error {
	written, err := writeAgentBriefTo(result.ProjectPath)
	if err != nil {
		return err
	}
	result.FilesCreated = append(result.FilesCreated, written...)
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
