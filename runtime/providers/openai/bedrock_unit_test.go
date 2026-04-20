package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// mockSigningCredential records whether Apply was invoked. Used to verify
// the Bedrock path attaches the credential (which in production performs
// SigV4 signing on the request).
type mockSigningCredential struct{ applied bool }

func (m *mockSigningCredential) Type() string { return "aws" }
func (m *mockSigningCredential) Apply(_ context.Context, _ *http.Request) error {
	m.applied = true
	return nil
}

func TestProvider_IsBedrock(t *testing.T) {
	tests := []struct {
		platform string
		want     bool
	}{
		{bedrockPlatform, true},
		{"azure", false},
		{"vertex", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.platform, func(t *testing.T) {
			p := &Provider{platform: tt.platform}
			if got := p.isBedrock(); got != tt.want {
				t.Errorf("isBedrock platform=%q got %v want %v", tt.platform, got, tt.want)
			}
		})
	}
}

func TestProvider_ChatCompletionsURL_Bedrock(t *testing.T) {
	p := &Provider{
		platform: bedrockPlatform,
		baseURL:  "https://bedrock-runtime.us-west-2.amazonaws.com",
		model:    "openai.gpt-oss-20b-1:0",
	}
	got := p.chatCompletionsURL()
	want := "https://bedrock-runtime.us-west-2.amazonaws.com/model/openai.gpt-oss-20b-1:0/invoke"
	if got != want {
		t.Errorf("Bedrock chatCompletionsURL = %q, want %q", got, want)
	}
	// Sanity: must not contain the standard /chat/completions or ?api-version=
	if strings.Contains(got, "/chat/completions") {
		t.Errorf("Bedrock URL must not include /chat/completions: %s", got)
	}
	if strings.Contains(got, "?api-version=") {
		t.Errorf("Bedrock URL must not include Azure ?api-version=: %s", got)
	}
}

func TestProvider_ResponsesURL_BedrockFallsBackToCompletions(t *testing.T) {
	p := &Provider{
		platform: bedrockPlatform,
		baseURL:  "https://bedrock-runtime.us-west-2.amazonaws.com",
		model:    "openai.gpt-oss-20b-1:0",
	}
	if got, want := p.responsesURL(), p.chatCompletionsURL(); got != want {
		t.Errorf("Bedrock responsesURL must equal chatCompletionsURL, got %q want %q", got, want)
	}
}

