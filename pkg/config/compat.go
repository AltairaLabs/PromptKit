package config

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
)

// kindProvider is the value of the top-level `kind:` field in a Provider YAML.
const kindProvider = "Provider"

// per1KScale converts a per-1K-tokens rate (legacy YAML form) to a per-token rate.
const per1KScale = 1000.0

// rawProviderDetect is used only to peek at the top-level YAML shape before
// running the field-rename sanity checks below.
type rawProviderDetect struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec map[string]yaml.Node `yaml:"spec"`
}

// rawProvider captures the canonical Provider YAML shape. One provider yaml
// declares one role (`spec.role`) and the implementation discriminator lives
// on `spec.type`. There is no multi-role / unified-list shape — providers
// that expose multiple roles (inference + embedding + image) are authored as
// separate provider yamls sharing credentials via the credential block.
type rawProvider struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec rawSpec `yaml:"spec"`
}

type rawSpec struct {
	ID               string             `yaml:"id,omitempty"`
	LegacyType       string             `yaml:"type,omitempty"`
	Model            string             `yaml:"model,omitempty"`
	BaseURL          string             `yaml:"base_url,omitempty"`
	Pricing          *legacyPricingYAML `yaml:"pricing,omitempty"`
	Defaults         map[string]any     `yaml:"defaults,omitempty"`
	IncludeRawOutput *bool              `yaml:"include_raw_output,omitempty"`
	// Capabilities is the per-model feature-flag list (e.g. vision, tools,
	// streaming). Distinct from spec.role which is the singular role
	// discriminator (llm/tts/stt).
	Capabilities []string `yaml:"capabilities,omitempty"`
}

type legacyPricingYAML struct {
	InputCostPer1K  float64 `yaml:"input_cost_per_1k"`
	OutputCostPer1K float64 `yaml:"output_cost_per_1k"`
}

// LoadProviderSpec reads a Provider YAML document and returns the canonical
// ProviderSpec.
//
// The 2026-05-18 cleanup removed the multi-role "unified" shape
// (spec.impl + spec.capabilities: [{type, model, pricing}, ...]) and the
// singular spec.capability field. Yamls using either are rejected here with
// an explicit migration message — there is no compat shim.
func LoadProviderSpec(data []byte) (*ProviderSpec, error) {
	var detect rawProviderDetect
	if err := yaml.Unmarshal(data, &detect); err != nil {
		return nil, fmt.Errorf("yaml unmarshal: %w", err)
	}
	if detect.Kind != kindProvider {
		return nil, fmt.Errorf("expected kind=%s, got %q", kindProvider, detect.Kind)
	}
	if err := rejectRemovedShapes(detect.Spec); err != nil {
		return nil, err
	}

	var raw rawProvider
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("yaml unmarshal: %w", err)
	}
	return translateLegacy(&raw)
}

// rejectRemovedShapes catches yamls authored against the pre-2026-05-18
// schema and points the author at the rename instead of failing later with
// a confusing schema-validation error.
func rejectRemovedShapes(spec map[string]yaml.Node) error {
	if _, ok := spec["capability"]; ok {
		return fmt.Errorf("spec.capability has been renamed to spec.role " +
			"(2026-05-18). Rename the field; the values (llm/tts/stt) are unchanged")
	}
	if _, ok := spec["impl"]; ok {
		return fmt.Errorf("spec.impl was part of the multi-role 'unified' " +
			"shape, which was removed 2026-05-18. Use spec.type for the " +
			"implementation discriminator and author one provider yaml per role")
	}
	if capsNode, ok := spec["capabilities"]; ok &&
		capsNode.Kind == yaml.SequenceNode &&
		len(capsNode.Content) > 0 &&
		capsNode.Content[0].Kind == yaml.MappingNode {
		return fmt.Errorf("spec.capabilities as a list of {type, model, pricing} " +
			"entries (multi-role shape) was removed 2026-05-18. Use spec.role " +
			"for the role and author one provider yaml per role. spec.capabilities " +
			"as a list of feature-flag strings (vision, tools, ...) is unchanged")
	}
	return nil
}

func translateLegacy(raw *rawProvider) (*ProviderSpec, error) {
	capSpec := CapabilitySpec{
		Type:     base.ProviderTypeInference,
		Model:    raw.Spec.Model,
		Defaults: raw.Spec.Defaults,
	}
	if raw.Spec.Pricing != nil {
		capSpec.Pricing = &base.PricingDescriptor{
			Source:   base.PricingSourceInline,
			Currency: "usd",
			Items: []base.PriceItem{
				{Unit: "input_token", Rate: raw.Spec.Pricing.InputCostPer1K / per1KScale},
				{Unit: "output_token", Rate: raw.Spec.Pricing.OutputCostPer1K / per1KScale},
			},
		}
	}
	return &ProviderSpec{
		Name:         raw.Metadata.Name,
		Impl:         raw.Spec.LegacyType,
		Endpoint:     raw.Spec.BaseURL,
		Capabilities: []CapabilitySpec{capSpec},
	}, nil
}

// pricingCurrency returns s if non-empty, otherwise "usd".
func pricingCurrency(s string) string {
	if s == "" {
		return "usd"
	}
	return s
}
