package providers

import (
	"testing"
)

const testModelName = "test-model"

func TestCreateProviderFromSpecUnsupported(t *testing.T) {
	spec := ProviderSpec{
		ID:    "test",
		Type:  "unsupported-type",
		Model: testModelName,
	}

	provider, err := CreateProviderFromSpec(spec)
	if err == nil {
		t.Error("Expected error but got none")
	}
	if provider != nil {
		t.Errorf("Expected nil provider but got %v", provider)
	}
}

func TestCreateProviderFromSpecEmptyType(t *testing.T) {
	spec := ProviderSpec{
		ID:    "test",
		Type:  "",
		Model: testModelName,
	}

	provider, err := CreateProviderFromSpec(spec)
	if err == nil {
		t.Error("Expected error but got none")
	}
	if provider != nil {
		t.Errorf("Expected nil provider but got %v", provider)
	}
}
