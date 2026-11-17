package gemini

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// linkageHistory is a conversation whose assistant turn called a tool, whose
// tool message answered it, and whose user then asked a follow-up.
func linkageHistory() []types.Message {
	return []types.Message{
		{Role: "user", Content: "Weather in Paris?"},
		{Role: "assistant", ToolCalls: []types.MessageToolCall{
			{ID: "call_abc", Name: "get_weather", Args: json.RawMessage(`{"city":"Paris"}`)},
		}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "call_abc", Name: "get_weather", Parts: []types.ContentPart{
			types.NewTextPart("18C and sunny"),
		}}},
		{Role: "user", Content: "Should I take a coat?"},
	}
}

// The tool result goes back as a functionResponse naming the function, in a
// turn of its own: gemini-2.5-flash answers a turn that mixes a
// functionResponse with the follow-up text with no parts at all.
const wantLinkedContents = `[
	{"role":"user","parts":[{"text":"Weather in Paris?"}]},
	{"role":"model","parts":[{"functionCall":{"name":"get_weather","args":{"city":"Paris"}}}]},
	{"role":"user","parts":[
		{"functionResponse":{"name":"get_weather","response":{"result":"18C and sunny"}}}]},
	{"role":"user","parts":[{"text":"Should I take a coat?"}]}
]`

func captureContents(t *testing.T, stream bool, run func(p *ToolProvider)) json.RawMessage {
	t.Helper()
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		if stream {
			_, _ = io.WriteString(w, "["+geminiSuccessBody+"]")
			return
		}
		_, _ = io.WriteString(w, geminiSuccessBody)
	}))
	t.Cleanup(server.Close)
	t.Setenv("GEMINI_API_KEY", "k")
	run(NewToolProvider("linkage", "gemini-2.5-flash", server.URL, providers.ProviderDefaults{}, false))
	var got struct {
		Contents json.RawMessage `json:"contents"`
	}
	require.NoError(t, json.Unmarshal(body, &got))
	return got.Contents
}

func TestPredict_PlainPathKeepsToolLinkage(t *testing.T) {
	got := captureContents(t, false, func(p *ToolProvider) {
		_, err := p.Predict(context.Background(), providers.PredictionRequest{Messages: linkageHistory()})
		require.NoError(t, err)
	})
	assert.JSONEq(t, wantLinkedContents, string(got))
}

func TestPredictStream_PlainPathKeepsToolLinkage(t *testing.T) {
	got := captureContents(t, true, func(p *ToolProvider) {
		ch, err := p.PredictStream(context.Background(), providers.PredictionRequest{Messages: linkageHistory()})
		require.NoError(t, err)
		for range ch {
		}
	})
	assert.JSONEq(t, wantLinkedContents, string(got))
}

// A tool result image goes out inside the functionResponse on the plain path,
// as on the tool path, and a Gemini 3 thought signature is replayed.
func TestPredict_PlainPathKeepsToolResultMediaAndSignature(t *testing.T) {
	got := captureContents(t, false, func(p *ToolProvider) {
		_, err := p.Predict(context.Background(), providers.PredictionRequest{Messages: []types.Message{
			{Role: "user", Content: "Take a picture"},
			{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "c1", Name: "camera", Args: json.RawMessage(`{}`),
				ProviderMetadata: map[string]string{providerMetaThoughtSignature: "sig-1"}}}},
			{Role: "tool", ToolResult: &types.MessageToolResult{ID: "c1", Name: "camera", Parts: []types.ContentPart{
				types.NewTextPart("done"),
				types.NewImagePartFromData("aW1n", types.MIMETypeImagePNG, nil),
			}}},
		}})
		require.NoError(t, err)
	})
	assert.JSONEq(t, `[
		{"role":"user","parts":[{"text":"Take a picture"}]},
		{"role":"model","parts":[{"functionCall":{"name":"camera","args":{}},"thoughtSignature":"sig-1"}]},
		{"role":"user","parts":[{"functionResponse":{"name":"camera","response":{
			"text":"done","inlineData":{"mimeType":"image/png","data":"aW1n"}}}}]}
	]`, string(got))
}