func TestNewProviderFromConfig_Bedrock(t *testing.T) {
	cred := &mockSigningCredential{}

	t.Run("derives baseURL from PlatformConfig.Region when empty", func(t *testing.T) {
		p := NewProviderFromConfig(&ProviderConfig{
			ID:       "test",
			Model:    "openai.gpt-oss-20b-1:0",
			Platform: bedrockPlatform,
			PlatformConfig: &providers.PlatformConfig{
				Type:   bedrockPlatform,
				Region: "us-west-2",
			},
			Credential: cred,
		})
		want := "https://bedrock-runtime.us-west-2.amazonaws.com"
		if p.baseURL != want {
			t.Errorf("baseURL = %q, want %q", p.baseURL, want)
		}
	})

	t.Run("explicit BaseURL is preserved", func(t *testing.T) {
		custom := "https://custom.bedrock.example"
		p := NewProviderFromConfig(&ProviderConfig{
			ID:       "test",
			Model:    "openai.gpt-oss-20b-1:0",
			BaseURL:  custom,
			Platform: bedrockPlatform,
			PlatformConfig: &providers.PlatformConfig{
				Type:   bedrockPlatform,
				Region: "us-west-2",
			},
			Credential: cred,
		})
		if p.baseURL != custom {
			t.Errorf("explicit baseURL must win, got %q", p.baseURL)
		}
	})

	t.Run("forces Chat Completions API mode (no Responses API on Bedrock)", func(t *testing.T) {
		p := NewProviderFromConfig(&ProviderConfig{
			ID:       "test",
			Model:    "openai.gpt-oss-20b-1:0",
			Platform: bedrockPlatform,
			PlatformConfig: &providers.PlatformConfig{
				Type: bedrockPlatform, Region: "us-west-2",
			},
			Credential: cred,
		})
		if p.apiMode != APIModeCompletions {
			t.Errorf("Bedrock provider must use APIModeCompletions, got %v", p.apiMode)
		}
	})

	t.Run("auto-adds max_tokens and top_p to unsupportedParams", func(t *testing.T) {
		// max_tokens — Bedrock OpenAI uses max_completion_tokens.
		// top_p    — gpt-oss rejects 0.0 and the framework default is 0;
		//            skipping leaves the model default in place.
		p := NewProviderFromConfig(&ProviderConfig{
			ID:       "test",
			Model:    "openai.gpt-oss-20b-1:0",
			Platform: bedrockPlatform,
			PlatformConfig: &providers.PlatformConfig{
				Type: bedrockPlatform, Region: "us-west-2",
			},
			Credential: cred,
		})
		for _, param := range []string{"max_tokens", "top_p"} {
			if !hasUnsupportedParam(p.unsupportedParams, param) {
				t.Errorf("Bedrock provider must mark %s unsupported, got %v",
					param, p.unsupportedParams)
			}
		}
	})

	t.Run("explicit UnsupportedParams overrides Bedrock auto-detection", func(t *testing.T) {
		p := NewProviderFromConfig(&ProviderConfig{
			ID:                "test",
			Model:             "openai.gpt-oss-20b-1:0",
			Platform:          bedrockPlatform,
			PlatformConfig:    &providers.PlatformConfig{Type: bedrockPlatform, Region: "us-west-2"},
			Credential:        cred,
			UnsupportedParams: []string{"top_p"},
		})
		if hasUnsupportedParam(p.unsupportedParams, "max_tokens") {
			t.Errorf("explicit UnsupportedParams must not be merged, got %v", p.unsupportedParams)
		}
		if !hasUnsupportedParam(p.unsupportedParams, "top_p") {
			t.Errorf("explicit top_p must be preserved, got %v", p.unsupportedParams)
		}
	})

	t.Run("non-Bedrock platforms unchanged", func(t *testing.T) {
		p := NewProviderFromConfig(&ProviderConfig{
			ID:    "test",
			Model: "gpt-4o",
		})
		if hasUnsupportedParam(p.unsupportedParams, "max_tokens") {
			t.Errorf("non-Bedrock provider should not mark max_tokens unsupported, got %v", p.unsupportedParams)
		}
	})
}

// newBedrockOpenAIMockProvider stands up an httptest server that answers
// the Bedrock invoke endpoint with a canned OpenAI Chat Completions JSON
// body, and returns a Provider wired to it.
func newBedrockOpenAIMockProvider(t *testing.T, responseBody string) *Provider {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(responseBody))
	}))
	t.Cleanup(server.Close)

	return NewProviderFromConfig(&ProviderConfig{
		ID:       "test-bedrock-openai",
		Model:    "openai.gpt-oss-20b-1:0",
		BaseURL:  server.URL,
		Platform: bedrockPlatform,
		PlatformConfig: &providers.PlatformConfig{
			Type: bedrockPlatform, Region: "us-west-2",
		},
		Credential: &mockSigningCredential{},
		Defaults: providers.ProviderDefaults{
			MaxTokens:   64,
			Temperature: 0.1,
			Pricing: providers.Pricing{
				InputCostPer1K:  0.00015,
				OutputCostPer1K: 0.0006,
			},
		},
	})
}

const bedrockOpenAIMockResponse = `{
  "choices":[{"finish_reason":"stop","index":0,"message":{"content":"Hello.","role":"assistant"}}],
  "created":1776687852,
  "id":"chatcmpl-test",
  "model":"openai.gpt-oss-20b-1:0",
  "object":"chat.completion",
  "usage":{"completion_tokens":2,"prompt_tokens":10,"total_tokens":12}
}`

