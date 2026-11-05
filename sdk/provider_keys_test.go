package sdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	rtprompt "github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pack"
)

// packRequiring builds a pack declaring one logical provider, and a prompt
// whose single check points at whatever name is given.
func packRequiring(declaredKey, role, checkType, checkKey string) (*pack.Pack, *pack.Prompt) {
	required := true
	p := &pack.Pack{}
	p.Requires = &rtprompt.Requires{
		Providers: []*rtprompt.ProviderRequirement{
			{Key: declaredKey, Role: role, Required: &required},
		},
	}
	prompt := &pack.Prompt{
		Validators: []*packspec.Validator{
			{Type: checkType, Params: map[string]any{"provider": checkKey}},
		},
	}
	return p, prompt
}

func TestCheckProviderKeys_Passes(t *testing.T) {
	p, prompt := packRequiring("grader", "llm", "toxicity", "grader")
	cfg := &config{}
	ensureProviderPool(cfg)
	cfg.providers.Register(mock.NewProvider("grader", "mock-model", false))

	assert.NoError(t, checkProviderKeys(p, prompt, cfg))
}

// A check naming nothing is not this gate's business: the guardrail compile
// path reports it, with a message about the missing param.
func TestCheckProviderKeys_IgnoresChecksThatNameNothing(t *testing.T) {
	p, prompt := packRequiring("grader", "llm", "toxicity", "grader")
	prompt.Validators[0].Params = nil
	cfg := &config{}

	assert.NoError(t, checkProviderKeys(p, prompt, cfg))
}

func TestCheckProviderKeys_UndeclaredName(t *testing.T) {
	p, prompt := packRequiring("grader", "llm", "toxicity", "graderr")
	cfg := &config{}
	ensureProviderPool(cfg)
	cfg.providers.Register(mock.NewProvider("grader", "mock-model", false))

	err := checkProviderKeys(p, prompt, cfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not declare in requires")
	assert.Contains(t, err.Error(), "graderr", "the name the check used")
	assert.Contains(t, err.Error(), "grader", "and what the pack declares instead")
}

func TestCheckProviderKeys_NothingWiredAtAll(t *testing.T) {
	p, prompt := packRequiring("grader", "llm", "toxicity", "grader")

	err := checkProviderKeys(p, prompt, &config{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no providers wired at all")
}

func TestCheckProviderKeys_WrongKind(t *testing.T) {
	p, prompt := packRequiring("grader", "llm", "toxicity", "grader")
	cfg := &config{}
	err := cfg.registerInferenceProvider("grader", textClassifierStub{})
	require.NoError(t, err)

	err = checkProviderKeys(p, prompt, cfg)

	require.Error(t, err)
	assert.ErrorIs(t, err, errProviderKeys)
	assert.Contains(t, err.Error(), "runs completions")
	assert.Contains(t, err.Error(), "toxicity", "the error names the check, so it can be found")
}

// A disabled validator is not wired, so its provider is not required.
func TestCheckProviderKeys_SkipsDisabledValidators(t *testing.T) {
	p, prompt := packRequiring("grader", "llm", "toxicity", "graderr")
	disabled := false
	prompt.Validators[0].Enabled = &disabled

	assert.NoError(t, checkProviderKeys(p, prompt, &config{}))
}

// A check that needs no ancillary provider is not policed, even if someone
// writes the param on it.
func TestCheckProviderKeys_IgnoresChecksThatNeedNoProvider(t *testing.T) {
	p, prompt := packRequiring("grader", "llm", "contains", "anything")
	prompt.Validators[0].Params["text"] = "forbidden"

	assert.NoError(t, checkProviderKeys(p, prompt, &config{}))
}

func TestDeclaredRequirementKeys_MalformedRequires(t *testing.T) {
	p := &pack.Pack{}
	p.Requires = &rtprompt.Requires{
		Providers: []*rtprompt.ProviderRequirement{
			{Key: "dup", Role: "llm"},
			{Key: "dup", Role: "llm"},
		},
	}
	prompt := &pack.Prompt{
		Validators: []*packspec.Validator{
			{Type: "toxicity", Params: map[string]any{"provider": "dup"}},
		},
	}

	err := checkProviderKeys(p, prompt, &config{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate key")
}

// packWithPackLevelEval builds a pack declaring one LLM provider and carrying
// one pack-level eval that names checkKey, with a prompt that has no checks.
func packWithPackLevelEval(declaredKey, checkKey string) (*pack.Pack, *pack.Prompt) {
	p, _ := packRequiring(declaredKey, "llm", "toxicity", declaredKey)
	p.Evals = []*packspec.Eval{
		{ID: "pack-tox", Type: "toxicity", Params: map[string]any{"provider": checkKey}},
	}
	return p, &pack.Prompt{}
}

// Pack-level evals run on every turn just like prompt-level ones, so Open must
// check their providers too rather than let every turn fail instead (#2334).
func TestCheckProviderKeys_PackLevelEvalUndeclaredName(t *testing.T) {
	p, prompt := packWithPackLevelEval("grader", "graderr")
	cfg := &config{}
	ensureProviderPool(cfg)
	cfg.providers.Register(mock.NewProvider("grader", "mock-model", false))

	err := checkProviderKeys(p, prompt, cfg)

	require.Error(t, err)
	assert.ErrorIs(t, err, errProviderKeys)
	assert.Contains(t, err.Error(), "does not declare in requires")
	assert.Contains(t, err.Error(), "graderr")
}

func TestCheckProviderKeys_PackLevelEvalUnbound(t *testing.T) {
	p, prompt := packWithPackLevelEval("grader", "grader")

	err := checkProviderKeys(p, prompt, &config{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no providers wired at all")
}

func TestCheckProviderKeys_PackLevelEvalWrongKind(t *testing.T) {
	p, prompt := packWithPackLevelEval("grader", "grader")
	cfg := &config{}
	require.NoError(t, cfg.registerInferenceProvider("grader", textClassifierStub{}))

	err := checkProviderKeys(p, prompt, cfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "runs completions")
	assert.Contains(t, err.Error(), "toxicity")
}

// A prompt-level eval with the same ID replaces the pack-level one at run
// time, so only the replacement's provider is checked.
func TestCheckProviderKeys_PromptEvalOverridesPackEval(t *testing.T) {
	p, prompt := packWithPackLevelEval("grader", "graderr")
	prompt.Evals = []*packspec.Eval{
		{ID: "pack-tox", Type: "toxicity", Params: map[string]any{"provider": "grader"}},
	}
	cfg := &config{}
	ensureProviderPool(cfg)
	cfg.providers.Register(mock.NewProvider("grader", "mock-model", false))

	assert.NoError(t, checkProviderKeys(p, prompt, cfg))

	// And the override is what is checked: break its key and the gate fails.
	prompt.Evals[0].Params["provider"] = "nope"
	err := checkProviderKeys(p, prompt, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"nope"`)
	assert.NotContains(t, err.Error(), "graderr")
}
