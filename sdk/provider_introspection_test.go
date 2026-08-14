package sdk

import (
	"slices"
	"testing"

	pkgconfig "github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
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

// The Bedrock embedding provider (#1300) is only reachable if the SDK links it
// in. Without the blank import it exists but no SDK caller can construct it,
// which is the exact shape of the gap #1774 reported.
func TestRegisteredProviderTypes_IncludesBedrockEmbedding(t *testing.T) {
	assert.Contains(t, RegisteredProviderTypes()[pkgconfig.RoleEmbedding], "bedrock")
}

// The binding #1774 reported as broken must now validate — and the platform
// block must be what makes the difference. Bedrock has no API-key mode, so a
// spec without one has no SigV4 signer and every request would go unsigned;
// accepting it would be worse than the original error.
func TestValidateOptions_AcceptsBedrockEmbeddingOnlyWithPlatform(t *testing.T) {
	withPlatform := func(p *pkgconfig.PlatformConfig) []Option {
		return []Option{
			WithContextWindow(10),
			WithStateStore(statestore.NewMemoryStore()),
			WithEmbeddingProvider(ProviderSpec{
				ID: "embed", Type: "bedrock",
				Model: "amazon.titan-embed-text-v2:0", Platform: p,
			}),
		}
	}

	assert.NoError(t,
		ValidateOptions(withPlatform(&pkgconfig.PlatformConfig{Type: "bedrock", Region: "us-west-2"})...),
		"a Bedrock embedding binding must validate")

	err := ValidateOptions(withPlatform(nil)...)
	require.Error(t, err, "without a platform block there is no SigV4 signer")
	assert.Contains(t, err.Error(), "platform")
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

// TestValidateOptions_TracksRegisteredProviderTypes pins the contract the two
// APIs jointly promise: registration is the only gate on construction, so what
// RegisteredProviderTypes lists is exactly what ValidateOptions accepts. The
// two calls differ only in the provider type, so acceptance cannot be
// unconditional — a ValidateOptions that ignored the type, always returned nil,
// or always errored fails one of the three assertions, and a lister that drifts
// from the validator fails the requires.
func TestValidateOptions_TracksRegisteredProviderTypes(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")

	listed := RegisteredProviderTypes()[pkgconfig.RoleTTS]
	require.Contains(t, listed, "openai")
	require.NotContains(t, listed, "polly", "test needs a type that is genuinely unregistered")

	assert.NoError(t, ValidateOptions(
		WithTTSProvider(ProviderSpec{ID: "voice", Type: "openai"}),
		WithSTTProvider(ProviderSpec{ID: "ears", Type: "openai"}),
	), "every listed type must validate")

	err := ValidateOptions(WithTTSProvider(ProviderSpec{ID: "voice", Type: "polly"}))
	require.Error(t, err, "an unlisted type must not validate")
	assert.Contains(t, err.Error(), "polly", "error must name the offending type")
}

// TestValidateOptions_ChecksConstructionNotCredentials pins the documented
// boundary of what validation covers. Both calls carry an unusable credential;
// only the one naming an unregistered type is rejected. A caller gating a
// deploy on ValidateOptions depends on knowing which side of this line it sits
// on, so the limitation is pinned rather than left to the doc comment.
func TestValidateOptions_ChecksConstructionNotCredentials(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-not-a-real-key")

	assert.NoError(t,
		ValidateOptions(WithTTSProvider(ProviderSpec{ID: "voice", Type: "openai"})),
		"an unusable credential must still validate — validity is not checked")

	// "transcribe" is the STT type a Bedrock deployment would reach for (#1774).
	err := ValidateOptions(WithSTTProvider(ProviderSpec{ID: "ears", Type: "transcribe"}))
	require.Error(t, err, "an unregistered type must be rejected")
	assert.Contains(t, err.Error(), "transcribe")
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
