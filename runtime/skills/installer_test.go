package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGitClone returns a GitCloneFunc that simulates cloning by creating a SKILL.md.
func fakeGitClone() func(string, string) error {
	return func(_, dest string) error {
		if err := os.MkdirAll(dest, 0o750); err != nil {
			return err
		}
		return os.WriteFile(
			filepath.Join(dest, "SKILL.md"),
			[]byte("---\nname: s\ndescription: d\n---\n\nBody"),
			0o644,
		)
	}
}

// noopCheckout returns a GitCheckout func that does nothing.
func noopCheckout() func(string, string) error {
	return func(_, _ string) error { return nil }
}

func TestParseSkillRef(t *testing.T) {
	tests := []struct {
		input   string
		wantOrg string
		wantN   string
		wantV   string
		wantErr bool
	}{
		{"@anthropic/pdf-processing", "anthropic", "pdf-processing", "", false},
		{"@org/skill@v1.2.0", "org", "skill", "v1.2.0", false},
		{"@AltairaLabs/example-skill@v2.0.0-rc1", "AltairaLabs", "example-skill", "v2.0.0-rc1", false},
		{"@org/name", "org", "name", "", false},
		// errors
		{"no-at-sign/name", "", "", "", true},
		{"@", "", "", "", true},
		{"@org", "", "", "", true},
		{"@org/", "", "", "", true},
		{"@/name", "", "", "", true},
		{"@org/name@", "", "", "", true},
		{"@org/na/me", "", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			ref, err := ParseSkillRef(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.input, err)
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

func TestSkillRefFullName(t *testing.T) {
	ref := SkillRef{Org: "anthropic", Name: "pdf-processing"}
	if got := ref.FullName(); got != "anthropic/pdf-processing" {
		t.Errorf("FullName() = %q, want %q", got, "anthropic/pdf-processing")
	}
}

func TestSkillRefGitURL(t *testing.T) {
	ref := SkillRef{Org: "anthropic", Name: "pdf-processing"}
	want := "https://github.com/anthropic/pdf-processing"
	if got := ref.GitURL(); got != want {
		t.Errorf("GitURL() = %q, want %q", got, want)
	}
}

func TestInstall(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	inst := &Installer{
		UserDir:      userDir,
		ProjectDir:   projectDir,
		GitCloneFunc: fakeGitClone(),
		GitCheckout:  noopCheckout(),
	}

	ref := SkillRef{Org: "testorg", Name: "test-skill"}

	// Install to user-level
	path, err := inst.Install(ref, false)
	if err != nil {
		t.Fatalf("Install() error: %v", err)
	}

	wantPath := filepath.Join(userDir, "testorg", "test-skill")
	if path != wantPath {
		t.Errorf("Install() path = %q, want %q", path, wantPath)
	}

	// Verify SKILL.md exists
	if _, err := os.Stat(filepath.Join(path, "SKILL.md")); err != nil {
		t.Errorf("SKILL.md not found at installed path: %v", err)
	}
}

func TestInstallProjectLevel(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	inst := &Installer{
		UserDir:      userDir,
		ProjectDir:   projectDir,
		GitCloneFunc: fakeGitClone(),
		GitCheckout:  noopCheckout(),
	}

	ref := SkillRef{Org: "org", Name: "skill"}
	path, err := inst.Install(ref, true)
	if err != nil {
		t.Fatalf("Install() error: %v", err)
	}

	wantPath := filepath.Join(projectDir, "org", "skill")
	if path != wantPath {
		t.Errorf("Install() path = %q, want %q", path, wantPath)
	}
}

func TestInstallWithVersion(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	checkoutCalled := false
	checkoutRef := ""

	inst := &Installer{
		UserDir:      userDir,
		ProjectDir:   projectDir,
		GitCloneFunc: fakeGitClone(),
		GitCheckout: func(_ string, ref string) error {
			checkoutCalled = true
			checkoutRef = ref
			return nil
		},
	}

	ref := SkillRef{Org: "org", Name: "skill", Version: "v1.2.0"}
	_, err := inst.Install(ref, false)
	if err != nil {
		t.Fatalf("Install() error: %v", err)
	}

	if !checkoutCalled {
		t.Error("expected GitCheckout to be called")
	}
	if checkoutRef != "v1.2.0" {
		t.Errorf("GitCheckout ref = %q, want %q", checkoutRef, "v1.2.0")
	}
}

func TestInstallAlreadyExists(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	// Pre-create the skill directory.
	existing := filepath.Join(userDir, "org", "skill")
	if err := os.MkdirAll(existing, 0o750); err != nil {
		t.Fatal(err)
	}

	inst := &Installer{
		UserDir:    userDir,
		ProjectDir: projectDir,
	}

	ref := SkillRef{Org: "org", Name: "skill"}
	_, err := inst.Install(ref, false)
	if err == nil {
		t.Fatal("expected error for already-installed skill")
	}
}

func TestInstallNoSkillFile(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	inst := &Installer{
		UserDir:    userDir,
		ProjectDir: projectDir,
		GitCloneFunc: func(_, dest string) error {
			return os.MkdirAll(dest, 0o750) // no SKILL.md
		},
		GitCheckout: noopCheckout(),
	}

	ref := SkillRef{Org: "org", Name: "skill"}
	_, err := inst.Install(ref, false)
	if err == nil {
		t.Fatal("expected error when SKILL.md is missing")
	}

	// Verify cleanup happened.
	destDir := filepath.Join(userDir, "org", "skill")
	if _, statErr := os.Stat(destDir); !os.IsNotExist(statErr) {
		t.Error("expected skill directory to be cleaned up on failure")
	}
}

func TestInstallCloneFailure(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	inst := &Installer{
		UserDir:    userDir,
		ProjectDir: projectDir,
		GitCloneFunc: func(_, _ string) error {
			return fmt.Errorf("git clone failed")
		},
	}

	ref := SkillRef{Org: "org", Name: "skill"}
	_, err := inst.Install(ref, false)
	if err == nil {
		t.Fatal("expected error on clone failure")
	}
}

func TestInstallLocal(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()
	srcDir := t.TempDir()

	// Create a source skill directory.
	skillSrc := filepath.Join(srcDir, "my-skill")
	if err := os.MkdirAll(skillSrc, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillSrc, "SKILL.md"), []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	inst := &Installer{
		UserDir:    userDir,
		ProjectDir: projectDir,
		CopyDirFunc: func(src, dest string) error {
			return defaultCopyDir(src, dest)
		},
	}

	path, err := inst.InstallLocal(skillSrc, false)
	if err != nil {
		t.Fatalf("InstallLocal() error: %v", err)
	}

	wantPath := filepath.Join(userDir, "local", "my-skill")
	if path != wantPath {
		t.Errorf("InstallLocal() path = %q, want %q", path, wantPath)
	}

	// Verify SKILL.md was copied.
	if _, err := os.Stat(filepath.Join(path, "SKILL.md")); err != nil {
		t.Errorf("SKILL.md not found at installed path: %v", err)
	}
}

func TestInstallLocalProjectLevel(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()
	srcDir := t.TempDir()

	skillSrc := filepath.Join(srcDir, "my-skill")
	if err := os.MkdirAll(skillSrc, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillSrc, "SKILL.md"), []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	inst := &Installer{
		UserDir:    userDir,
		ProjectDir: projectDir,
		CopyDirFunc: func(src, dest string) error {
			return defaultCopyDir(src, dest)
		},
	}

	path, err := inst.InstallLocal(skillSrc, true)
	if err != nil {
		t.Fatalf("InstallLocal() error: %v", err)
	}

	wantPath := filepath.Join(projectDir, "local", "my-skill")
	if path != wantPath {
		t.Errorf("InstallLocal() path = %q, want %q", path, wantPath)
	}
}

func TestInstallLocalAlreadyExists(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()
	srcDir := t.TempDir()

	skillSrc := filepath.Join(srcDir, "my-skill")
	if err := os.MkdirAll(skillSrc, 0o750); err != nil {
		t.Fatal(err)
	}

	// Pre-create the destination.
	existing := filepath.Join(userDir, "local", "my-skill")
	if err := os.MkdirAll(existing, 0o750); err != nil {
		t.Fatal(err)
	}

	inst := &Installer{
		UserDir:    userDir,
		ProjectDir: projectDir,
	}

	_, err := inst.InstallLocal(skillSrc, false)
	if err == nil {
		t.Fatal("expected error for already-installed skill")
	}
}

func TestInstallLocalCopyFailure(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()
	srcDir := t.TempDir()

	skillSrc := filepath.Join(srcDir, "my-skill")
	if err := os.MkdirAll(skillSrc, 0o750); err != nil {
		t.Fatal(err)
	}

	inst := &Installer{
		UserDir:    userDir,
		ProjectDir: projectDir,
		CopyDirFunc: func(_, _ string) error {
			return fmt.Errorf("copy failed")
		},
	}

	_, err := inst.InstallLocal(skillSrc, false)
	if err == nil {
		t.Fatal("expected error on copy failure")
	}
}

func TestInstallLocalNotDir(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	// Create a file, not a directory.
	filePath := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(filePath, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	inst := &Installer{
		UserDir:    userDir,
		ProjectDir: projectDir,
	}

	_, err := inst.InstallLocal(filePath, false)
	if err == nil {
		t.Fatal("expected error for non-directory source")
	}
}

func TestRemove(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	// Create an installed skill.
	skillDir := filepath.Join(userDir, "org", "skill")
	if err := os.MkdirAll(skillDir, 0o750); err != nil {
		t.Fatal(err)
	}

	inst := &Installer{
		UserDir:    userDir,
		ProjectDir: projectDir,
	}

	ref := SkillRef{Org: "org", Name: "skill"}
	if err := inst.Remove(ref); err != nil {
		t.Fatalf("Remove() error: %v", err)
	}

	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Error("expected skill directory to be removed")
	}
}

func TestRemoveNotFound(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	inst := &Installer{
		UserDir:    userDir,
		ProjectDir: projectDir,
	}

	ref := SkillRef{Org: "org", Name: "nonexistent"}
	if err := inst.Remove(ref); err == nil {
		t.Fatal("expected error for non-installed skill")
	}
}

func TestList(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	// Create user-level skills.
	for _, path := range []string{
		filepath.Join(userDir, "org-a", "skill-1"),
		filepath.Join(userDir, "org-b", "skill-2"),
	} {
		if err := os.MkdirAll(path, 0o750); err != nil {
			t.Fatal(err)
		}
	}

	// Create project-level skill.
	projSkill := filepath.Join(projectDir, "org-a", "skill-3")
	if err := os.MkdirAll(projSkill, 0o750); err != nil {
		t.Fatal(err)
	}

	inst := &Installer{
		UserDir:    userDir,
		ProjectDir: projectDir,
	}

	skills, err := inst.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}

	if len(skills) != 3 {
		t.Fatalf("expected 3 skills, got %d", len(skills))
	}

	// Project skills come first (alphabetically "project" < "user").
	if skills[0].Location != "project" || skills[0].Name != "skill-3" {
		t.Errorf("first skill = %+v, want project/skill-3", skills[0])
	}
}

func TestListEmpty(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	inst := &Installer{
		UserDir:    userDir,
		ProjectDir: projectDir,
	}

	skills, err := inst.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}

	if len(skills) != 0 {
		t.Errorf("expected 0 skills, got %d", len(skills))
	}
}

func TestResolve(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	// Create skill in both locations — project should win.
	for _, dir := range []string{
		filepath.Join(projectDir, "org", "skill"),
		filepath.Join(userDir, "org", "skill"),
	} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}

	inst := &Installer{
		UserDir:    userDir,
		ProjectDir: projectDir,
	}

	ref := SkillRef{Org: "org", Name: "skill"}
	path, err := inst.Resolve(ref)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	wantPath := filepath.Join(projectDir, "org", "skill")
	if path != wantPath {
		t.Errorf("Resolve() = %q, want %q (project should win)", path, wantPath)
	}
}

