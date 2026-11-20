package tts_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
)

// elevenLabsTextServer records the text field of every synthesis request.
func elevenLabsTextServer(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var texts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		texts = append(texts, body.Text)
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("mp3"))
	}))
	t.Cleanup(srv.Close)
	return srv, &texts
}

func elevenLabsFromSpec(t *testing.T, url, model string, extra map[string]any) tts.Service {
	t.Helper()
	svc, err := tts.CreateFromSpec(tts.ProviderSpec{
		ID: "voice", Type: "elevenlabs", Model: model, BaseURL: url,
		Credential:       credentials.NewAPIKeyCredential("k"),
		AdditionalConfig: extra,
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	return svc
}

func synthesizeText(t *testing.T, svc tts.Service, text string) {
	t.Helper()
	rc, err := svc.Synthesize(context.Background(), text, tts.SynthesisConfig{Voice: "v"})
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	_, _ = io.Copy(io.Discard, rc)
	_ = rc.Close()
}

const taggedLine = "[whispers] hello there"

// A tag-capable model whose ID does not start with eleven_v3 keeps its tags,
// and is offered the rubric, when additional_config.inline_tags declares it.
func TestElevenLabs_DeclaredInlineTagsPassThrough(t *testing.T) {
	srv, texts := elevenLabsTextServer(t)
	svc := elevenLabsFromSpec(t, srv.URL, "eleven_expressive_v4", map[string]any{"inline_tags": true})

	synthesizeText(t, svc, taggedLine)

	if len(*texts) != 1 || (*texts)[0] != taggedLine {
		t.Errorf("sent %q, want %q", *texts, taggedLine)
	}
	if svc.(tts.PersonaRubricProvider).PersonaRubric() == "" {
		t.Error("a declared tag-capable model must be offered the markup rubric")
	}
}

// inline_tags: false strips the tags even on eleven_v3, and offers no rubric.
func TestElevenLabs_DeclaredNoInlineTagsStrips(t *testing.T) {
	srv, texts := elevenLabsTextServer(t)
	svc := elevenLabsFromSpec(t, srv.URL, tts.ElevenLabsModelV3, map[string]any{"inline_tags": false})

	synthesizeText(t, svc, taggedLine)

	if len(*texts) != 1 || (*texts)[0] != " hello there" {
		t.Errorf("sent %q, want the line without its tag", *texts)
	}
	if got := svc.(tts.PersonaRubricProvider).PersonaRubric(); got != "" {
		t.Errorf("rubric = %q, want none", got)
	}
	if got := svc.(tts.SpokenTextReporter).SpokenText(taggedLine, tts.SynthesisConfig{}); got != " hello there" {
		t.Errorf("spoken text = %q, want %q", got, " hello there")
	}
}

// Undeclared, the model ID decides, as before.
func TestElevenLabs_UndeclaredInlineTagsFollowTheModel(t *testing.T) {
	srv, texts := elevenLabsTextServer(t)
	synthesizeText(t, elevenLabsFromSpec(t, srv.URL, tts.ElevenLabsModelV3, nil), taggedLine)
	synthesizeText(t, elevenLabsFromSpec(t, srv.URL, "eleven_expressive_v4", nil), taggedLine)

	want := []string{taggedLine, " hello there"}
	if len(*texts) != 2 || (*texts)[0] != want[0] || (*texts)[1] != want[1] {
		t.Errorf("sent %q, want %q", *texts, want)
	}
}

func TestElevenLabs_InlineTagsMustBeABool(t *testing.T) {
	_, err := tts.CreateFromSpec(tts.ProviderSpec{ID: "voice", Type: "elevenlabs",
		AdditionalConfig: map[string]any{"inline_tags": "yes"}})
	want := `elevenlabs tts provider "voice": additional_config.inline_tags must be true or false, got "yes"`
	if err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
}
