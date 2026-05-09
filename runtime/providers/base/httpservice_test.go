package base

import (
	"net/http"
	"testing"
)

func TestWithBaseURL(t *testing.T) {
	f := &HTTPServiceFields{}
	WithBaseURL("https://example.com")(f)
	if f.BaseURL != "https://example.com" {
		t.Errorf("BaseURL = %q, want %q", f.BaseURL, "https://example.com")
	}
}

func TestWithClient(t *testing.T) {
	c := &http.Client{}
	f := &HTTPServiceFields{}
	WithClient(c)(f)
	if f.Client != c {
		t.Error("Client was not set correctly")
	}
}

func TestWithModel(t *testing.T) {
	f := &HTTPServiceFields{}
	WithModel("gpt-4o")(f)
	if f.Model != "gpt-4o" {
		t.Errorf("Model = %q, want %q", f.Model, "gpt-4o")
	}
}

func TestWithAPIKey(t *testing.T) {
	f := &HTTPServiceFields{}
	WithAPIKey("sk-test")(f)
	if f.APIKey != "sk-test" {
		t.Errorf("APIKey = %q, want %q", f.APIKey, "sk-test")
	}
}

func TestHTTPServiceOption_Chaining(t *testing.T) {
	f := &HTTPServiceFields{}
	opts := []HTTPServiceOption{
		WithAPIKey("sk-abc"),
		WithBaseURL("https://proxy.example.com"),
		WithModel("my-model"),
		WithClient(&http.Client{}),
	}
	for _, opt := range opts {
		opt(f)
	}
	if f.APIKey != "sk-abc" {
		t.Errorf("APIKey = %q, want %q", f.APIKey, "sk-abc")
	}
	if f.BaseURL != "https://proxy.example.com" {
		t.Errorf("BaseURL = %q, want %q", f.BaseURL, "https://proxy.example.com")
	}
	if f.Model != "my-model" {
		t.Errorf("Model = %q, want %q", f.Model, "my-model")
	}
	if f.Client == nil {
		t.Error("Client should not be nil after WithClient")
	}
}

func TestNewHTTPService_Defaults(t *testing.T) {
	pricing := &PricingDescriptor{Source: PricingSourceInline, Currency: "usd"}
	impl, fields := NewHTTPService("sk-key", HTTPServiceDefaults{
		Name:    "test-svc",
		Type:    ProviderTypeTTS,
		Pricing: pricing,
		BaseURL: "https://api.example.com",
		Model:   "v1",
		Timeout: 30,
	})
	if impl == nil {
		t.Fatal("NewHTTPService returned nil Implementation")
	}
	if impl.Name() != "test-svc" {
		t.Errorf("Name = %q, want %q", impl.Name(), "test-svc")
	}
	if impl.Type() != ProviderTypeTTS {
		t.Errorf("Type = %q, want %q", impl.Type(), ProviderTypeTTS)
	}
	if impl.Pricing() != pricing {
		t.Error("Pricing not propagated")
	}
	if fields == nil {
		t.Fatal("NewHTTPService returned nil HTTPServiceFields")
	}
	if fields.APIKey != "sk-key" {
		t.Errorf("APIKey = %q", fields.APIKey)
	}
	if fields.BaseURL != "https://api.example.com" {
		t.Errorf("BaseURL = %q", fields.BaseURL)
	}
	if fields.Model != "v1" {
		t.Errorf("Model = %q", fields.Model)
	}
	if fields.Client == nil {
		t.Error("Client should be created from defaults.Timeout")
	}
}

func TestNewHTTPService_OptsOverrideDefaults(t *testing.T) {
	_, fields := NewHTTPService("sk-key", HTTPServiceDefaults{
		Name:    "test-svc",
		Type:    ProviderTypeSTT,
		BaseURL: "https://api.example.com",
		Model:   "v1",
	},
		WithBaseURL("https://override.example.com"),
		WithModel("v2"),
	)
	if fields.BaseURL != "https://override.example.com" {
		t.Errorf("BaseURL = %q, want override", fields.BaseURL)
	}
	if fields.Model != "v2" {
		t.Errorf("Model = %q, want override", fields.Model)
	}
}
