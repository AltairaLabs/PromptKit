package sdk

import (
	"slices"
	"testing"

	pkgconfig "github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisteredProviderTypes_CoversEveryRole(t *testing.T) {
	got := RegisteredProviderTypes()

	wantRoles := []string{
		pkgconfig.RoleLLM, pkgconfig.RoleImage, pkgconfig.RoleVideo,
		pkgconfig.RoleTTS, pkgconfig.RoleSTT,
		pkgconfig.RoleEmbedding, pkgconfig.RoleInference,
	}
	gotRoles := make([]string, 0, len(got))
	for role := range got {
		gotRoles = append(gotRoles, role)
	}
	slices.Sort(wantRoles)
	slices.Sort(gotRoles)
	assert.Equal(t, wantRoles, gotRoles,
		"every role applyProviderConfig routes must be listed, and no others")
}

// Each role must read its own registry. Asserting a type unique to one
// capability catches a lister wired to the wrong registry — the TTS and STT
// sets overlap on "openai", so only "cartesia" distinguishes them.
func TestRegisteredProviderTypes_RolesReadTheirOwnRegistry(t *testing.T) {
	got := RegisteredProviderTypes()

	assert.Contains(t, got[pkgconfig.RoleTTS], "cartesia")
	assert.NotContains(t, got[pkgconfig.RoleSTT], "cartesia",
		"STT must not report TTS-only types")
	assert.Contains(t, got[pkgconfig.RoleSTT], "openai")

	// Registered by the blank imports in runtime_config.go.
	assert.Contains(t, got[pkgconfig.RoleEmbedding], "voyageai")
	assert.NotContains(t, got[pkgconfig.RoleLLM], "voyageai",
		"completion role must not report embedding-only types")
	assert.Contains(t, got[pkgconfig.RoleLLM], "openai")
}

// llm/image/video all resolve against the same completion-provider registry,
// so they must report identical sets rather than silently diverging.
func TestRegisteredProviderTypes_CompletionRolesShareOneRegistry(t *testing.T) {
	got := RegisteredProviderTypes()
	assert.Equal(t, got[pkgconfig.RoleLLM], got[pkgconfig.RoleImage])
	assert.Equal(t, got[pkgconfig.RoleLLM], got[pkgconfig.RoleVideo])
}

func TestRegisteredProviderTypes_SortedPerRole(t *testing.T) {
	for role, types := range RegisteredProviderTypes() {
		assert.True(t, slices.IsSorted(types), "role %q: %v not sorted", role, types)
	}
}

// The caller must not be able to corrupt the registries through the returned
// map — it is a snapshot, not a view.
func TestRegisteredProviderTypes_ReturnsIndependentCopy(t *testing.T) {
	first := RegisteredProviderTypes()
	require.NotEmpty(t, first[pkgconfig.RoleLLM])
	first[pkgconfig.RoleLLM][0] = "mutated"
	delete(first, pkgconfig.RoleTTS)

	second := RegisteredProviderTypes()
	assert.NotEqual(t, "mutated", second[pkgconfig.RoleLLM][0])
	assert.NotEmpty(t, second[pkgconfig.RoleTTS])
}

func TestValidateOptions_RejectsUnregisteredProviderTypeWithoutAPack(t *testing.T) {
	// The point of the call: no pack path, no conversation, no request.
	err := ValidateOptions(WithEmbeddingProvider(ProviderSpec{
		ID:   "embed",
		Type: "titan",
		// Platform set exactly as a Bedrock deployment would; it must not
		// rescue an unregistered type.
		Platform: &pkgconfig.PlatformConfig{Type: "bedrock", Region: "us-west-2"},
	}))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "titan", "error must name the offending type")
}

func TestValidateOptions_AcceptsRegisteredProviderSet(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")

	err := ValidateOptions(
		WithTTSProvider(ProviderSpec{ID: "voice", Type: "openai"}),
		WithSTTProvider(ProviderSpec{ID: "ears", Type: "openai"}),
	)

	assert.NoError(t, err)
}

// Validation must apply the same cross-option checks Open does, or a set that
// passes here would still fail at request time.
func TestValidateOptions_AppliesCrossOptionChecks(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")

	// The type is registered and constructs fine; the set is still invalid,
	// because an embedding provider sets the retrieval slot and that requires
	// a context window. Open enforces this after applying options — if
	// validation stopped at per-option construction it would pass here and
	// fail at request time.
	err := ValidateOptions(WithEmbeddingProvider(ProviderSpec{ID: "embed", Type: "openai"}))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "WithContextWindow")
}
