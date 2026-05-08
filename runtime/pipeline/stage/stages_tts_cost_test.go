package stage_test

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// pricingTTSService is a TTSService that also exposes Pricing/ImplName/ModelName
// so the TTSStage cost-stamping path can compute cost.
type pricingTTSService struct {
	audio []byte
}

func (p *pricingTTSService) Synthesize(_ context.Context, _ string) ([]byte, error) {
	return p.audio, nil
}

func (p *pricingTTSService) MIMEType() string { return "audio/pcm" }

func (p *pricingTTSService) Pricing() *base.PricingDescriptor {
	return &base.PricingDescriptor{
		Source:   base.PricingSourceInline,
		Currency: "usd",
		Items: []base.PriceItem{
			{Unit: "character", Rate: 0.0001},
		},
	}
}

func (p *pricingTTSService) ImplName() string  { return "test-tts" }
func (p *pricingTTSService) ModelName() string { return "test-model" }

// nonPricingTTSService is a plain TTSService without Pricing — simulates
// a provider that hasn't been migrated yet.
type nonPricingTTSService struct{}

func (n *nonPricingTTSService) Synthesize(_ context.Context, _ string) ([]byte, error) {
	return []byte("audio"), nil
}

func (n *nonPricingTTSService) MIMEType() string { return "audio/pcm" }

func TestTTSStage_CostStampedOnMessage(t *testing.T) {
	svc := &pricingTTSService{audio: []byte("fake pcm audio")}
	s := stage.NewTTSStage(svc, stage.DefaultTTSConfig())

	text := "hello world" // 11 runes
	msg := &types.Message{
		Role:    "assistant",
		Content: text,
	}
	elem := stage.NewMessageElement(msg)
	inputs := []stage.StreamElement{elem}
	results := runStage(t, s, inputs, 2*time.Second)

	if len(results) != 1 {
		t.Fatalf("expected 1 element, got %d", len(results))
	}
	result := results[0]
	if result.Message == nil {
		t.Fatal("result.Message is nil")
	}
	if result.Message.Meta == nil {
		t.Fatal("result.Message.Meta is nil — tts_cost not stamped")
	}
	raw, ok := result.Message.Meta["tts_cost"]
	if !ok {
		t.Fatal("result.Message.Meta missing key 'tts_cost'")
	}
	costMap, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("tts_cost is %T, want map[string]any", raw)
	}
	totalCost, ok := costMap["total_cost_usd"].(float64)
	if !ok {
		t.Fatalf("tts_cost.total_cost_usd is %T, want float64", costMap["total_cost_usd"])
	}
	if totalCost <= 0 {
		t.Errorf("tts_cost.total_cost_usd = %v, want > 0", totalCost)
	}
	// 11 chars × $0.0001/char = $0.0011
	wantCost := 11 * 0.0001
	diff := totalCost - wantCost
	if diff < 0 {
		diff = -diff
	}
	if diff > 1e-12 {
		t.Errorf("tts_cost.total_cost_usd = %v, want ~%v", totalCost, wantCost)
	}

	// Verify quantities are present
	if _, hasQty := costMap["quantities"]; !hasQty {
		t.Error("tts_cost missing 'quantities' key")
	}
}

func TestTTSStage_NoCostWithoutPricer(t *testing.T) {
	// A service without Pricing() should not stamp any cost on the message.
	svc := &nonPricingTTSService{}
	s := stage.NewTTSStage(svc, stage.DefaultTTSConfig())

	text := "hello"
	msg := &types.Message{Role: "assistant", Content: text}
	elem := stage.NewMessageElement(msg)
	results := runStage(t, s, []stage.StreamElement{elem}, 2*time.Second)

	if len(results) != 1 {
		t.Fatalf("expected 1 element, got %d", len(results))
	}
	if results[0].Message == nil {
		t.Fatal("result.Message is nil")
	}
	if _, ok := results[0].Message.Meta["tts_cost"]; ok {
		t.Error("tts_cost should not be set for a non-pricer service")
	}
}

func TestTTSStage_CostNotStampedOnTextOnlyElement(t *testing.T) {
	// When the element carries only text (no Message), cost cannot be stamped.
	svc := &pricingTTSService{audio: []byte("audio")}
	s := stage.NewTTSStage(svc, stage.DefaultTTSConfig())

	text := "hello"
	elem := stage.StreamElement{Text: &text}
	results := runStage(t, s, []stage.StreamElement{elem}, 2*time.Second)

	if len(results) != 1 {
		t.Fatalf("expected 1 element, got %d", len(results))
	}
	// No message → no Meta key; the audio should still be populated.
	if results[0].Audio == nil {
		t.Error("expected audio in output element")
	}
}
