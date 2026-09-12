package sdk

import (
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
)

func requiring(providers ...*packspec.ProviderRequirement) *prompt.Pack {
	return &prompt.Pack{Pack: packspec.Pack{Requires: &prompt.Requires{Providers: providers}}}
}

func shorthand(key string) *packspec.ProviderRequirement {
	return &packspec.ProviderRequirement{Shorthand: key}
}

// TestUnsatisfiedRequiredProviderFailsOpen is the behaviour RFC 0012 asks of a
// runtime, and the whole reason for carrying the declaration: the failure moves
// from the first request that needs a provider to the moment the pack is
// opened.
func TestUnsatisfiedRequiredProviderFailsOpen(t *testing.T) {
	p := requiring(&packspec.ProviderRequirement{Key: "embeddings", Role: "embedding"})
	cfg := &config{}

	err := checkProviderRequirements(p, cfg)
	if err == nil {
		t.Fatal("an unsatisfied required provider must fail")
	}
	if !strings.Contains(err.Error(), "embeddings") {
		t.Errorf("the error must name the missing key so an operator can act: %v", err)
	}
}

// TestUnsatisfiedOptionalProviderDoesNotFail — the spec draws the line here:
// required is an error, optional is a warning. Treating optional as fatal would
// make a judge-model declaration unusable in environments that do not run evals.
func TestUnsatisfiedOptionalProviderDoesNotFail(t *testing.T) {
	optional := false
	p := requiring(&packspec.ProviderRequirement{
		Key: "judge", Role: "llm", Required: &optional,
	})
	if err := checkProviderRequirements(p, &config{}); err != nil {
		t.Errorf("an unsatisfied OPTIONAL provider must not fail Open: %v", err)
	}
}

// TestDefaultKeyIsSatisfiedByTheAgentProvider — `- default` is the RFC's first
// example. It has to be satisfied by an ordinary WithProvider call, otherwise
// the simplest possible declaration breaks the simplest possible setup.
func TestDefaultKeyIsSatisfiedByTheAgentProvider(t *testing.T) {
	cfg := &config{agentSet: true, agentProviderID: "openai-gpt4"}
	if err := checkProviderRequirements(requiring(shorthand("default")), cfg); err != nil {
		t.Errorf("the reserved key `default` must be satisfied by the agent provider: %v", err)
	}
}

func TestNamedProviderSatisfiesItsOwnKey(t *testing.T) {
	cfg := &config{agentSet: true, agentProviderID: "fast"}
	if err := checkProviderRequirements(requiring(shorthand("fast")), cfg); err != nil {
		t.Errorf("a provider must satisfy a requirement naming its own id: %v", err)
	}
}

func TestEmbeddingAndSpeechRolesAreReported(t *testing.T) {
	cfg := &config{
		embeddingProviderIDs: []string{"embeddings"},
		ttsProviderIDs:       []string{"voice"},
		sttProviderIDs:       []string{"ears"},
	}
	p := requiring(
		&packspec.ProviderRequirement{Key: "embeddings", Role: "embedding"},
		&packspec.ProviderRequirement{Key: "voice", Role: "tts"},
		&packspec.ProviderRequirement{Key: "ears", Role: "stt"},
	)
	if err := checkProviderRequirements(p, cfg); err != nil {
		t.Errorf("declarative providers of every role must satisfy requirements: %v", err)
	}
}

// TestMalformedRequiresIsThePacksError — a duplicate key means the author asked
// for two providers and would get one. Surfacing it at Open costs nothing and
// the alternative is a silent collapse.
func TestMalformedRequiresIsThePacksError(t *testing.T) {
	p := requiring(shorthand("dup"), shorthand("dup"))
	err := checkProviderRequirements(p, &config{})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected a duplicate-key error, got %v", err)
	}
}

func TestPackWithoutRequirementsOpensCleanly(t *testing.T) {
	if err := checkProviderRequirements(&prompt.Pack{}, &config{}); err != nil {
		t.Errorf("a pack declaring nothing must not be affected: %v", err)
	}
}

// TestInventoryOmitsRolesWithNoProviders — an empty slice under a role would
// read as "this role is available with no keys", which is a different claim
// from "this host supplies no providers of this role".
func TestInventoryOmitsRolesWithNoProviders(t *testing.T) {
	inv := (&config{}).providerInventory()
	if len(inv) != 0 {
		t.Errorf("an unconfigured host should report no roles, got %v", inv)
	}
}

// TestProgrammaticProvidersSatisfyRequirements — the ID slices are populated
// only by the spec-based options; WithContextRetrieval, WithTTS and WithVADMode
// set a live service and never touch them. Reading only the slices made this
// gate reject a host that had wired a working provider the documented way.
//
// A false failure at Open is worse than no check: it blocks a correct setup
// rather than an incorrect one. sdk/examples/long-conversation uses
// WithContextRetrieval, so this was not hypothetical.
func TestProgrammaticProvidersSatisfyRequirements(t *testing.T) {
	p := requiring(&packspec.ProviderRequirement{Key: "embeddings", Role: "embedding"})
	cfg := &config{retrievalProvider: &fakeEmbedderForTests{}}
	if err := checkProviderRequirements(p, cfg); err != nil {
		t.Errorf("a programmatically wired provider must satisfy a requirement: %v", err)
	}
}

// TestProgrammaticProviderDoesNotSatisfyAnotherRole — being lenient about the
// KEY must not make the check lenient about the ROLE, or the gate stops meaning
// anything.
func TestProgrammaticProviderDoesNotSatisfyAnotherRole(t *testing.T) {
	p := requiring(&packspec.ProviderRequirement{Key: "embeddings", Role: "embedding"})
	cfg := &config{ttsProviderIDs: []string{"voice"}}
	if err := checkProviderRequirements(p, cfg); err == nil {
		t.Error("a TTS provider must not satisfy an embedding requirement")
	}
}