func TestProvider_PredictStream_BedrockEmitsSingleChunk(t *testing.T) {
	p := newBedrockOpenAIMockProvider(t, bedrockOpenAIMockResponse)

	ch, err := p.PredictStream(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("PredictStream: %v", err)
	}

	chunks := 0
	var last providers.StreamChunk
	for c := range ch {
		if c.Error != nil {
			t.Fatalf("chunk error: %v", c.Error)
		}
		chunks++
		last = c
	}
	if chunks != 1 {
		t.Fatalf("Bedrock fallback must emit exactly one chunk, got %d", chunks)
	}
	if last.Content != "Hello." {
		t.Errorf("chunk content = %q, want %q", last.Content, "Hello.")
	}
	if last.Delta != "Hello." {
		t.Errorf("chunk delta = %q, want %q (delta mirrors content for fallback)", last.Delta, "Hello.")
	}
	if last.FinishReason == nil || *last.FinishReason != finishStop {
		t.Errorf("chunk finish reason = %v, want %q", last.FinishReason, finishStop)
	}
	if last.CostInfo == nil {
		t.Error("chunk must carry CostInfo")
	}
}

func TestProvider_PredictStream_BedrockSurfacesError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"bad"}`))
	}))
	t.Cleanup(server.Close)

	p := NewProviderFromConfig(&ProviderConfig{
		ID:       "test-bedrock-openai-err",
		Model:    "openai.gpt-oss-20b-1:0",
		BaseURL:  server.URL,
		Platform: bedrockPlatform,
		PlatformConfig: &providers.PlatformConfig{
			Type: bedrockPlatform, Region: "us-west-2",
		},
		Credential: &mockSigningCredential{},
		Defaults:   providers.ProviderDefaults{MaxTokens: 64},
	})

	_, err := p.PredictStream(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error from Bedrock fallback when upstream returns 400")
	}
}

func TestToolProvider_PredictStreamWithTools_BedrockEmitsSingleChunk(t *testing.T) {
	respBody := `{
  "choices":[{"finish_reason":"tool_calls","index":0,"message":{
    "content":"",
    "role":"assistant",
    "tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]
  }}],
  "created":1776687852,
  "id":"chatcmpl-test-tools",
  "model":"openai.gpt-oss-20b-1:0",
  "object":"chat.completion",
  "usage":{"completion_tokens":5,"prompt_tokens":20,"total_tokens":25}
}`
	p := newBedrockOpenAIMockProvider(t, respBody)
	tp := &ToolProvider{Provider: p}

	tools, err := tp.BuildTooling([]*providers.ToolDescriptor{
		{
			Name:        "get_weather",
			Description: "Get weather",
			InputSchema: []byte(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
		},
	})
	if err != nil {
		t.Fatalf("BuildTooling: %v", err)
	}

	ch, err := tp.PredictStreamWithTools(
		context.Background(),
		providers.PredictionRequest{Messages: []types.Message{{Role: "user", Content: "weather?"}}},
		tools,
		"auto",
	)
	if err != nil {
		t.Fatalf("PredictStreamWithTools: %v", err)
	}

	chunks := 0
	var last providers.StreamChunk
	for c := range ch {
		if c.Error != nil {
			t.Fatalf("chunk error: %v", c.Error)
		}
		chunks++
		last = c
	}
	if chunks != 1 {
		t.Fatalf("Bedrock fallback must emit exactly one chunk, got %d", chunks)
	}
	if len(last.ToolCalls) != 1 || last.ToolCalls[0].Name != "get_weather" {
		t.Errorf("expected get_weather tool call, got %+v", last.ToolCalls)
	}
	if last.FinishReason == nil || *last.FinishReason != "tool_calls" {
		t.Errorf("expected finish=tool_calls, got %v", last.FinishReason)
	}
}

func TestToolProvider_PredictStreamWithTools_BedrockSurfacesError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"oops"}`))
	}))
	t.Cleanup(server.Close)

	tp := &ToolProvider{
		Provider: NewProviderFromConfig(&ProviderConfig{
			ID:       "test-bedrock-tools-err",
			Model:    "openai.gpt-oss-20b-1:0",
			BaseURL:  server.URL,
			Platform: bedrockPlatform,
			PlatformConfig: &providers.PlatformConfig{
				Type: bedrockPlatform, Region: "us-west-2",
			},
			Credential: &mockSigningCredential{},
			Defaults:   providers.ProviderDefaults{MaxTokens: 64},
		}),
	}

	_, err := tp.PredictStreamWithTools(
		context.Background(),
		providers.PredictionRequest{Messages: []types.Message{{Role: "user", Content: "hi"}}},
		nil,
		"auto",
	)
	if err == nil {
		t.Fatal("expected error from Bedrock tool fallback when upstream returns 500")
	}
}
