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
