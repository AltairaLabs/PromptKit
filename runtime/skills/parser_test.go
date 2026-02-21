package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validSkillMD = `---
name: code-review
description: Reviews code for best practices
license: MIT
compatibility: ">=1.0.0"
metadata:
  tags: "go,python"
  author: "test-author"
allowed-tools:
  - file-read
  - file-write
---

## Instructions

Review the code carefully and provide feedback.
`

const minimalSkillMD = `---
name: simple-skill
description: A simple skill
---

Do the thing.
`

const frontmatterOnlySkillMD = `---
name: no-body
description: Skill with no body
---
`

func TestParseSkillContent_AllFields(t *testing.T) {
	meta, body, err := ParseSkillContent([]byte(validSkillMD))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if meta.Name != "code-review" {
		t.Errorf("expected name %q, got %q", "code-review", meta.Name)
	}
	if meta.Description != "Reviews code for best practices" {
		t.Errorf("expected description %q, got %q", "Reviews code for best practices", meta.Description)
	}
	if meta.License != "MIT" {
		t.Errorf("expected license %q, got %q", "MIT", meta.License)
	}
	if meta.Compatibility != ">=1.0.0" {
		t.Errorf("expected compatibility %q, got %q", ">=1.0.0", meta.Compatibility)
	}
	if len(meta.Metadata) != 2 {
		t.Errorf("expected 2 metadata entries, got %d", len(meta.Metadata))
	}
	if meta.Metadata["tags"] != "go,python" {
		t.Errorf("expected tags %q, got %q", "go,python", meta.Metadata["tags"])
	}
	if meta.Metadata["author"] != "test-author" {
		t.Errorf("expected author %q, got %q", "test-author", meta.Metadata["author"])
	}
	if len(meta.AllowedTools) != 2 {
		t.Fatalf("expected 2 allowed tools, got %d", len(meta.AllowedTools))
	}
	if meta.AllowedTools[0] != "file-read" {
		t.Errorf("expected allowed tool %q, got %q", "file-read", meta.AllowedTools[0])
	}
	if meta.AllowedTools[1] != "file-write" {
		t.Errorf("expected allowed tool %q, got %q", "file-write", meta.AllowedTools[1])
	}
	if !strings.Contains(body, "Review the code carefully") {
		t.Errorf("expected body to contain instructions, got %q", body)
	}
}

func TestParseSkillContent_MinimalFields(t *testing.T) {
	meta, body, err := ParseSkillContent([]byte(minimalSkillMD))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if meta.Name != "simple-skill" {
		t.Errorf("expected name %q, got %q", "simple-skill", meta.Name)
	}
	if meta.Description != "A simple skill" {
		t.Errorf("expected description %q, got %q", "A simple skill", meta.Description)
	}
	if meta.License != "" {
		t.Errorf("expected empty license, got %q", meta.License)
	}
	if len(meta.Metadata) != 0 {
		t.Errorf("expected no metadata, got %d entries", len(meta.Metadata))
	}
	if len(meta.AllowedTools) != 0 {
		t.Errorf("expected no allowed tools, got %d", len(meta.AllowedTools))
	}
	if body != "Do the thing." {
		t.Errorf("expected body %q, got %q", "Do the thing.", body)
	}
}

func TestParseSkillContent_FrontmatterOnly(t *testing.T) {
	meta, body, err := ParseSkillContent([]byte(frontmatterOnlySkillMD))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if meta.Name != "no-body" {
		t.Errorf("expected name %q, got %q", "no-body", meta.Name)
	}
	if body != "" {
		t.Errorf("expected empty body, got %q", body)
	}
}

