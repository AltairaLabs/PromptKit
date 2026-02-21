package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/skills"
)

func TestSkillCmdRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Use == "skill" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'skill' command to be registered on rootCmd")
	}
}

func TestSkillSubcommands(t *testing.T) {
	want := map[string]bool{"install": false, "list": false, "remove": false}

	for _, cmd := range skillCmd.Commands() {
		if _, ok := want[cmd.Name()]; ok {
			want[cmd.Name()] = true
		}
	}

	for name, found := range want {
		if !found {
			t.Errorf("expected subcommand %q on skillCmd", name)
		}
	}
}

func TestSkillInstallProjectFlag(t *testing.T) {
	flag := skillInstallCmd.Flags().Lookup("project")
	if flag == nil {
		t.Fatal("expected --project flag on skill install")
	}
	if flag.DefValue != "false" {
		t.Errorf("--project default = %q, want %q", flag.DefValue, "false")
	}
}

func TestRunSkillListEmpty(t *testing.T) {
	// Capture stdout by redirecting to a buffer.
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Use temp dirs so no skills are found.
	origUserDir := os.Getenv("XDG_CONFIG_HOME")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	defer func() {
		if origUserDir != "" {
			os.Setenv("XDG_CONFIG_HOME", origUserDir)
		}
	}()

	err := runSkillList(nil, nil)

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("runSkillList() error: %v", err)
	}

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	if output == "" {
		t.Error("expected some output from runSkillList")
	}
}

func TestRunSkillInstallLocalPath(t *testing.T) {
	srcDir := t.TempDir()

	// Create a source skill.
	skillDir := filepath.Join(srcDir, "test-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: test-skill\ndescription: Test\n---\n\nInstructions"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Point installer to temp dirs.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// Capture stdout.
	old := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	err := runSkillInstall(nil, []string{skillDir})

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("runSkillInstall() error: %v", err)
	}
}

func TestRunSkillListWithSkills(t *testing.T) {
	userDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", userDir)

	// Create a skill in the user dir structure.
	skillPath := filepath.Join(userDir, "promptkit", "skills", "myorg", "myskill")
	if err := os.MkdirAll(skillPath, 0o755); err != nil {
		t.Fatal(err)
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runSkillList(nil, nil)

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("runSkillList() error: %v", err)
	}

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	if !bytes.Contains([]byte(output), []byte("myorg")) {
		t.Errorf("expected output to contain 'myorg', got: %s", output)
	}
}

func TestRunSkillRemoveSuccess(t *testing.T) {
	userDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", userDir)

	// Create a skill to remove.
	skillPath := filepath.Join(userDir, "promptkit", "skills", "org", "skill")
	if err := os.MkdirAll(skillPath, 0o755); err != nil {
		t.Fatal(err)
	}

	old := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	err := runSkillRemove(nil, []string{"@org/skill"})

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("runSkillRemove() error: %v", err)
	}

	// Verify removed.
	if _, statErr := os.Stat(skillPath); !os.IsNotExist(statErr) {
		t.Error("expected skill to be removed")
	}
}

func TestRunSkillInstallInvalidRef(t *testing.T) {
	err := runSkillInstall(nil, []string{"invalid-ref"})
	if err == nil {
		t.Fatal("expected error for invalid ref")
	}
}

func TestRunSkillRemoveInvalidRef(t *testing.T) {
	err := runSkillRemove(nil, []string{"not-a-ref"})
	if err == nil {
		t.Fatal("expected error for invalid ref")
	}
}

func TestRunSkillRemoveNotInstalled(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	err := runSkillRemove(nil, []string{"@org/nonexistent"})
	if err == nil {
		t.Fatal("expected error for non-installed skill")
	}
}

func TestParseSkillRefFromCLI(t *testing.T) {
	tests := []struct {
		arg     string
		wantOrg string
		wantN   string
		wantV   string
		wantErr bool
	}{
		{"@anthropic/pdf-processing", "anthropic", "pdf-processing", "", false},
		{"@org/skill@v1.0.0", "org", "skill", "v1.0.0", false},
		{"invalid", "", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			ref, err := skills.ParseSkillRef(tt.arg)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.arg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ref.Org != tt.wantOrg {
				t.Errorf("Org = %q, want %q", ref.Org, tt.wantOrg)
			}
			if ref.Name != tt.wantN {
				t.Errorf("Name = %q, want %q", ref.Name, tt.wantN)
			}
			if ref.Version != tt.wantV {
				t.Errorf("Version = %q, want %q", ref.Version, tt.wantV)
			}
		})
	}
}

func TestSkillInstallIntoFlag(t *testing.T) {
	flag := skillInstallCmd.Flags().Lookup("into")
	if flag == nil {
		t.Fatal("expected --into flag on skill install")
	}
	if flag.DefValue != "" {
		t.Errorf("--into default = %q, want empty", flag.DefValue)
	}
}

func TestRunSkillInstallIntoLocalPath(t *testing.T) {
	srcDir := t.TempDir()
	targetDir := t.TempDir()

	// Create a source skill.
	skillDir := filepath.Join(srcDir, "test-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: test-skill\ndescription: Test\n---\n\nInstructions"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Set --into flag.
	oldInto := skillIntoFlag
	skillIntoFlag = targetDir
	defer func() { skillIntoFlag = oldInto }()

	// Capture stdout.
	old := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	err := runSkillInstall(nil, []string{skillDir})

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("runSkillInstall() with --into error: %v", err)
	}

	// Verify installed to target dir.
	installed := filepath.Join(targetDir, "test-skill", "SKILL.md")
	if _, err := os.Stat(installed); err != nil {
		t.Errorf("expected SKILL.md at %s: %v", installed, err)
	}
}

func TestRunSkillInstallIntoInvalidRef(t *testing.T) {
	oldInto := skillIntoFlag
	skillIntoFlag = t.TempDir()
	defer func() { skillIntoFlag = oldInto }()

	err := runSkillInstall(nil, []string{"invalid-ref"})
	if err == nil {
		t.Fatal("expected error for invalid ref with --into")
	}
}

func TestIsLocalPathFromCLI(t *testing.T) {
	if !skills.IsLocalPath("./my-skill") {
		t.Error("expected ./my-skill to be a local path")
	}
	if skills.IsLocalPath("@org/skill") {
		t.Error("expected @org/skill to NOT be a local path")
	}
}
