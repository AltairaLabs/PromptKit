package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// With api_mode interactions every turn goes to the Interactions API, a turn
// with no tools (Predict / PredictStream) as well as a tool turn. These tests
// drive the public entry points of a provider built from a spec, as a host
// builds it, against a server that records where each request went.

// routeServer records each request's path and body and answers the
// Interactions endpoint with unary or SSE bodies, and generateContent with a
// minimal candidate.
type routeServer struct {
	url string

	mu     sync.Mutex
	paths  []string
	bodies [][]byte
}

func newRouteServer(t *testing.T, unary, stream string) *routeServer {
	t.Helper()
	s := &routeServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.paths = append(s.paths, r.URL.Path)
		s.bodies = append(s.bodies, body)
		s.mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, ":streamGenerateContent"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `[{"candidates":[{"content":{"role":"model","parts":[{"text":"GC"}]},`+
				`"finishReason":"STOP"}]}]`)
		case strings.HasSuffix(r.URL.Path, ":generateContent"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"GC"}]},`+
				`"finishReason":"STOP"}]}`)
		case strings.Contains(string(body), `"stream":true`):
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, stream)
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, unary)
		}
	}))
	t.Cleanup(srv.Close)
	s.url = srv.URL
	return s
}

func (s *routeServer) sentPaths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...)
}

// lastBody decodes the last request body.
func (s *routeServer) lastBody(t *testing.T) map[string]any {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.bodies) == 0 {
		t.Fatal("no request reached the server")
	}
	var body map[string]any
	if err := json.Unmarshal(s.bodies[len(s.bodies)-1], &body); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	return body
}

// newSpecProvider builds a gemini provider from a spec with the given
// additional_config, as a host does.
func newSpecProvider(t *testing.T, url string, cfg map[string]any) providers.Provider {
	t.Helper()
	t.Setenv("GEMINI_API_KEY", "test-key")
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: uniqueProviderID("int-all"), Type: "gemini", Model: "gemini-3.7-flash", BaseURL: url,
		AdditionalConfig: cfg,
	})
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}
	return p
}

var interactionsMode = map[string]any{"api_mode": "interactions"}

// streamTurn drains a stream into a response: the final content, finish
// reason and cost, the concatenated reasoning and opaque reasoning.
func streamTurn(t *testing.T, ch <-chan providers.StreamChunk) providers.PredictionResponse {
	t.Helper()
	var out providers.PredictionResponse
	for c := range ch {
		if c.Error != nil {
			t.Fatalf("stream chunk error: %v", c.Error)
		}
		out.Content = c.Content
		if c.FinishReason != nil {
			out.FinishReason = *c.FinishReason
		}
		if c.CostInfo != nil {
			out.CostInfo = c.CostInfo
		}
		if len(c.OpaqueReasoning) > 0 {
			if out.Reasoning == nil {
				out.Reasoning = &types.ReasoningTrace{}
			}
			out.Reasoning.Opaque = append(out.Reasoning.Opaque, c.OpaqueReasoning...)
		}
	}
	return out
}

// predictTurn runs one tool-less turn through Predict or PredictStream.
func predictTurn(
	t *testing.T, p providers.Provider, req providers.PredictionRequest, stream bool,
) providers.PredictionResponse {
	t.Helper()
	if !stream {
		resp, err := p.Predict(context.Background(), req)
		if err != nil {
			t.Fatalf("Predict: %v", err)
		}
		return resp
	}
	ch, err := p.PredictStream(context.Background(), req)
	if err != nil {
		t.Fatalf("PredictStream: %v", err)
	}
	return streamTurn(t, ch)
}

const (
	allTurnsUnary = `{"id":"i1","status":"completed","steps":[
	  {"type":"thought","signature":"SIG"},
	  {"type":"model_output","content":[{"type":"text","text":"OK"}]}],
	  "usage":{"total_input_tokens":7,"total_output_tokens":2,"total_thought_tokens":5}}`
	allTurnsStream = "event: step.start\n" +
		`data: {"index":0,"step":{"type":"thought"}}` + "\n\n" +
		"event: step.delta\n" +
		`data: {"index":0,"delta":{"type":"thought_signature","signature":"SIG"}}` + "\n\n" +
		"event: step.start\n" +
		`data: {"index":1,"step":{"type":"model_output"}}` + "\n\n" +
		"event: step.delta\n" +
		`data: {"index":1,"delta":{"type":"text","text":"OK"}}` + "\n\n" +
		"event: interaction.completed\n" +
		`data: {"interaction":{"id":"i1","status":"completed","usage":{"total_input_tokens":7,` +
		`"total_output_tokens":2,"total_thought_tokens":5}}}` + "\n\n"
)

// A turn with no tools reaches the Interactions endpoint, with the same
// answer, finish reason, cost and replayable reasoning a tool turn gets.
// Without api_mode it still goes to generateContent.
func TestInteractions_ToolLessTurnsUseTheConfiguredAPI(t *testing.T) {
	req := providers.PredictionRequest{
		System:   "be terse",
		Messages: []types.Message{{Role: roleUser, Content: "hi"}},
	}
	for _, stream := range []bool{false, true} {
		name := map[bool]string{false: "unary", true: "stream"}[stream]

		t.Run(name+"/interactions", func(t *testing.T) {
			srv := newRouteServer(t, allTurnsUnary, allTurnsStream)
			resp := predictTurn(t, newSpecProvider(t, srv.url, interactionsMode), req, stream)

			if got := strings.Join(srv.sentPaths(), ","); got != "/interactions" {
				t.Fatalf("requests went to %q, want one to /interactions", got)
			}
			body := srv.lastBody(t)
			if got, want := canonicalJSON(t, body["input"]),
				`[{"content":[{"text":"hi","type":"text"}],"type":"user_input"}]`; got != want {
				t.Errorf("input = %s, want %s", got, want)
			}
			if body["system_instruction"] != "be terse" {
				t.Errorf("system_instruction = %v, want %q", body["system_instruction"], "be terse")
			}
			if _, hasTools := body["tools"]; hasTools {
				t.Errorf("a tool-less turn sent tools: %v", body["tools"])
			}
			if resp.Content != "OK" || resp.FinishReason != types.FinishReasonStop {
				t.Errorf("content, finish = %q, %q; want %q, %q",
					resp.Content, resp.FinishReason, "OK", types.FinishReasonStop)
			}
			if resp.CostInfo == nil || resp.CostInfo.InputTokens != 7 || resp.CostInfo.OutputTokens != 2 {
				t.Errorf("cost = %+v, want 7 input and 2 output tokens", resp.CostInfo)
			}
			if resp.Reasoning == nil || len(resp.Reasoning.Opaque) != 1 ||
				resp.Reasoning.Opaque[0] != (types.OpaqueReasoning{
					Provider: providerNameGemini, Kind: kindInteractionsThought, Data: "SIG"}) {
				t.Errorf("reasoning = %+v, want the thought signature to replay", resp.Reasoning)
			}
		})

		t.Run(name+"/default", func(t *testing.T) {
			srv := newRouteServer(t, allTurnsUnary, allTurnsStream)
			resp := predictTurn(t, newSpecProvider(t, srv.url, nil), req, stream)
			action := map[bool]string{false: ":generateContent", true: ":streamGenerateContent"}[stream]
			if got, want := strings.Join(srv.sentPaths(), ","), "/models/gemini-3.7-flash"+action; got != want {
				t.Errorf("requests went to %q, want %q", got, want)
			}
			if resp.Content != "GC" {
				t.Errorf("content = %q, want %q", resp.Content, "GC")
			}
		})
	}
}

// A JSON-schema turn with no tools carries the schema in response_format.
func TestInteractions_ToolLessTurnCarriesTheResponseSchema(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "unary", true: "stream"}[stream], func(t *testing.T) {
			srv := newRouteServer(t, allTurnsUnary, allTurnsStream)
			predictTurn(t, newSpecProvider(t, srv.url, interactionsMode), providers.PredictionRequest{
				Messages:       []types.Message{{Role: roleUser, Content: "capital of France?"}},
				ResponseFormat: schemaFormat(),
			}, stream)
			want := `{"mime_type":"application/json","schema":{"properties":{"a":{"type":"string"}},` +
				`"type":"object"},"type":"text"}`
			if got := canonicalJSON(t, srv.lastBody(t)["response_format"]); got != want {
				t.Errorf("response_format = %s, want %s", got, want)
			}
		})
	}
}

// A document the Interactions API does not accept (it reads PDF, CSV, Office
// and OpenDocument only) fails a tool-less turn before any request, with an
// UnsupportedContentError naming it. generateContent reads it as text.
func TestInteractions_ToolLessTurnRefusesUnacceptedDocument(t *testing.T) {
	msg := types.Message{Role: roleUser}
	msg.AddTextPart("summarize")
	msg.AddPart(types.ContentPart{Type: types.ContentTypeDocument, Media: &types.MediaContent{
		Data: strRef("aGVsbG8="), MIMEType: "text/markdown"}})
	req := providers.PredictionRequest{Messages: []types.Message{msg}}

	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "unary", true: "stream"}[stream], func(t *testing.T) {
			srv := newRouteServer(t, allTurnsUnary, allTurnsStream)
			p := newSpecProvider(t, srv.url, interactionsMode)
			var err error
			if stream {
				_, err = p.PredictStream(context.Background(), req)
			} else {
				_, err = p.Predict(context.Background(), req)
			}
			var unsupported *providers.UnsupportedContentError
			if !errors.As(err, &unsupported) {
				t.Fatalf("want UnsupportedContentError, got %v", err)
			}
			if unsupported.ContentType != types.ContentTypeDocument || unsupported.MIMEType != "text/markdown" ||
				unsupported.PartIndex != 1 {
				t.Errorf("error = %+v, want the text/markdown document at part 1", unsupported)
			}
			if want := "the Gemini Interactions API does not accept document format text/markdown"; !strings.Contains(
				err.Error(), want) {
				t.Errorf("error %q does not say %q", err, want)
			}
			if paths := srv.sentPaths(); len(paths) != 0 {
				t.Errorf("requests went out without the document: %v", paths)
			}
		})
	}
}

// An image or audio the model generates comes back as a model_output content
// item (captured live: gemini-3.1-flash-lite-image returned image/jpeg, a TTS
// model audio/wav). It reaches the response's Parts beside the text, as an
// inline-data part does on generateContent.
func TestInteractions_GeneratedMediaReachesParts(t *testing.T) {
	// The thought's text is reasoning and stays out of Parts.
	unary := `{"id":"i1","status":"completed","steps":[
	  {"type":"thought","signature":"SIG","content":[{"type":"text","text":"planning"}]},
	  {"type":"model_output","content":[{"type":"text","text":"Here:"},
	    {"type":"image","mime_type":"image/jpeg","data":"aW1n"},
	    {"type":"audio","mime_type":"audio/wav","data":"YXVk"}]}]}`
	srv := newRouteServer(t, unary, "")
	resp := predictTurn(t, newSpecProvider(t, srv.url, interactionsMode), providers.PredictionRequest{
		Messages: []types.Message{{Role: roleUser, Content: "draw a red square"}},
	}, false)

	want := []types.ContentPart{
		types.NewTextPart("Here:"),
		types.NewImagePartFromData("aW1n", "image/jpeg", nil),
		types.NewAudioPartFromData("YXVk", "audio/wav"),
	}
	if got, w := canonicalJSON(t, resp.Parts), canonicalJSON(t, want); got != w {
		t.Errorf("parts = %s, want %s", got, w)
	}
	if resp.Content != "Here:" {
		t.Errorf("content = %q, want %q", resp.Content, "Here:")
	}
}

// A text-only answer leaves Parts empty, as a tool turn on either API does:
// Content carries it.
func TestInteractions_TextAnswerHasNoParts(t *testing.T) {
	srv := newRouteServer(t, allTurnsUnary, "")
	resp := predictTurn(t, newSpecProvider(t, srv.url, interactionsMode), providers.PredictionRequest{
		Messages: []types.Message{{Role: roleUser, Content: "hi"}},
	}, false)
	if len(resp.Parts) != 0 {
		t.Errorf("parts = %+v, want none", resp.Parts)
	}
}

// The Interactions API has no explicit context caching: it answers a
// cached_content field with HTTP 400 "Unknown parameter" (probed live). A
// config that asks for both is refused when the provider is built, rather
// than every turn paying the full input price the caller meant to cache.
func TestInteractions_ExplicitCachingIsRefusedAtConstruction(t *testing.T) {
	build := func(cfg map[string]any) error {
		t.Setenv("GEMINI_API_KEY", "test-key")
		_, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
			ID: "gemini-cache-mode", Type: "gemini", Model: "gemini-3.7-flash",
			BaseURL: "http://unused.invalid", AdditionalConfig: cfg,
		})
		return err
	}

	err := build(map[string]any{"api_mode": "interactions", "explicit_caching": true})
	want := `gemini provider "gemini-cache-mode": additional_config.explicit_caching is not supported ` +
		`with api_mode interactions: the Interactions API has no explicit context caching; ` +
		`remove explicit_caching or use api_mode generate_content`
	if err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}

	for _, ok := range []map[string]any{
		{"api_mode": "interactions", "explicit_caching": false},
		{"api_mode": "generate_content", "explicit_caching": true},
		{"explicit_caching": true},
		{"api_mode": "interactions"},
	} {
		if err := build(ok); err != nil {
			t.Errorf("config %v: unexpected error %v", ok, err)
		}
	}
}

// A streamed turn does not deliver generated media: the stream carries text,
// reasoning and tool calls only, as a generateContent stream does. A media
// delta ({"type":"image","mime_type":…,"data":…}, captured live) is not
// dropped silently: a warning names it, and the text still arrives.
func TestInteractions_StreamedGeneratedMediaWarns(t *testing.T) {
	stream := "event: step.start\n" +
		`data: {"index":0,"step":{"type":"model_output"}}` + "\n\n" +
		"event: step.delta\n" +
		`data: {"index":0,"delta":{"type":"text","text":"Here:"}}` + "\n\n" +
		"event: step.delta\n" +
		`data: {"index":0,"delta":{"type":"image","mime_type":"image/jpeg","data":"aW1n"}}` + "\n\n" +
		"event: interaction.completed\n" +
		`data: {"interaction":{"id":"i1","status":"completed"}}` + "\n\n"
	logs := captureWarnings(t)
	srv := newRouteServer(t, "", stream)
	p := newSpecProvider(t, srv.url, interactionsMode)
	resp := predictTurn(t, p, providers.PredictionRequest{
		Messages: []types.Message{{Role: roleUser, Content: "draw"}},
	}, true)
	if resp.Content != "Here:" {
		t.Errorf("content = %q, want %q", resp.Content, "Here:")
	}
	want := "gemini interactions: generated media is not delivered on a stream"
	if !strings.Contains(logs.String(), want) || !strings.Contains(logs.String(), "mime_type=image/jpeg") {
		t.Errorf("logs %q lack %q for image/jpeg", logs.String(), want)
	}
}

// The provider stage checks a request's media against what the provider can
// send (providers.ValidateMultimodalRequest) before calling it. With every
// turn on the Interactions API, that is the Interactions API's list: a BMP
// image it takes passes, and a Markdown document it refuses fails before the
// provider is called. Without api_mode the generateContent list applies.
func TestInteractions_SendableFormatsFollowTheAPI(t *testing.T) {
	part := func(kind, mime string) types.ContentPart {
		return types.ContentPart{Type: kind, Media: &types.MediaContent{Data: strRef("eA=="), MIMEType: mime}}
	}
	validate := func(cfg map[string]any, p types.ContentPart) error {
		msg := types.Message{Role: roleUser}
		msg.AddTextPart("look")
		msg.AddPart(p)
		return providers.ValidateMultimodalRequest(newSpecProvider(t, "http://unused.invalid", cfg),
			&providers.PredictionRequest{Messages: []types.Message{msg}})
	}
	errText := func(err error) string {
		if err == nil {
			return "<nil>"
		}
		return err.Error()
	}

	for _, tc := range []struct {
		name string
		cfg  map[string]any
		part types.ContentPart
		want string
	}{
		{"interactions takes BMP", interactionsMode, part(types.ContentTypeImage, "image/bmp"), "<nil>"},
		{"interactions takes PDF", interactionsMode, part(types.ContentTypeDocument, types.MIMETypePDF), "<nil>"},
		{"interactions refuses Markdown", interactionsMode, part(types.ContentTypeDocument, "text/markdown"),
			"unsupported document format: text/markdown"},
		{"interactions refuses video/flv", interactionsMode, part(types.ContentTypeVideo, "video/flv"),
			"unsupported video format: video/flv"},
		{"generateContent reads Markdown", nil, part(types.ContentTypeDocument, "text/markdown"), "<nil>"},
		{"generateContent refuses BMP", nil, part(types.ContentTypeImage, "image/bmp"),
			"unsupported image format: image/bmp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := errText(validate(tc.cfg, tc.part))
			if tc.want == "<nil>" && got != "<nil>" || !strings.Contains(got, tc.want) {
				t.Errorf("validation = %q, want %q", got, tc.want)
			}
		})
	}
}