func TestResolveUserLevel(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	// Only in user-level.
	skillDir := filepath.Join(userDir, "org", "skill")
	if err := os.MkdirAll(skillDir, 0o750); err != nil {
		t.Fatal(err)
	}

	inst := &Installer{
		UserDir:    userDir,
		ProjectDir: projectDir,
	}

	ref := SkillRef{Org: "org", Name: "skill"}
	path, err := inst.Resolve(ref)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	if path != skillDir {
		t.Errorf("Resolve() = %q, want %q", path, skillDir)
	}
}

func TestResolveNotFound(t *testing.T) {
	inst := &Installer{
		UserDir:    t.TempDir(),
		ProjectDir: t.TempDir(),
	}

	ref := SkillRef{Org: "org", Name: "missing"}
	_, err := inst.Resolve(ref)
	if err == nil {
		t.Fatal("expected error for uninstalled skill")
	}
}

func TestDefaultSkillsUserDir(t *testing.T) {
	// Test with XDG_CONFIG_HOME set.
	t.Setenv("XDG_CONFIG_HOME", "/tmp/test-xdg")
	dir, err := DefaultSkillsUserDir()
	if err != nil {
		t.Fatalf("DefaultSkillsUserDir() error: %v", err)
	}
	want := "/tmp/test-xdg/promptkit/skills"
	if dir != want {
		t.Errorf("DefaultSkillsUserDir() = %q, want %q", dir, want)
	}
}

