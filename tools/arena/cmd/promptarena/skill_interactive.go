package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/runtime/skills"
)

func runSkillInstall(_ *cobra.Command, args []string) error {
	arg := args[0]

	inst, err := skills.NewInstaller()
	if err != nil {
		return fmt.Errorf("initializing installer: %w", err)
	}

	// --into: install directly into a target directory.
	if skillIntoFlag != "" {
		return runSkillInstallInto(inst, arg)
	}

	if skills.IsLocalPath(arg) {
		path, installErr := inst.InstallLocal(arg, skillProjectFlag)
		if installErr != nil {
			return installErr
		}
		fmt.Printf("Installed local skill to %s\n", path)
		return nil
	}

	ref, err := skills.ParseSkillRef(arg)
	if err != nil {
		return err
	}

	levelStr := skillLevelUser
	if skillProjectFlag {
		levelStr = skillLevelProject
	}

	fmt.Printf("Installing %s (%s-level)...\n", ref.FullName(), levelStr)
	if ref.Version != "" {
		fmt.Printf("  Version: %s\n", ref.Version)
	}

	path, err := inst.Install(ref, skillProjectFlag)
	if err != nil {
		return err
	}

	fmt.Printf("  Installed to: %s\n", path)
	return nil
}

func runSkillInstallInto(inst *skills.Installer, arg string) error {
	if skills.IsLocalPath(arg) {
		path, err := inst.InstallLocalInto(arg, skillIntoFlag)
		if err != nil {
			return err
		}
		fmt.Printf("Installed local skill to %s\n", path)
		return nil
	}

	ref, err := skills.ParseSkillRef(arg)
	if err != nil {
		return err
	}

	fmt.Printf("Installing %s into %s...\n", ref.FullName(), skillIntoFlag)
	if ref.Version != "" {
		fmt.Printf("  Version: %s\n", ref.Version)
	}

	path, err := inst.InstallInto(ref, skillIntoFlag)
	if err != nil {
		return err
	}

	fmt.Printf("  Installed to: %s\n", path)
	return nil
}

func runSkillList(_ *cobra.Command, _ []string) error {
	inst, err := skills.NewInstaller()
	if err != nil {
		return fmt.Errorf("initializing installer: %w", err)
	}

	installed, err := inst.List()
	if err != nil {
		return err
	}

	if len(installed) == 0 {
		fmt.Println("No skills installed.")
		fmt.Println("Use 'promptarena skill install <ref>' to install one.")
		return nil
	}

	// Group by location.
	var currentLocation string
	for _, s := range installed {
		if s.Location != currentLocation {
			if currentLocation != "" {
				fmt.Println()
			}
			fmt.Printf("%s:\n", strings.ToUpper(s.Location[:1])+s.Location[1:])
			currentLocation = s.Location
		}
		fmt.Printf("  @%s/%s  (%s)\n", s.Org, s.Name, s.Path)
	}

	return nil
}

func runSkillRemove(_ *cobra.Command, args []string) error {
	ref, err := skills.ParseSkillRef(args[0])
	if err != nil {
		return err
	}

	inst, err := skills.NewInstaller()
	if err != nil {
		return fmt.Errorf("initializing installer: %w", err)
	}

	if err := inst.Remove(ref); err != nil {
		return err
	}

	fmt.Printf("Removed skill %s\n", ref.FullName())
	return nil
}
