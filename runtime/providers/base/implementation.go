package base

import "context"

// Implementation is an embeddable struct that supplies default Provider
// method implementations for a provider. Concrete impls populate the fields
// in their constructor and override any methods (Validate, Init, HealthCheck,
// Close) that need real behavior.
type Implementation struct {
	name    string
	typ     ProviderType
	pricing *PricingDescriptor
}

// NewImplementation constructs a helper. Pass nil pricing for free / local providers.
func NewImplementation(name string, typ ProviderType, pricing *PricingDescriptor) *Implementation {
	return &Implementation{name: name, typ: typ, pricing: pricing}
}

// Name returns the provider's unique registry name.
func (i *Implementation) Name() string { return i.name }

// Type returns the capability type.
func (i *Implementation) Type() ProviderType { return i.typ }

// Pricing returns the configured pricing descriptor (may be nil). Safe to
// call on a nil receiver — a provider struct built via a raw literal (common
// in tests) that skips NewImplementation has a nil *Implementation, and this
// must degrade to "no pricing configured" rather than panic.
func (i *Implementation) Pricing() *PricingDescriptor {
	if i == nil {
		return nil
	}
	return i.pricing
}

// Validate performs synchronous config validation. Default no-op.
func (i *Implementation) Validate() error { return nil }

// Init performs asynchronous setup (network, warm-up). Default no-op.
func (i *Implementation) Init(ctx context.Context) error { _ = ctx; return nil }

// HealthCheck reports liveness. Default no-op.
func (i *Implementation) HealthCheck(ctx context.Context) error { _ = ctx; return nil }

// Close releases resources. Default no-op.
func (i *Implementation) Close() error { return nil }

// SetPricing allows post-construction pricing wiring (used when the resolver
// fills in pricing after Init completes).
func (i *Implementation) SetPricing(p *PricingDescriptor) { i.pricing = p }