func TestParseSkillContent_MissingName(t *testing.T) {
	content := `---
description: No name provided
---

Body text.
`
	_, _, err := ParseSkillContent([]byte(content))
	if err == nil {
		t.Fatal("expected error for missing name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("expected 'name is required' error, got: %v", err)
	}
}

func TestParseSkillContent_MissingDescription(t *testing.T) {
	content := `---
name: no-desc
---

Body text.
`
	_, _, err := ParseSkillContent([]byte(content))
	if err == nil {
		t.Fatal("expected error for missing description")
	}
	if !strings.Contains(err.Error(), "description is required") {
		t.Errorf("expected 'description is required' error, got: %v", err)
	}
}

func TestParseSkillContent_NoFrontmatter(t *testing.T) {
	content := `Just some markdown without frontmatter.`
	_, _, err := ParseSkillContent([]byte(content))
	if err == nil {
		t.Fatal("expected error for missing frontmatter")
	}
	if !strings.Contains(err.Error(), "missing frontmatter") {
		t.Errorf("expected 'missing frontmatter' error, got: %v", err)
	}
}

func TestParseSkillContent_EmptyContent(t *testing.T) {
	_, _, err := ParseSkillContent([]byte(""))
	if err == nil {
		t.Fatal("expected error for empty content")
	}
	if !strings.Contains(err.Error(), "empty skill content") {
		t.Errorf("expected 'empty skill content' error, got: %v", err)
	}
}

func TestParseSkillContent_MissingClosingDelimiter(t *testing.T) {
	content := `---
name: broken
description: No closing delimiter
`
	_, _, err := ParseSkillContent([]byte(content))
	if err == nil {
		t.Fatal("expected error for missing closing delimiter")
	}
	if !strings.Contains(err.Error(), "missing closing frontmatter delimiter") {
		t.Errorf("expected closing delimiter error, got: %v", err)
	}
}

func TestParseSkillContent_InvalidYAML(t *testing.T) {
	content := `---
name: [invalid yaml
description: broken
---

Body.
`
	_, _, err := ParseSkillContent([]byte(content))
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	if !strings.Contains(err.Error(), "invalid YAML frontmatter") {
		t.Errorf("expected YAML error, got: %v", err)
	}
}

func TestParseSkillContent_NameValidation(t *testing.T) {
	tests := []struct {
		name    string
		skill   string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid simple name",
			skill:   "---\nname: myskill\ndescription: test\n---\n",
			wantErr: false,
		},
		{
			name:    "valid kebab-case",
			skill:   "---\nname: my-cool-skill\ndescription: test\n---\n",
			wantErr: false,
		},
		{
			name:    "valid with numbers",
			skill:   "---\nname: skill-v2\ndescription: test\n---\n",
			wantErr: false,
		},
		{
			name:    "uppercase letters",
			skill:   "---\nname: MySkill\ndescription: test\n---\n",
			wantErr: true,
			errMsg:  "must be kebab-case",
		},
		{
			name:    "spaces in name",
			skill:   "---\nname: my skill\ndescription: test\n---\n",
			wantErr: true,
			errMsg:  "must be kebab-case",
		},
		{
			name:    "underscores in name",
			skill:   "---\nname: my_skill\ndescription: test\n---\n",
			wantErr: true,
			errMsg:  "must be kebab-case",
		},
		{
			name:    "leading hyphen",
			skill:   "---\nname: -skill\ndescription: test\n---\n",
			wantErr: true,
			errMsg:  "must be kebab-case",
		},
		{
			name:    "trailing hyphen",
			skill:   "---\nname: skill-\ndescription: test\n---\n",
			wantErr: true,
			errMsg:  "must be kebab-case",
		},
		{
			name:    "consecutive hyphens",
			skill:   "---\nname: my--skill\ndescription: test\n---\n",
			wantErr: true,
			errMsg:  "must be kebab-case",
		},
		{
			name: "name too long",
			skill: "---\nname: " + strings.Repeat("a", 65) +
				"\ndescription: test\n---\n",
			wantErr: true,
			errMsg:  "exceeds maximum length",
		},
		{
			name: "name at max length",
			skill: "---\nname: " + strings.Repeat("a", 64) +
				"\ndescription: test\n---\n",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := ParseSkillContent([]byte(tt.skill))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got: %v", tt.errMsg, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestParseSkillContent_DescriptionTooLong(t *testing.T) {
	longDesc := strings.Repeat("a", 1025)
	content := "---\nname: test-skill\ndescription: " + longDesc + "\n---\n"
	_, _, err := ParseSkillContent([]byte(content))
	if err == nil {
		t.Fatal("expected error for description too long")
	}
	if !strings.Contains(err.Error(), "exceeds maximum length") {
		t.Errorf("expected max length error, got: %v", err)
	}
}

func TestParseSkillContent_DescriptionAtMaxLength(t *testing.T) {
	desc := strings.Repeat("a", 1024)
	content := "---\nname: test-skill\ndescription: " + desc + "\n---\n"
	meta, _, err := ParseSkillContent([]byte(content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(meta.Description) != 1024 {
		t.Errorf("expected description length 1024, got %d", len(meta.Description))
	}
}

func TestParseSkillFile(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "my-skill")
	if err := os.Mkdir(skillDir, 0o755); err != nil {
		t.Fatalf("creating skill dir: %v", err)
	}

	skillPath := filepath.Join(skillDir, "SKILL.md")
	err := os.WriteFile(skillPath, []byte(validSkillMD), 0o644)
	if err != nil {
		t.Fatalf("writing skill file: %v", err)
	}

	skill, err := ParseSkillFile(skillPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if skill.Name != "code-review" {
		t.Errorf("expected name %q, got %q", "code-review", skill.Name)
	}
	if skill.Path != skillDir {
		t.Errorf("expected path %q, got %q", skillDir, skill.Path)
	}
	if !strings.Contains(skill.Instructions, "Review the code carefully") {
		t.Errorf("expected instructions to contain review text, got %q", skill.Instructions)
	}
}

func TestParseSkillFile_NotFound(t *testing.T) {
	_, err := ParseSkillFile("/nonexistent/SKILL.md")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	if !strings.Contains(err.Error(), "reading skill file") {
		t.Errorf("expected reading error, got: %v", err)
	}
}

func TestParseSkillMetadata(t *testing.T) {
	dir := t.TempDir()
	skillPath := filepath.Join(dir, "SKILL.md")
	err := os.WriteFile(skillPath, []byte(validSkillMD), 0o644)
	if err != nil {
		t.Fatalf("writing skill file: %v", err)
	}

	meta, err := ParseSkillMetadata(skillPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if meta.Name != "code-review" {
		t.Errorf("expected name %q, got %q", "code-review", meta.Name)
	}
	if meta.Description != "Reviews code for best practices" {
		t.Errorf("expected description %q, got %q",
			"Reviews code for best practices", meta.Description)
	}
}

func TestParseSkillMetadata_NotFound(t *testing.T) {
	_, err := ParseSkillMetadata("/nonexistent/SKILL.md")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestParseSkillFile_InvalidContent(t *testing.T) {
	dir := t.TempDir()
	skillPath := filepath.Join(dir, "SKILL.md")
	err := os.WriteFile(skillPath, []byte("no frontmatter here"), 0o644)
	if err != nil {
		t.Fatalf("writing skill file: %v", err)
	}

	_, err = ParseSkillFile(skillPath)
	if err == nil {
		t.Fatal("expected error for invalid content")
	}
}

func TestParseSkillMetadata_InvalidContent(t *testing.T) {
	dir := t.TempDir()
	skillPath := filepath.Join(dir, "SKILL.md")
	err := os.WriteFile(skillPath, []byte("no frontmatter here"), 0o644)
	if err != nil {
		t.Fatalf("writing skill file: %v", err)
	}

	_, err = ParseSkillMetadata(skillPath)
	if err == nil {
		t.Fatal("expected error for invalid content")
	}
}

func TestParseSkillContent_WhitespaceHandling(t *testing.T) {
	// Content with extra whitespace around body
	content := `---
name: whitespace-test
description: Test whitespace handling
---


  Some body with whitespace

`
	meta, body, err := ParseSkillContent([]byte(content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.Name != "whitespace-test" {
		t.Errorf("expected name %q, got %q", "whitespace-test", meta.Name)
	}
	// Body should be trimmed
	if body != "Some body with whitespace" {
		t.Errorf("expected trimmed body %q, got %q", "Some body with whitespace", body)
	}
}
