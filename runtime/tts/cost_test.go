package tts

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
)

// mockPricingService is a minimal Service implementation that also satisfies
// the pricer interface — used to verify ComputeTTSCost without HTTP.
type mockPricingService struct {
	name  string
	model string
	desc  *base.PricingDescriptor
}

func (m *mockPricingService) Name() string                     { return m.name }
func (m *mockPricingService) ImplName() string                 { return m.name }
func (m *mockPricingService) ModelName() string                { return m.model }
func (m *mockPricingService) Pricing() *base.PricingDescriptor { return m.desc }

func (m *mockPricingService) Synthesize(_ context.Context, _ string, _ SynthesisConfig) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
func (m *mockPricingService) SupportedVoices() []Voice        { return nil }
func (m *mockPricingService) SupportedFormats() []AudioFormat { return nil }

func TestComputeTTSCost_WithPricer(t *testing.T) {
	svc := &mockPricingService{
		name:  "test-provider",
		model: "test-model",
		desc: &base.PricingDescriptor{
			Source:   base.PricingSourceInline,
			Currency: "usd",
			Items: []base.PriceItem{
				{Unit: "character", Rate: 0.0001},
			},
		},
	}

	text := "hello world" // 11 runes
	latency := 100 * time.Millisecond
	ci := ComputeTTSCost(svc, text, latency)
	if ci == nil {
		t.Fatal("ComputeTTSCost() returned nil for pricer service")
	}
	if ci.TotalCost <= 0 {
		t.Errorf("TotalCost = %v, want > 0", ci.TotalCost)
	}
	wantCost := 11 * 0.0001
	if !approxEqual(ci.TotalCost, wantCost) {
		t.Errorf("TotalCost = %v, want ~%v", ci.TotalCost, wantCost)
	}
	if ci.Quantities["character"] != 11 {
		t.Errorf("Quantities[character] = %v, want 11", ci.Quantities["character"])
	}
	if ci.ProviderName != "test-provider" {
		t.Errorf("ProviderName = %q, want %q", ci.ProviderName, "test-provider")
	}
	if ci.Capability != "tts" {
		t.Errorf("Capability = %q, want %q", ci.Capability, "tts")
	}
	if ci.Latency != latency {
		t.Errorf("Latency = %v, want %v", ci.Latency, latency)
	}
}

func TestComputeTTSCost_NoPricer(t *testing.T) {
	// A plain Service without Pricing() should return nil
	type plainSvc struct{}
	ci := ComputeTTSCost(plainSvc{}, "hello", time.Second)
	if ci != nil {
		t.Errorf("ComputeTTSCost() = %v for non-pricer, want nil", ci)
	}
}

func TestComputeTTSCost_NilDescriptor(t *testing.T) {
	svc := &mockPricingService{name: "free", model: "local", desc: nil}
	ci := ComputeTTSCost(svc, "hello", time.Second)
	// nil descriptor → nil cost (free/local provider)
	if ci != nil {
		t.Errorf("ComputeTTSCost() = %v for nil descriptor, want nil", ci)
	}
}

func TestCostInfoToMetaMap_NilInput(t *testing.T) {
	m := CostInfoToMetaMap(nil)
	if m != nil {
		t.Errorf("CostInfoToMetaMap(nil) = %v, want nil", m)
	}
}

func TestCostInfoToMetaMap_PopulatesKeys(t *testing.T) {
	svc := &mockPricingService{
		name:  "openai",
		model: "tts-1",
		desc: &base.PricingDescriptor{
			Source:   base.PricingSourceInline,
			Currency: "usd",
			Items: []base.PriceItem{
				{Unit: "character", Rate: 0.000015},
			},
		},
	}

	ci := ComputeTTSCost(svc, "test text", 50*time.Millisecond)
	m := CostInfoToMetaMap(ci)
	if m == nil {
		t.Fatal("CostInfoToMetaMap() returned nil for populated CostInfo")
	}
	if _, ok := m["total_cost_usd"]; !ok {
		t.Error("missing key total_cost_usd")
	}
	if _, ok := m["quantities"]; !ok {
		t.Error("missing key quantities")
	}
	if _, ok := m["capability"]; !ok {
		t.Error("missing key capability")
	}
	if _, ok := m["provider_name"]; !ok {
		t.Error("missing key provider_name")
	}
	totalCost, ok := m["total_cost_usd"].(float64)
	if !ok || totalCost <= 0 {
		t.Errorf("total_cost_usd = %v, want > 0 float64", m["total_cost_usd"])
	}
}

// approxEqual returns true when a and b differ by less than 1e-12.
func approxEqual(a, b float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < 1e-12
}

// TestComputeTTSCost_OpenAI verifies the real OpenAI pricing descriptor
// produces a sensible cost for a known character count.
func TestComputeTTSCost_OpenAI(t *testing.T) {
	svc := NewOpenAI("test-key")
	const chars = 1000
	text := strings.Repeat("a", chars)
	ci := ComputeTTSCost(svc, text, time.Millisecond)
	if ci == nil {
		t.Fatal("ComputeTTSCost() returned nil for OpenAI service")
	}
	wantCost := chars * openAICharRatePerChar
	if !approxEqual(ci.TotalCost, wantCost) {
		t.Errorf("TotalCost = %v, want ~%v", ci.TotalCost, wantCost)
	}
}

// TestComputeTTSCost_ElevenLabs verifies the ElevenLabs pricing descriptor.
func TestComputeTTSCost_ElevenLabs(t *testing.T) {
	svc := NewElevenLabs("test-key")
	const chars = 100
	text := strings.Repeat("a", chars)
	ci := ComputeTTSCost(svc, text, time.Millisecond)
	if ci == nil {
		t.Fatal("ComputeTTSCost() returned nil for ElevenLabs service")
	}
	wantCost := chars * elevenLabsCharRatePerChar
	if !approxEqual(ci.TotalCost, wantCost) {
		t.Errorf("TotalCost = %v, want ~%v", ci.TotalCost, wantCost)
	}
}

// TestComputeTTSCost_Cartesia verifies the Cartesia pricing descriptor.
func TestComputeTTSCost_Cartesia(t *testing.T) {
	svc := NewCartesia("test-key")
	const chars = 500
	text := strings.Repeat("a", chars)
	ci := ComputeTTSCost(svc, text, time.Millisecond)
	if ci == nil {
		t.Fatal("ComputeTTSCost() returned nil for Cartesia service")
	}
	wantCost := chars * cartesiaCharRatePerChar
	if !approxEqual(ci.TotalCost, wantCost) {
		t.Errorf("TotalCost = %v, want ~%v", ci.TotalCost, wantCost)
	}
}
