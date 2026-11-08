package prompt

import (
	"fmt"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"
)

// pathRepo loads fragments by path, as the file repositories do.
type pathOnlyRepo struct {
	*mockRepository
	byPath map[string]string
}

func (r *pathOnlyRepo) LoadFragment(name, relativePath, baseDir string) (*Fragment, error) {
	if relativePath == "" {
		return r.mockRepository.LoadFragment(name, relativePath, baseDir)
	}
	if c, ok := r.byPath[relativePath]; ok {
		return &Fragment{Content: c}, nil
	}
	return nil, fmt.Errorf("no file %s: %w", relativePath, fs.ErrNotExist)
}

// A fragment whose path a variable chooses, under a fixed name, compiled on
// v2.18.0 as its {{persona}} reference. With validation.enum on the variable
// the compiler must not write each candidate under the one name: the second
// candidate's content would conflict with the first.
func TestPackCompiler_FixedNameVariablePathWithEnumCompiles(t *testing.T) {
	repo := &pathOnlyRepo{mockRepository: newMockRepository(), byPath: map[string]string{
		"personas/us.md": "US persona",
		"personas/uk.md": "UK persona",
	}}
	cfg := fragmentConfig("chat", FragmentRef{Name: "persona", Path: "personas/{{region}}.md", Required: true})
	cfg.Spec.SystemTemplate = "{{persona}}"
	cfg.Spec.Variables = []VariableMetadata{
		{Name: "region", Validation: map[string]interface{}{"enum": []interface{}{"us", "uk"}}},
	}
	repo.prompts["chat"] = cfg

	pack, err := compileWithRepo(t, repo, "chat")
	require.NoError(t, err, "a pack that compiled before the enum expansion must still compile")
	// A pack holds one fragment per name, so a fixed name cannot carry a
	// candidate per value: it stays the {{persona}} reference.
	require.Equal(t, map[string]string{"persona": "{{persona}}"}, pack.Fragments)
}

// Optional variant: only personas/us.md exists. The source renders "" for
// region=uk. A pack holding persona="US persona" would serve the US persona
// for every region.
func TestPackCompiler_FixedNameVariablePathDoesNotLeakACandidate(t *testing.T) {
	repo := &pathOnlyRepo{mockRepository: newMockRepository(), byPath: map[string]string{
		"personas/us.md": "US persona",
	}}
	cfg := fragmentConfig("chat", FragmentRef{Name: "persona", Path: "personas/{{region}}.md"})
	cfg.Spec.SystemTemplate = "{{persona}}"
	cfg.Spec.Variables = []VariableMetadata{
		{Name: "region", Validation: map[string]interface{}{"enum": []interface{}{"us", "uk"}}},
	}
	repo.prompts["chat"] = cfg

	pack, err := compileWithRepo(t, repo, "chat")
	require.NoError(t, err)
	require.NotEqual(t, "US persona", pack.Fragments["persona"],
		"one candidate's content became the fragment for every value of region")
}

// A name that varies with only some of its path's variables cannot give each
// candidate a name of its own either: it stays the reference.
func TestPackCompiler_NameMissingAPathVariableStaysReference(t *testing.T) {
	repo := &pathOnlyRepo{mockRepository: newMockRepository(), byPath: map[string]string{
		"en/us.md": "en us", "fr/us.md": "fr us",
	}}
	cfg := fragmentConfig("chat", FragmentRef{Name: "persona_{{region}}", Path: "{{lang}}/{{region}}.md", Required: true})
	cfg.Spec.Variables = []VariableMetadata{
		{Name: "region", Validation: map[string]interface{}{"enum": []interface{}{"us"}}},
		{Name: "lang", Validation: map[string]interface{}{"enum": []interface{}{"en", "fr"}}},
	}
	repo.prompts["chat"] = cfg

	pack, err := compileWithRepo(t, repo, "chat")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"persona_{{region}}": "{{persona_{{region}}}}"}, pack.Fragments)
}

// Where the name carries every path variable, each candidate key holds its
// own file's content.
func TestPackCompiler_NameVaryingPathCandidatesKeepTheirContent(t *testing.T) {
	repo := &pathOnlyRepo{mockRepository: newMockRepository(), byPath: map[string]string{
		"personas/us.md": "US persona", "personas/uk.md": "UK persona",
	}}
	cfg := fragmentConfig("chat", FragmentRef{Name: "persona_{{region}}", Path: "personas/{{region}}.md", Required: true})
	cfg.Spec.Variables = []VariableMetadata{
		{Name: "region", Validation: map[string]interface{}{"enum": []interface{}{"us", "uk"}}},
	}
	repo.prompts["chat"] = cfg

	pack, err := compileWithRepo(t, repo, "chat")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"persona_us": "US persona", "persona_uk": "UK persona"}, pack.Fragments)
}
