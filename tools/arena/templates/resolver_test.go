package templates

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRepoResolver(t *testing.T) {
	cfg := &RepoConfig{
		Repos: map[string]Repository{
			"test": {Type: RepositoryTypeRemoteGit, URL: "https://example.com"},
		},
	}
	resolver := NewRepoResolver(cfg)
	assert.NotNil(t, resolver)
	assert.Equal(t, cfg, resolver.config)
}

func TestParseTemplateRef_WithVersionAndRepo(t *testing.T) {
	cfg := &RepoConfig{
		Repos: map[string]Repository{
			"custom": {Type: RepositoryTypeRemoteGit, URL: "https://custom.com/index.yaml"},
		},
	}
	resolver := NewRepoResolver(cfg)

	ref, err := resolver.ParseTemplateRef("custom/mytemplate@1.0.0", "")
	require.NoError(t, err)
	assert.Equal(t, "custom", ref.RepoName)
	assert.Equal(t, "mytemplate", ref.TemplateName)
	assert.Equal(t, "1.0.0", ref.Version)
	assert.Equal(t, "https://custom.com/index.yaml", ref.Repository.URL)
}

func TestParseTemplateRef_NoRepoUsesDefault(t *testing.T) {
	cfg := &RepoConfig{
		Repos: map[string]Repository{
			DefaultRepoName: {Type: RepositoryTypeRemoteGit, URL: DefaultGitHubIndex},
		},
	}
	resolver := NewRepoResolver(cfg)

	ref, err := resolver.ParseTemplateRef("mytemplate@2.0.0", "")
	require.NoError(t, err)
	assert.Equal(t, DefaultRepoName, ref.RepoName)
	assert.Equal(t, "mytemplate", ref.TemplateName)
	assert.Equal(t, "2.0.0", ref.Version)
}

func TestParseTemplateRef_CustomDefaultRepo(t *testing.T) {
	cfg := &RepoConfig{
		Repos: map[string]Repository{
			"internal": {Type: RepositoryTypeLocal, URL: "/opt/templates"},
		},
	}
	resolver := NewRepoResolver(cfg)

	ref, err := resolver.ParseTemplateRef("mytemplate", "internal")
	require.NoError(t, err)
	assert.Equal(t, "internal", ref.RepoName)
	assert.Equal(t, "mytemplate", ref.TemplateName)
	assert.Equal(t, "", ref.Version)
	assert.Equal(t, "/opt/templates", ref.Repository.URL)
}

func TestParseTemplateRef_NoVersion(t *testing.T) {
	cfg := &RepoConfig{
		Repos: map[string]Repository{
			"test": {Type: RepositoryTypeRemoteGit, URL: "https://test.com"},
		},
	}
	resolver := NewRepoResolver(cfg)

	ref, err := resolver.ParseTemplateRef("test/template", "")
	require.NoError(t, err)
	assert.Equal(t, "test", ref.RepoName)
	assert.Equal(t, "template", ref.TemplateName)
	assert.Equal(t, "", ref.Version)
}

func TestParseTemplateRef_EmptyRepoPartIgnored(t *testing.T) {
	cfg := &RepoConfig{
		Repos: map[string]Repository{
			DefaultRepoName: {Type: RepositoryTypeRemoteGit, URL: DefaultGitHubIndex},
		},
	}
	resolver := NewRepoResolver(cfg)

	// Edge case: "/" at start means no repo prefix
	ref, err := resolver.ParseTemplateRef("/template", "")
	require.NoError(t, err)
	assert.Equal(t, DefaultRepoName, ref.RepoName)
	assert.Equal(t, "/template", ref.TemplateName)
}

func TestGetCachePath(t *testing.T) {
	resolver := NewRepoResolver(nil)
	ref := &TemplateRef{
		RepoName:     "custom",
		TemplateName: "mytemplate",
		Version:      "1.2.3",
	}

	path := resolver.GetCachePath("/cache", ref)
	expected := filepath.Join("/cache", "custom", "mytemplate", "1.2.3", "template.yaml")
	assert.Equal(t, expected, path)
}