func TestNewInstaller(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	inst, err := NewInstaller()
	if err != nil {
		t.Fatalf("NewInstaller() error: %v", err)
	}
	if inst.UserDir == "" {
		t.Error("expected UserDir to be set")
	}
	if inst.ProjectDir == "" {
		t.Error("expected ProjectDir to be set")
	}
	if inst.GitCloneFunc == nil {
		t.Error("expected GitCloneFunc to be set")
	}
	if inst.GitCheckout == nil {
		t.Error("expected GitCheckout to be set")
	}
	if inst.CopyDirFunc == nil {
		t.Error("expected CopyDirFunc to be set")
	}
}

func TestDefaultSkillsProjectDir(t *testing.T) {
	dir, err := DefaultSkillsProjectDir()
	if err != nil {
		t.Fatalf("DefaultSkillsProjectDir() error: %v", err)
	}
	if dir == "" {
		t.Error("expected non-empty directory")
	}
	// Should end with .promptkit/skills
	if !strings.HasSuffix(dir, filepath.Join(".promptkit", "skills")) {
		t.Errorf("DefaultSkillsProjectDir() = %q, want suffix .promptkit/skills", dir)
	}
}

func TestDefaultSkillsUserDirFallback(t *testing.T) {
	// Test without XDG_CONFIG_HOME — falls back to ~/.config.
	t.Setenv("XDG_CONFIG_HOME", "")
	dir, err := DefaultSkillsUserDir()
	if err != nil {
		t.Fatalf("DefaultSkillsUserDir() error: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".config", "promptkit", "skills")
	if dir != want {
		t.Errorf("DefaultSkillsUserDir() = %q, want %q", dir, want)
	}
}

func TestIsLocalPath(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"./skills/my-skill", true},
		{"../other/skill", true},
		{"/absolute/path", true},
		{"@org/skill", false},
		{"relative/path", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := IsLocalPath(tt.input); got != tt.want {
				t.Errorf("IsLocalPath(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestInstallInto(t *testing.T) {
	targetDir := t.TempDir()

	inst := &Installer{
		GitCloneFunc: fakeGitClone(),
		GitCheckout:  noopCheckout(),
	}

	ref := SkillRef{Org: "org", Name: "my-skill"}
	path, err := inst.InstallInto(ref, targetDir)
	if err != nil {
		t.Fatalf("InstallInto() error: %v", err)
	}

	wantPath := filepath.Join(targetDir, "my-skill")
	if path != wantPath {
		t.Errorf("InstallInto() path = %q, want %q", path, wantPath)
	}

	if _, err := os.Stat(filepath.Join(path, "SKILL.md")); err != nil {
		t.Errorf("SKILL.md not found at installed path: %v", err)
	}
}

func TestInstallIntoWithVersion(t *testing.T) {
	targetDir := t.TempDir()
	checkoutCalled := false

	inst := &Installer{
		GitCloneFunc: fakeGitClone(),
		GitCheckout: func(_, ref string) error {
			checkoutCalled = true
			if ref != "v2.0.0" {
				t.Errorf("GitCheckout ref = %q, want %q", ref, "v2.0.0")
			}
			return nil
		},
	}

	ref := SkillRef{Org: "org", Name: "skill", Version: "v2.0.0"}
	_, err := inst.InstallInto(ref, targetDir)
	if err != nil {
		t.Fatalf("InstallInto() error: %v", err)
	}
	if !checkoutCalled {
		t.Error("expected GitCheckout to be called")
	}
}

func TestInstallIntoAlreadyExists(t *testing.T) {
	targetDir := t.TempDir()

	// Pre-create the destination.
	existing := filepath.Join(targetDir, "skill")
	if err := os.MkdirAll(existing, 0o750); err != nil {
		t.Fatal(err)
	}

	inst := &Installer{}
	ref := SkillRef{Org: "org", Name: "skill"}
	_, err := inst.InstallInto(ref, targetDir)
	if err == nil {
		t.Fatal("expected error for already-existing skill")
	}
}

func TestInstallIntoCloneFailure(t *testing.T) {
	targetDir := t.TempDir()

	inst := &Installer{
		GitCloneFunc: func(_, _ string) error {
			return fmt.Errorf("git clone failed")
		},
	}

	ref := SkillRef{Org: "org", Name: "skill"}
	_, err := inst.InstallInto(ref, targetDir)
	if err == nil {
		t.Fatal("expected error on clone failure")
	}
}

func TestInstallIntoNoSkillFile(t *testing.T) {
	targetDir := t.TempDir()

	inst := &Installer{
		GitCloneFunc: func(_, dest string) error {
			return os.MkdirAll(dest, 0o750) // no SKILL.md
		},
		GitCheckout: noopCheckout(),
	}

	ref := SkillRef{Org: "org", Name: "skill"}
	_, err := inst.InstallInto(ref, targetDir)
	if err == nil {
		t.Fatal("expected error when SKILL.md is missing")
	}

	// Verify cleanup.
	destDir := filepath.Join(targetDir, "skill")
	if _, statErr := os.Stat(destDir); !os.IsNotExist(statErr) {
		t.Error("expected skill directory to be cleaned up on failure")
	}
}

func TestInstallLocalInto(t *testing.T) {
	srcDir := t.TempDir()
	targetDir := t.TempDir()

	// Create source skill.
	skillSrc := filepath.Join(srcDir, "my-skill")
	if err := os.MkdirAll(skillSrc, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillSrc, "SKILL.md"), []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	inst := &Installer{
		CopyDirFunc: defaultCopyDir,
	}

	path, err := inst.InstallLocalInto(skillSrc, targetDir)
	if err != nil {
		t.Fatalf("InstallLocalInto() error: %v", err)
	}

	wantPath := filepath.Join(targetDir, "my-skill")
	if path != wantPath {
		t.Errorf("InstallLocalInto() path = %q, want %q", path, wantPath)
	}

	if _, err := os.Stat(filepath.Join(path, "SKILL.md")); err != nil {
		t.Errorf("SKILL.md not found at installed path: %v", err)
	}
}

func TestInstallLocalIntoAlreadyExists(t *testing.T) {
	srcDir := t.TempDir()
	targetDir := t.TempDir()

	skillSrc := filepath.Join(srcDir, "skill")
	if err := os.MkdirAll(skillSrc, 0o750); err != nil {
		t.Fatal(err)
	}

	// Pre-create destination.
	if err := os.MkdirAll(filepath.Join(targetDir, "skill"), 0o750); err != nil {
		t.Fatal(err)
	}

	inst := &Installer{}
	_, err := inst.InstallLocalInto(skillSrc, targetDir)
	if err == nil {
		t.Fatal("expected error for already-existing skill")
	}
}

func TestInstallLocalIntoCopyFailure(t *testing.T) {
	srcDir := t.TempDir()
	targetDir := t.TempDir()

	skillSrc := filepath.Join(srcDir, "skill")
	if err := os.MkdirAll(skillSrc, 0o750); err != nil {
		t.Fatal(err)
	}

	inst := &Installer{
		CopyDirFunc: func(_, _ string) error {
			return fmt.Errorf("copy failed")
		},
	}

	_, err := inst.InstallLocalInto(skillSrc, targetDir)
	if err == nil {
		t.Fatal("expected error on copy failure")
	}
}

func TestInstallLocalIntoNotDir(t *testing.T) {
	targetDir := t.TempDir()

	// Create a file, not a directory.
	filePath := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(filePath, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	inst := &Installer{}
	_, err := inst.InstallLocalInto(filePath, targetDir)
	if err == nil {
		t.Fatal("expected error for non-directory source")
	}
}

func TestHasSkillFile(t *testing.T) {
	dir := t.TempDir()

	// No SKILL.md
	if hasSkillFile(dir) {
		t.Error("expected false for empty directory")
	}

	// Add SKILL.md in subdirectory
	subDir := filepath.Join(dir, "sub")
	if err := os.MkdirAll(subDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "SKILL.md"), []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !hasSkillFile(dir) {
		t.Error("expected true when SKILL.md exists in subdirectory")
	}
}
