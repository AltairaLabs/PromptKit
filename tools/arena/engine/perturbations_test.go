package engine

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

func TestExpandPerturbations(t *testing.T) {
	t.Run("no perturbations", func(t *testing.T) {
		scenario := &config.Scenario{
			Turns: []config.TurnDefinition{
				{Role: "user", Content: "Hello"},
			},
		}
		variants := ExpandPerturbations(scenario)
		if variants != nil {
			t.Fatalf("expected nil, got %d variants", len(variants))
		}
	})

	t.Run("single key single value", func(t *testing.T) {
		scenario := &config.Scenario{
			Turns: []config.TurnDefinition{
				{Role: "user", Content: "Go to {city}", Perturbations: map[string][]string{
					"city": {"NYC"},
				}},
			},
		}
		variants := ExpandPerturbations(scenario)
		if len(variants) != 1 {
			t.Fatalf("expected 1 variant, got %d", len(variants))
		}
		if variants[0].Substitutions["city"] != "NYC" {
			t.Fatalf("expected NYC, got %s", variants[0].Substitutions["city"])
		}
	})

	t.Run("single key multiple values", func(t *testing.T) {
		scenario := &config.Scenario{
			Turns: []config.TurnDefinition{
				{Role: "user", Content: "Go to {city}", Perturbations: map[string][]string{
					"city": {"NYC", "LA", "Tokyo"},
				}},
			},
		}
		variants := ExpandPerturbations(scenario)
		if len(variants) != 3 {
			t.Fatalf("expected 3 variants, got %d", len(variants))
		}
	})

	t.Run("cartesian product of two keys", func(t *testing.T) {
		scenario := &config.Scenario{
			Turns: []config.TurnDefinition{
				{Role: "user", Content: "Fly from {city} {tone}", Perturbations: map[string][]string{
					"city": {"NYC", "LA"},
					"tone": {"formal", "casual"},
				}},
			},
		}
		variants := ExpandPerturbations(scenario)
		if len(variants) != 4 {
			t.Fatalf("expected 4 variants (2x2), got %d", len(variants))
		}
		// Verify all combinations exist
		seen := make(map[string]bool)
		for _, v := range variants {
			key := v.Substitutions["city"] + "+" + v.Substitutions["tone"]
			seen[key] = true
		}
		expected := []string{"NYC+casual", "NYC+formal", "LA+casual", "LA+formal"}
		for _, e := range expected {
			if !seen[e] {
				t.Errorf("missing combination: %s", e)
			}
		}
	})

	t.Run("perturbations across turns merged", func(t *testing.T) {
		scenario := &config.Scenario{
			Turns: []config.TurnDefinition{
				{Role: "user", Content: "Go to {city}", Perturbations: map[string][]string{
					"city": {"NYC", "LA"},
				}},
				{Role: "user", Content: "In {city} do {action}", Perturbations: map[string][]string{
					"city":   {"NYC", "Tokyo"}, // NYC is duplicate, Tokyo is new
					"action": {"eat", "shop"},
				}},
			},
		}
		variants := ExpandPerturbations(scenario)
		// city: [NYC, LA, Tokyo] (3) × action: [eat, shop] (2) = 6
		if len(variants) != 6 {
			t.Fatalf("expected 6 variants (3x2), got %d", len(variants))
		}
	})

	t.Run("empty values ignored", func(t *testing.T) {
		scenario := &config.Scenario{
			Turns: []config.TurnDefinition{
				{Role: "user", Content: "Go to {city}", Perturbations: map[string][]string{
					"city":  {"NYC"},
					"empty": {},
				}},
			},
		}
		variants := ExpandPerturbations(scenario)
		if len(variants) != 1 {
			t.Fatalf("expected 1 variant, got %d", len(variants))
		}
	})
}

func TestApplyPerturbation(t *testing.T) {
	t.Run("substitutes in content", func(t *testing.T) {
		turns := []config.TurnDefinition{
			{Role: "user", Content: "Book a flight from {city} to London"},
		}
		variant := PerturbationVariant{Substitutions: map[string]string{"city": "NYC"}}
		result := ApplyPerturbation(turns, variant)
		if result[0].Content != "Book a flight from NYC to London" {
			t.Fatalf("expected substituted content, got %q", result[0].Content)
		}
	})

	t.Run("substitutes in text parts", func(t *testing.T) {
		turns := []config.TurnDefinition{
			{Role: "user", Parts: []config.TurnContentPart{
				{Type: "text", Text: "Hello {name}"},
				{Type: "image"},
			}},
		}
		variant := PerturbationVariant{Substitutions: map[string]string{"name": "Alice"}}
		result := ApplyPerturbation(turns, variant)
		if result[0].Parts[0].Text != "Hello Alice" {
			t.Fatalf("expected substituted text part, got %q", result[0].Parts[0].Text)
		}
	})

	t.Run("empty substitutions returns original", func(t *testing.T) {
		turns := []config.TurnDefinition{
			{Role: "user", Content: "Hello {name}"},
		}
		variant := PerturbationVariant{Substitutions: map[string]string{}}
		result := ApplyPerturbation(turns, variant)
		if result[0].Content != "Hello {name}" {
			t.Fatalf("expected unchanged content, got %q", result[0].Content)
		}
	})

	t.Run("multiple substitutions", func(t *testing.T) {
		turns := []config.TurnDefinition{
			{Role: "user", Content: "Fly from {city} to {dest}"},
		}
		variant := PerturbationVariant{Substitutions: map[string]string{
			"city": "NYC",
			"dest": "London",
		}}
		result := ApplyPerturbation(turns, variant)
		if result[0].Content != "Fly from NYC to London" {
			t.Fatalf("expected substituted content, got %q", result[0].Content)
		}
	})

	t.Run("does not modify original turns", func(t *testing.T) {
		turns := []config.TurnDefinition{
			{Role: "user", Content: "Hello {name}"},
		}
		variant := PerturbationVariant{Substitutions: map[string]string{"name": "Alice"}}
		_ = ApplyPerturbation(turns, variant)
		if turns[0].Content != "Hello {name}" {
			t.Fatal("original turns should not be modified")
		}
	})
}

func TestCollectPerturbations(t *testing.T) {
	t.Run("deduplicates values", func(t *testing.T) {
		turns := []config.TurnDefinition{
			{Perturbations: map[string][]string{"city": {"NYC", "LA"}}},
			{Perturbations: map[string][]string{"city": {"NYC", "Tokyo"}}},
		}
		merged := collectPerturbations(turns)
		cities := merged["city"]
		if len(cities) != 3 {
			t.Fatalf("expected 3 unique cities, got %d: %v", len(cities), cities)
		}
	})
}

func TestCartesianProduct(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		result := cartesianProduct(map[string][]string{})
		if result != nil {
			t.Fatalf("expected nil, got %d variants", len(result))
		}
	})

	t.Run("three keys", func(t *testing.T) {
		result := cartesianProduct(map[string][]string{
			"a": {"1", "2"},
			"b": {"x"},
			"c": {"p", "q", "r"},
		})
		// 2 × 1 × 3 = 6
		if len(result) != 6 {
			t.Fatalf("expected 6 variants, got %d", len(result))
		}
	})
}
