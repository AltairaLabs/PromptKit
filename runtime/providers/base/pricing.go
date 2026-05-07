package base

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// PricingSource discriminates how pricing values are obtained.
type PricingSource string

const (
	// PricingSourceInline reads pricing from the loaded YAML descriptor.
	PricingSourceInline PricingSource = "inline"
	// PricingSourceRemote is reserved; see RemotePricingResolver (future work).
	PricingSourceRemote PricingSource = "remote"
)

// PricingDescriptor is the runtime-resolved pricing for a single provider+capability.
type PricingDescriptor struct {
	Source           PricingSource `json:"source"`
	PricingCorrectAt time.Time     `json:"correct_at,omitempty"`
	Currency         string        `json:"currency,omitempty"` // default "usd"
	Items            []PriceItem   `json:"items"`
}

// PriceItem is one line of a pricing table: a rate per unit, optionally
// scoped by dimensions (e.g. image size + quality).
type PriceItem struct {
	Unit       string            `json:"unit"`
	Rate       float64           `json:"rate"`
	Dimensions map[string]string `json:"dimensions,omitempty"`
}

// per1MAliasPrefix recognized in YAML/JSON as a readability shorthand.
// Example: { "per_1m_input_token": 2.50 } → { unit: "input_token", rate: 0.0000025 }.
const per1MAliasPrefix = "per_1m_"

// per1MScale converts a per-1M rate to a per-unit rate.
const per1MScale = 1_000_000.0

// UnmarshalJSON normalizes the per-1M alias form into the canonical PriceItem shape.
func (p *PriceItem) UnmarshalJSON(data []byte) error {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Canonical shape: explicit unit/rate keys.
	if _, hasUnit := raw["unit"]; hasUnit {
		type alias PriceItem
		var a alias
		if err := json.Unmarshal(data, &a); err != nil {
			return err
		}
		*p = PriceItem(a)
		return nil
	}

	// Alias shape: exactly one per_1m_<unit> key.
	for k, v := range raw {
		if !strings.HasPrefix(k, per1MAliasPrefix) {
			continue
		}
		unit := strings.TrimPrefix(k, per1MAliasPrefix)
		var rate float64
		if err := json.Unmarshal(v, &rate); err != nil {
			return fmt.Errorf("price item %s: %w", k, err)
		}
		p.Unit = unit
		p.Rate = rate / per1MScale
		// Dimensions may still be present alongside the alias.
		if dimsRaw, ok := raw["dimensions"]; ok {
			if err := json.Unmarshal(dimsRaw, &p.Dimensions); err != nil {
				return fmt.Errorf("price item dimensions: %w", err)
			}
		}
		return nil
	}
	return fmt.Errorf("price item missing unit/rate or per_1m_* alias: %s", string(data))
}

// PricingRef identifies what we're pricing — the resolver maps this to a PricingDescriptor.
type PricingRef struct {
	Impl       string
	Model      string
	Capability ProviderType
	Hints      map[string]string
}

// PricingResolver returns a PricingDescriptor for a given reference.
// Foundation PR ships only InlinePricingResolver; future work may add a
// remote impl that calls out to a service.
type PricingResolver interface {
	Resolve(ctx context.Context, ref PricingRef) (*PricingDescriptor, error)
}

// InlinePricingResolver returns descriptors registered at construction time
// (typically by the config loader). Lookup is keyed by (impl, model, capability).
type InlinePricingResolver struct {
	mu      sync.RWMutex
	entries map[string]*PricingDescriptor
}

// NewInlinePricingResolver creates an empty resolver.
func NewInlinePricingResolver() *InlinePricingResolver {
	return &InlinePricingResolver{entries: make(map[string]*PricingDescriptor)}
}

// Register stores a pricing descriptor under the given reference.
func (r *InlinePricingResolver) Register(ref PricingRef, desc *PricingDescriptor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[refKey(ref)] = desc
}

// Resolve returns the registered descriptor for ref, or an error if none.
func (r *InlinePricingResolver) Resolve(ctx context.Context, ref PricingRef) (*PricingDescriptor, error) {
	_ = ctx
	r.mu.RLock()
	defer r.mu.RUnlock()
	if d, ok := r.entries[refKey(ref)]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("no inline pricing registered for impl=%s model=%s capability=%s",
		ref.Impl, ref.Model, ref.Capability)
}

func refKey(ref PricingRef) string {
	return ref.Impl + "|" + ref.Model + "|" + string(ref.Capability)
}
