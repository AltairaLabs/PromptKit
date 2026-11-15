//go:build integration

// Azure platform integration test for the WithAzure provider path.
//
// The unit tests either side of this seam each cover half of the guarantee —
// that platformBaseURL defers for Azure, and that the openai factory turns a
// deferred base URL into a deployment path. Neither proves the halves meet, and
// #1811 was exactly a break between them: both sides were individually
// defensible and every call still 404d. Only a real request settles it.
//
// Run locally:
//
//	az login --scope https://cognitiveservices.azure.com/.default
//	export AZURE_OPENAI_ENDPOINT=https://<resource>.openai.azure.com
//	export AZURE_OPENAI_DEPLOYMENT=<deployment-name>
//	go test -tags=integration ./sdk/... -run Azure -v
package sdk

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	pkgconfig "github.com/AltairaLabs/PromptKit/pkg/v2/config"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/v2/stt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// azurePlatformConfig builds the platformConfig WithAzure produces, or skips
// when the environment does not name a live deployment.
func azurePlatformConfig(t *testing.T) *platformConfig {
	t.Helper()

	endpoint := os.Getenv("AZURE_OPENAI_ENDPOINT")
	if endpoint == "" {
		t.Skip("AZURE_OPENAI_ENDPOINT not set")
	}
	deployment := os.Getenv("AZURE_OPENAI_DEPLOYMENT")
	if deployment == "" {
		deployment = "gpt-4o-mini"
	}

	return &platformConfig{
		platformType: platformTypeAzure,
		providerType: "openai",
		model:        deployment,
		endpoint:     endpoint,
	}
}

// The regression test for #1811. Before the fix this reached
// {endpoint}/chat/completions and Azure answered 404 for every call; the
// provider still constructed cleanly, so nothing short of a real request
// caught it.
func TestAzurePlatformProviderReachesDeployment(t *testing.T) {
	pc := azurePlatformConfig(t)

	prov, err := resolvePlatformProvider(&config{platform: pc})
	if err != nil {
		t.Fatalf("resolvePlatformProvider: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := prov.Predict(ctx, providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "Reply with the single word: ok"}},
	})
	if err != nil {
		// A 404 here is the original bug resurfacing, so name it rather than
		// letting it read as a generic provider failure.
		if strings.Contains(err.Error(), "404") {
			t.Fatalf("Predict returned 404 — the deployment path is not being built (#1811): %v", err)
		}
		t.Fatalf("Predict: %v", err)
	}
	if strings.TrimSpace(resp.Content) == "" {
		t.Error("Predict returned empty content")
	}
	t.Logf("Azure responded: %q", strings.TrimSpace(resp.Content))
}

// WithAzure and WithPlatformEndpoint write the same field, so a caller who
// worked around #1811 by passing a full deployment URL must keep working rather
// than having a second deployment path stacked onto the first.
func TestAzurePlatformProviderAcceptsFullDeploymentURL(t *testing.T) {
	pc := azurePlatformConfig(t)
	pc.endpoint = strings.TrimRight(pc.endpoint, "/") + "/openai/deployments/" + pc.model

	prov, err := resolvePlatformProvider(&config{platform: pc})
	if err != nil {
		t.Fatalf("resolvePlatformProvider: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := prov.Predict(ctx, providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "Reply with the single word: ok"}},
	})
	if err != nil {
		t.Fatalf("Predict with a full deployment URL: %v", err)
	}
	if strings.TrimSpace(resp.Content) == "" {
		t.Error("Predict returned empty content")
	}
	t.Logf("Azure responded: %q", strings.TrimSpace(resp.Content))
}

// Keyless Azure TTS and STT through the spec options: speech synthesized by
// the TTS deployment transcribes back to the same words on the transcription
// deployment, both authenticated by the Entra token (#2677).
//
//	export AZURE_OPENAI_TTS_DEPLOYMENT=<gpt-4o-mini-tts or tts deployment>
//	export AZURE_OPENAI_TRANSCRIBE_DEPLOYMENT=<gpt-4o-transcribe or whisper deployment>
//	export AZURE_OPENAI_AUDIO_API_VERSION=<api-version>   # optional
func TestAzurePlatformVoiceRoundTrip(t *testing.T) {
	endpoint := os.Getenv("AZURE_OPENAI_ENDPOINT")
	ttsDep, sttDep := os.Getenv("AZURE_OPENAI_TTS_DEPLOYMENT"), os.Getenv("AZURE_OPENAI_TRANSCRIBE_DEPLOYMENT")
	if endpoint == "" || ttsDep == "" || sttDep == "" {
		t.Skip("AZURE_OPENAI_ENDPOINT, AZURE_OPENAI_TTS_DEPLOYMENT and AZURE_OPENAI_TRANSCRIBE_DEPLOYMENT must be set")
	}
	platform := &pkgconfig.PlatformConfig{Type: "azure", Endpoint: endpoint}
	if v := os.Getenv("AZURE_OPENAI_AUDIO_API_VERSION"); v != "" {
		platform.AdditionalConfig = map[string]any{"api_version": v}
	}
	c := &config{}
	if err := WithTTSProvider(ProviderSpec{ID: "voice", Type: "openai", Model: ttsDep, Platform: platform})(c); err != nil {
		t.Fatalf("WithTTSProvider: %v", err)
	}
	if err := WithSTTProvider(ProviderSpec{ID: "ears", Type: "openai", Model: sttDep, Platform: platform})(c); err != nil {
		t.Fatalf("WithSTTProvider: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	rc, err := c.ttsService.Synthesize(ctx, "The quick brown fox jumps over the lazy dog.",
		tts.SynthesisConfig{Voice: "alloy", Format: tts.FormatMP3})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	audio, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil || len(audio) == 0 {
		t.Fatalf("read synthesized audio (%d bytes): %v", len(audio), err)
	}

	text, err := c.sttService.TranscribeBytes(ctx, audio, stt.TranscriptionConfig{Format: "mp3"})
	if err != nil {
		t.Fatalf("TranscribeBytes: %v", err)
	}
	if !strings.Contains(strings.ToLower(text), "fox") {
		t.Fatalf("transcript %q does not contain the synthesized words", text)
	}
	t.Logf("round trip: %d bytes of speech -> %q", len(audio), text)
}
