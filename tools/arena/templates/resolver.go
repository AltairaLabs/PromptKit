package templates

import (
	"path/filepath"
	"strings"
)

// RepoResolver handles repository resolution and template reference parsing.
type RepoResolver struct {
	config *RepoConfig
}

// NewRepoResolver creates a new repository resolver with the given config.
func NewRepoResolver(config *RepoConfig) *RepoResolver {
	return &RepoResolver{config: config}
}

// TemplateRef represents a parsed template reference.
type TemplateRef struct {
	RepoName     string     // e.g., "local", "community"
	TemplateName string     // e.g., "iot-maintenance-demo"
	Version      string     // e.g., "1.0.0" or empty
	Repository   Repository // The resolved repository
}

// ParseTemplateRef parses a template reference like "local/template@version" or "template@version".
// If no repo prefix is present, uses the provided defaultRepo.
func (r *RepoResolver) ParseTemplateRef(ref, defaultRepo string) (*TemplateRef, error) {
	result := &TemplateRef{}

	// Extract version if present (name@version)
	name := ref
	if idx := strings.LastIndex(ref, "@"); idx > 0 {
		result.Version = ref[idx+1:]
		name = ref[:idx]
	}

	// Extract repo prefix if present (repo/name)
	const maxParts = 2
	parts := strings.SplitN(name, "/", maxParts)
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		// Has repo prefix
		result.RepoName = parts[0]
		result.TemplateName = parts[1]
	} else {
		// No repo prefix, use default
		result.RepoName = defaultRepo
		if result.RepoName == "" {
			result.RepoName = DefaultRepoName
		}
		result.TemplateName = name
	}

	// Resolve the repository
	result.Repository = ResolveIndex(result.RepoName, r.config)

	return result, nil
}

// GetCachePath returns the cache path for a template within a repository.
func (r *RepoResolver) GetCachePath(cacheDir string, ref *TemplateRef) string {
	return filepath.Join(cacheDir, ref.RepoName, ref.TemplateName, ref.Version, "template.yaml")
}

// GetRepoDirForType returns a repo name suitable for use in cache paths.
// For unknown repos, returns a sanitized version of the URL/path.
func GetRepoDirForType(repoType RepositoryType, repoName string) string {
	if repoName != "" {
		return repoName
	}
	// Fallback based on type
	if repoType == RepositoryTypeLocal {
		return "local"
	}
	return "default"
}

// SplitTemplateRef is a standalone utility that splits "repo/template" into parts.
// Returns empty repo string if no repo prefix is present.
func SplitTemplateRef(ref string) (repo, name string) {
	const maxParts = 2
	parts := strings.SplitN(ref, "/", maxParts)
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		// Check if this looks like a file path (starts with . or /)
		if strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "../") {
			return "", ref
		}
		return parts[0], parts[1]
	}
	return "", ref
}

// ResolveRepoForTemplate determines which repository to use for a template reference.
// It checks if the template name has a repo prefix and uses that, otherwise uses defaultRepo.
func (r *RepoResolver) ResolveRepoForTemplate(
	templateName, defaultRepo string,
) (repo Repository, cleanName string) {
	repoName := defaultRepo
	if repo, _ := SplitTemplateRef(templateName); repo != "" {
		repoName = repo
	}
	if repoName == "" {
		repoName = DefaultRepoName
	}
	return ResolveIndex(repoName, r.config), repoName
}