func TestGetCachePath_NoVersion(t *testing.T) {
	resolver := NewRepoResolver(nil)
	ref := &TemplateRef{
		RepoName:     "local",
		TemplateName: "test",
		Version:      "",
	}

	path := resolver.GetCachePath("/tmp/cache", ref)
	expected := filepath.Join("/tmp/cache", "local", "test", "", "template.yaml")
	assert.Equal(t, expected, path)
}

func TestGetRepoDirForType_WithName(t *testing.T) {
	result := GetRepoDirForType(RepositoryTypeRemoteGit, "myrepo")
	assert.Equal(t, "myrepo", result)
}

func TestGetRepoDirForType_LocalFallback(t *testing.T) {
	result := GetRepoDirForType(RepositoryTypeLocal, "")
	assert.Equal(t, "local", result)
}

func TestGetRepoDirForType_RemoteFallback(t *testing.T) {
	result := GetRepoDirForType(RepositoryTypeRemoteGit, "")
	assert.Equal(t, "default", result)
}

func TestSplitTemplateRef_WithRepo(t *testing.T) {
	repo, name := SplitTemplateRef("myrepo/template")
	assert.Equal(t, "myrepo", repo)
	assert.Equal(t, "template", name)
}

func TestSplitTemplateRef_NoRepo(t *testing.T) {
	repo, name := SplitTemplateRef("template")
	assert.Equal(t, "", repo)
	assert.Equal(t, "template", name)
}

func TestSplitTemplateRef_FilePath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantRepo string
		wantName string
	}{
		{"relative current", "./template", "", "./template"},
		{"relative parent", "../template", "", "../template"},
		{"absolute", "/path/to/template", "", "/path/to/template"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, name := SplitTemplateRef(tt.input)
			assert.Equal(t, tt.wantRepo, repo)
			assert.Equal(t, tt.wantName, name)
		})
	}
}

func TestSplitTemplateRef_EmptyParts(t *testing.T) {
	// Edge cases with empty parts
	repo, name := SplitTemplateRef("/template")
	assert.Equal(t, "", repo)
	assert.Equal(t, "/template", name)

	repo, name = SplitTemplateRef("repo/")
	assert.Equal(t, "", repo)
	assert.Equal(t, "repo/", name)
}

func TestResolveRepoForTemplate_WithPrefix(t *testing.T) {
	cfg := &RepoConfig{
		Repos: map[string]Repository{
			"custom": {Type: RepositoryTypeLocal, URL: "/custom/path"},
		},
	}
	resolver := NewRepoResolver(cfg)

	repo, cleanName := resolver.ResolveRepoForTemplate("custom/template", "default")
	assert.Equal(t, "/custom/path", repo.URL)
	assert.Equal(t, "custom", cleanName)
}

func TestResolveRepoForTemplate_NoPrefix(t *testing.T) {
	cfg := &RepoConfig{
		Repos: map[string]Repository{
			"default": {Type: RepositoryTypeRemoteGit, URL: "https://default.com"},
		},
	}
	resolver := NewRepoResolver(cfg)

	repo, cleanName := resolver.ResolveRepoForTemplate("template", "default")
	assert.Equal(t, "https://default.com", repo.URL)
	assert.Equal(t, "default", cleanName)
}

func TestResolveRepoForTemplate_EmptyDefault(t *testing.T) {
	cfg := &RepoConfig{
		Repos: map[string]Repository{
			DefaultRepoName: {Type: RepositoryTypeRemoteGit, URL: DefaultGitHubIndex},
		},
	}
	resolver := NewRepoResolver(cfg)

	repo, cleanName := resolver.ResolveRepoForTemplate("template", "")
	assert.Equal(t, DefaultGitHubIndex, repo.URL)
	assert.Equal(t, DefaultRepoName, cleanName)
}

func TestParseTemplateRef_MultipleSlashes(t *testing.T) {
	cfg := &RepoConfig{
		Repos: map[string]Repository{
			DefaultRepoName: {Type: RepositoryTypeRemoteGit, URL: DefaultGitHubIndex},
		},
	}
	resolver := NewRepoResolver(cfg)

	// Should only split on first slash
	ref, err := resolver.ParseTemplateRef("repo/sub/template@1.0", "")
	require.NoError(t, err)
	assert.Equal(t, "repo", ref.RepoName)
	assert.Equal(t, "sub/template", ref.TemplateName)
	assert.Equal(t, "1.0", ref.Version)
}