// The plain and tool paths serialize one history to the same contents.
func TestPlainAndToolPathsSerializeHistoryIdentically(t *testing.T) {
	history := append(linkageHistory(),
		types.Message{Role: "assistant", Content: "Checking.", ToolCalls: []types.MessageToolCall{
			{ID: "c1", Name: "get_weather", Args: json.RawMessage(`{"city":"Lyon"}`)},
			{ID: "c2", Name: "get_weather", Args: nil},
		}},
		types.Message{Role: "tool", ToolResult: &types.MessageToolResult{ID: "c1", Name: "get_weather",
			Parts: []types.ContentPart{types.NewTextPart(`{"temp":12}`)}}},
		types.Message{Role: "tool", ToolResult: &types.MessageToolResult{ID: "c2", Name: "get_weather",
			Parts: []types.ContentPart{types.NewTextPart("9C")}}},
		types.Message{Role: "assistant", Content: "Lyon is 12C."},
		types.Message{Role: "user", Parts: []types.ContentPart{types.NewTextPart("and this?"),
			types.NewImagePartFromData("aW1n", types.MIMETypeImagePNG, nil)}},
	)
	req := providers.PredictionRequest{System: "be brief", MaxTokens: 50, Messages: history}

	plain := captureContents(t, false, func(p *ToolProvider) {
		_, err := p.Predict(context.Background(), req)
		require.NoError(t, err)
	})
	withTools := captureContents(t, false, func(p *ToolProvider) {
		tools, err := p.BuildTooling([]*providers.ToolDescriptor{{
			Name: "get_weather", Description: "w", InputSchema: json.RawMessage(`{"type":"object"}`),
		}})
		require.NoError(t, err)
		_, _, err = p.PredictWithTools(context.Background(), req, tools, "auto")
		require.NoError(t, err)
	})

	assert.JSONEq(t, string(withTools), string(plain))
	var contents []map[string]any
	require.NoError(t, json.Unmarshal(plain, &contents))
	assert.Len(t, contents, 8)
}

// convertContents serializes msgs as every request path does, decoded into
// the typed content shape for field-level assertions.
func convertContents(t *testing.T, p *Provider, msgs []types.Message) []geminiContent {
	t.Helper()
	maps, err := p.buildToolContents(context.Background(), msgs)
	require.NoError(t, err)
	var contents []geminiContent
	b, err := json.Marshal(maps)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &contents))
	return contents
}

// contentMaps encodes typed contents in the request's wire shape.
func contentMaps(t *testing.T, contents []geminiContent) []map[string]any {
	t.Helper()
	b, err := json.Marshal(contents)
	require.NoError(t, err)
	var maps []map[string]any
	require.NoError(t, json.Unmarshal(b, &maps))
	return maps
}

// convertOneMessage serializes one message as every request path does and
// returns its content, or the zero content when the message sends nothing.
func convertOneMessage(msg types.Message) (geminiContent, error) {
	maps, err := convProvider().buildToolContents(context.Background(), []types.Message{msg})
	if err != nil || len(maps) == 0 {
		return geminiContent{}, err
	}
	b, err := json.Marshal(maps[0])
	if err != nil {
		return geminiContent{}, err
	}
	var c geminiContent
	err = json.Unmarshal(b, &c)
	return c, err
}

// convertOnePart serializes a user message holding only part and returns the
// part as sent.
func convertOnePart(part types.ContentPart) (geminiPart, error) {
	return convertOnePartWith(convProvider(), part)
}

func convertOnePartWith(p *Provider, part types.ContentPart) (geminiPart, error) {
	maps, err := p.buildToolContents(context.Background(),
		[]types.Message{{Role: "user", Parts: []types.ContentPart{part}}})
	if err != nil || len(maps) == 0 {
		return geminiPart{}, err
	}
	b, err := json.Marshal(maps[0])
	if err != nil {
		return geminiPart{}, err
	}
	var c geminiContent
	if err := json.Unmarshal(b, &c); err != nil || len(c.Parts) == 0 {
		return geminiPart{}, err
	}
	return c.Parts[0], nil
}
