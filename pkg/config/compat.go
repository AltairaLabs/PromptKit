package config

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
)

// kindProvider is the value of the top-level `kind:` field in a Provider YAML.
const kindProvider = "Provider"

// per1KScale converts a per-1K-tokens rate (legacy YAML form) to a per-token rate.
const per1KScale = 1000.0

// rawProviderDetect is used only to peek at the top-level YAML shape before
// choosing the legacy or unified parse path.
type rawProviderDetect struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec map[string]yaml.Node `yaml:"spec"`
}

// rawProviderUnified captures the new unified Provider YAML shape.
type rawProviderUnified struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec rawSpecUnified `yaml:"spec"`
}

type rawSpecUnified struct {
	Impl         string              `yaml:"impl,omitempty"`
	Endpoint     string              `yaml:"endpoint,omitempty"`
	Auth         AuthSpec            `yaml:"auth,omitempty"`
	Timeouts     TimeoutsSpec        `yaml:"timeouts,omitempty"`
	Retry        RetrySpec           `yaml:"retry,omitempty"`
	Capabilities []rawCapabilityYAML `yaml:"capabilities,omitempty"`
}

type rawCapabilityYAML struct {
	Type     string          `yaml:"type"`
	Model    string          `yaml:"model"`
	Defaults map[string]any  `yaml:"defaults,omitempty"`
	Pricing  *rawPricingYAML `yaml:"pricing,omitempty"`
}

type rawPricingYAML struct {
	Source    string           `yaml:"source,omitempty"`
	CorrectAt string           `yaml:"correct_at,omitempty"`
	Currency  string           `yaml:"currency,omitempty"`
	Items     []base.PriceItem `yaml:"items,omitempty"`
}

// rawProviderLegacy captures the legacy chat-only Provider YAML shape.
type rawProviderLegacy struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec rawSpecLegacy `yaml:"spec"`
}

type rawSpecLegacy struct {
	ID               string             `yaml:"id,omitempty"`
	LegacyType       string             `yaml:"type,omitempty"`
	Model            string             `yaml:"model,omitempty"`
	BaseURL          string             `yaml:"base_url,omitempty"`
	Pricing          *legacyPricingYAML `yaml:"pricing,omitempty"`
	Defaults         map[string]any     `yaml:"defaults,omitempty"`
	IncludeRawOutput *bool              `yaml:"include_raw_output,omitempty"`
	// Capabilities is the decorative []string field present on legacy YAMLs; ignored.
	Capabilities []string `yaml:"capabilities,omitempty"`
}

type legacyPricingYAML struct {
	InputCostPer1K  float64 `yaml:"input_cost_per_1k"`
	OutputCostPer1K float64 `yaml:"output_cost_per_1k"`
}

// LoadProviderSpec reads a Provider YAML document (either legacy or unified
// shape) and returns the unified ProviderSpec.
//
// Legacy shape: top-level spec.type / spec.model / spec.pricing fields.
// Unified shape: spec.impl + spec.capabilities[] where each element is a
// mapping node (not a scalar string).
func LoadProviderSpec(data []byte) (*ProviderSpec, error) {
	// First pass: detect which shape we have.
	var detect rawProviderDetect
	if err := yaml.Unmarshal(data, &detect); err != nil {
		return nil, fmt.Errorf("yaml unmarshal: %w", err)
	}
	if detect.Kind != kindProvider {
		return nil, fmt.Errorf("expected kind=%s, got %q", kindProvider, detect.Kind)
	}

	unified := isUnifiedShape(detect.Spec)

	if unified {
		var raw rawProviderUnified
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("yaml unmarshal (unified): %w", err)
		}
		return translateUnified(&raw)
	}

	var raw rawProviderLegacy
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("yaml unmarshal (legacy): %w", err)
	}
	return translateLegacy(&raw)
}

// isUnifiedShape returns true if the spec map indicates the new unified YAML
// layout: either spec.impl is present, or spec.capabilities is a sequence
// whose first element is a mapping node (not a scalar string).
func isUnifiedShape(spec map[string]yaml.Node) bool {
	if _, hasImpl := spec["impl"]; hasImpl {
		return true
	}
	capsNode, ok := spec["capabilities"]
	if !ok {
		return false
	}
	if capsNode.Kind != yaml.SequenceNode || len(capsNode.Content) == 0 {
		return false
	}
	return capsNode.Content[0].Kind == yaml.MappingNode
}

func translateLegacy(raw *rawProviderLegacy) (*ProviderSpec, error) {
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

func translateUnified(raw *rawProviderUnified) (*ProviderSpec, error) {
	spec := &ProviderSpec{
		Name:     raw.Metadata.Name,
		Impl:     raw.Spec.Impl,
		Endpoint: raw.Spec.Endpoint,
		Auth:     raw.Spec.Auth,
		Timeouts: raw.Spec.Timeouts,
		Retry:    raw.Spec.Retry,
	}
	for _, c := range raw.Spec.Capabilities {
		typ, err := base.ParseProviderType(c.Type)
		if err != nil {
			return nil, fmt.Errorf("capability: %w", err)
		}
		cs := CapabilitySpec{Type: typ, Model: c.Model, Defaults: c.Defaults}
		if c.Pricing != nil {
			if c.Pricing.Source == "remote" {
				return nil, fmt.Errorf("pricing.source=remote is not yet supported; use source=inline or omit")
			}
			cs.Pricing = &base.PricingDescriptor{
				Source:   base.PricingSourceInline,
				Currency: pricingCurrency(c.Pricing.Currency),
				Items:    c.Pricing.Items,
			}
			if c.Pricing.CorrectAt != "" {
				if ts, perr := time.Parse("2006-01-02", c.Pricing.CorrectAt); perr == nil {
					cs.Pricing.PricingCorrectAt = ts
				}
			}
		}
		spec.Capabilities = append(spec.Capabilities, cs)
	}
	return spec, nil
}

// pricingCurrency returns s if non-empty, otherwise "usd".
func pricingCurrency(s string) string {
	if s == "" {
		return "usd"
	}
	return s
}
