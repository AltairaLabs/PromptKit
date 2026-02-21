package main

import (
	"github.com/spf13/cobra"
)

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage AgentSkills.io skills",
	Long: `Install, list, and remove shared skills from Git repositories or local paths.

Skills are installed to either a user-level directory (~/.config/promptkit/skills/)
or a project-level directory (.promptkit/skills/).

Subcommands:
  install   Install a skill from Git or local path
  list      List installed skills
  remove    Remove an installed skill

Examples:
  promptarena skill install @anthropic/pdf-processing
  promptarena skill install @anthropic/pdf-processing@v1.0.0
  promptarena skill install ./path/to/skill
  promptarena skill install @anthropic/pdf-processing --project
  promptarena skill list
  promptarena skill remove @anthropic/pdf-processing`,
}

var skillInstallCmd = &cobra.Command{
	Use:   "install <ref>",
	Short: "Install a skill from Git or local path",
	Long: `Install a skill from a Git repository or local filesystem path.

Skill references use the format @org/name[@version]:
  @org/skill-name           Clone from https://github.com/org/skill-name
  @org/skill-name@v1.2.0    Clone and checkout v1.2.0
  ./path/to/skill            Copy from local path

By default, skills are installed to the user-level directory
(~/.config/promptkit/skills/). Use --project to install to the
project-level directory (.promptkit/skills/), or --into to install
directly into a specific directory (e.g., a workflow stage directory).

Examples:
  promptarena skill install @anthropic/pdf-processing
  promptarena skill install @anthropic/pdf-processing@v1.0.0
  promptarena skill install ./path/to/skill
  promptarena skill install @anthropic/pdf-processing --project
  promptarena skill install @anthropic/pci-compliance --into ./skills/billing`,
	Args: cobra.ExactArgs(1),
	RunE: runSkillInstall,
}

var skillListCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed skills",
	Long: `List all installed skills from both user-level and project-level directories.

Search locations:
  .promptkit/skills/              (project-level)
  ~/.config/promptkit/skills/     (user-level)

Examples:
  promptarena skill list`,
	RunE: runSkillList,
}

var skillRemoveCmd = &cobra.Command{
	Use:   "remove <ref>",
	Short: "Remove an installed skill",
	Long: `Remove an installed skill by its @org/name reference.

The skill is removed from whichever directory it is found in
(project-level is checked first, then user-level).

Examples:
  promptarena skill remove @anthropic/pdf-processing`,
	Args: cobra.ExactArgs(1),
	RunE: runSkillRemove,
}

const (
	skillLevelUser    = "user"
	skillLevelProject = "project"
)

var (
	skillProjectFlag bool
	skillIntoFlag    string
)

func init() {
	rootCmd.AddCommand(skillCmd)
	skillCmd.AddCommand(skillInstallCmd)
	skillCmd.AddCommand(skillListCmd)
	skillCmd.AddCommand(skillRemoveCmd)
	skillInstallCmd.Flags().BoolVar(&skillProjectFlag, "project", false,
		"Install to project-level (.promptkit/skills/) instead of user-level")
	skillInstallCmd.Flags().StringVar(&skillIntoFlag, "into", "",
		"Install directly into a specific directory (e.g., a workflow stage skills directory)")
	skillInstallCmd.MarkFlagsMutuallyExclusive("project", "into")
}
