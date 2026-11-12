package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// optionalProps is a schema with n optional string properties and one required.
func optionalProps(n int) json.RawMessage {
	props := []string{`"id":{"type":"string"}`}
	for i := 0; i < n; i++ {
		props = append(props, fmt.Sprintf(`"p%d":{"type":"string"}`, i))
	}
	return json.RawMessage(`{"type":"object","properties":{` + strings.Join(props, ",") +
		`},"required":["id"],"additionalProperties":false}`)
}

// unionProps is a schema with n required properties typed string-or-null.
func unionProps(n int) json.RawMessage {
	var props, req []string
	for i := 0; i < n; i++ {
		props = append(props, fmt.Sprintf(`"u%d":{"type":["string","null"]}`, i))
		req = append(req, fmt.Sprintf(`"u%d"`, i))
	}
	return json.RawMessage(`{"type":"object","properties":{` + strings.Join(props, ",") +
		`},"required":[` + strings.Join(req, ",") + `],"additionalProperties":false}`)
}

// sentTools sends one tool round through a Claude provider with default
// config and returns the tools as the API received them: name to strict flag,
// and name to input_schema.
func sentTools(
	t *testing.T, descriptors []*providers.ToolDescriptor, rf *providers.ResponseFormat,
) (map[string]bool, map[string]json.RawMessage) {
	t.Helper()
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	var captured map[string]interface{}
	server := newParamCaptureServer(t, &captured, false)
	p, err := providers.CreateProviderFromSpec(newClaudeSpec(server.URL, nil))
	require.NoError(t, err)
	tp := p.(*ToolProvider)
	tools, err := tp.BuildTooling(descriptors)
	require.NoError(t, err)
	_, _, err = tp.PredictWithTools(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}}, ResponseFormat: rf,
	}, tools, "auto")
	require.NoError(t, err)

	raw, err := json.Marshal(captured["tools"])
	require.NoError(t, err)
	var wire []struct {
		Name        string          `json:"name"`
		Strict      bool            `json:"strict"`
		InputSchema json.RawMessage `json:"input_schema"`
	}
	require.NoError(t, json.Unmarshal(raw, &wire))
	strict := map[string]bool{}
	schemas := map[string]json.RawMessage{}
	for _, w := range wire {
		strict[w.Name] = w.Strict
		schemas[w.Name] = w.InputSchema
	}
	return strict, schemas
}

func tool(name string, schema json.RawMessage) *providers.ToolDescriptor {
	return &providers.ToolDescriptor{Name: name, Description: name, InputSchema: schema}
}

// Anthropic refuses a request whose strict schemas carry more than 24 optional
// parameters between them. Tools stay strict in the order they are declared
// while they fit; a tool that would go over is sent non-strict, with the
// caller's schema, and a later tool that fits is still strict.
func TestStrictTools_OptionalParametersOverTheLimitAreSentNonStrict(t *testing.T) {
	small := json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}`)
	// grep's schema carries a keyword strict adaptation strips, so the schema
	// sent shows whether it was adapted.
	grep := json.RawMessage(strings.Replace(string(optionalProps(13)),
		`"p0":{"type":"string"}`, `"p0":{"type":"integer","minimum":1}`, 1))
	strict, schemas := sentTools(t, []*providers.ToolDescriptor{
		tool("read", optionalProps(13)), tool("grep", grep), tool("bash", small),
	}, nil)
	assert.Equal(t, map[string]bool{"read": true, "grep": false, "bash": true}, strict)
	assert.JSONEq(t, string(grep), string(schemas["grep"]), "a non-strict tool keeps the caller's schema")
}

// Optional parameters of nested objects and array items count too (measured
// against api.anthropic.com).
func TestStrictTools_NestedOptionalParametersCount(t *testing.T) {
	nested := json.RawMessage(`{"type":"object","properties":{"o":{"type":"object","properties":{` +
		`"a":{"type":"array","items":` + string(optionalProps(21)) + `}},"required":["a"]}},"required":["o"]}`)
	strict, _ := sentTools(t, []*providers.ToolDescriptor{
		tool("first", optionalProps(4)), tool("nested", nested),
	}, nil)
	assert.Equal(t, map[string]bool{"first": true, "nested": false}, strict)
}

// A JSON output schema shares the optional-parameter budget and is always
// constrained, so the tools get what it leaves.
func TestStrictTools_OutputSchemaSharesTheBudget(t *testing.T) {
	rf := &providers.ResponseFormat{Type: providers.ResponseFormatJSONSchema, JSONSchema: optionalProps(20)}
	strict, _ := sentTools(t, []*providers.ToolDescriptor{
		tool("fits", optionalProps(4)), tool("over", optionalProps(1)),
	}, rf)
	assert.Equal(t, map[string]bool{"fits": true, "over": false}, strict)
}

// At most 20 tools may be strict.
func TestStrictTools_TwentyFirstToolIsNonStrict(t *testing.T) {
	var descriptors []*providers.ToolDescriptor
	for i := 0; i < 21; i++ {
		descriptors = append(descriptors, tool(fmt.Sprintf("t%02d", i), optionalProps(0)))
	}
	strict, _ := sentTools(t, descriptors, nil)
	assert.True(t, strict["t19"])
	assert.False(t, strict["t20"])
}

// At most 16 parameters may use a union type (anyOf or a type array).
func TestStrictTools_UnionParametersOverTheLimitAreSentNonStrict(t *testing.T) {
	anyOfTen := json.RawMessage(strings.ReplaceAll(string(unionProps(10)),
		`{"type":["string","null"]}`, `{"anyOf":[{"type":"string"},{"type":"null"}]}`))
	strict, _ := sentTools(t, []*providers.ToolDescriptor{
		tool("ten", anyOfTen), tool("seven", unionProps(7)), tool("six", unionProps(6)),
	}, nil)
	assert.Equal(t, map[string]bool{"ten": true, "seven": false, "six": true}, strict)
}

// warnRecords collects the warnings logged.
type warnRecords struct {
	mu   sync.Mutex
	msgs []string
}

func (h *warnRecords) Enabled(_ context.Context, l slog.Level) bool { return l >= slog.LevelWarn }
func (h *warnRecords) Handle(_ context.Context, r slog.Record) error {
	msg := r.Message
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "tools" {
			msg += " tools=" + a.Value.String()
		}
		return true
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	h.msgs = append(h.msgs, msg)
	return nil
}
func (h *warnRecords) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *warnRecords) WithGroup(string) slog.Handler      { return h }

// With strict_tools: false nothing is strict, so nothing is over the limits
// and nothing is reported as dropped.
func TestStrictTools_StrictOffReportsNothing(t *testing.T) {
	h := &warnRecords{}
	logger.SetLogger(slog.New(h))
	t.Cleanup(func() { logger.SetLogger(nil) })
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	var captured map[string]interface{}
	server := newParamCaptureServer(t, &captured, false)
	spec := newClaudeSpec(server.URL, nil)
	spec.AdditionalConfig = map[string]interface{}{"strict_tools": false}
	p, err := providers.CreateProviderFromSpec(spec)
	require.NoError(t, err)
	tp := p.(*ToolProvider)
	descriptors := []*providers.ToolDescriptor{tool("read", optionalProps(13)), tool("grep", optionalProps(13))}
	for i := 0; i < 20; i++ {
		descriptors = append(descriptors, tool(fmt.Sprintf("t%02d", i), optionalProps(0)))
	}
	tools, err := tp.BuildTooling(descriptors)
	require.NoError(t, err)
	_, _, err = tp.PredictWithTools(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	}, tools, "auto")
	require.NoError(t, err)
	assert.Empty(t, h.msgs)
}

// Dropping strict is a warning naming the tools, logged once however many
// rounds send the same tool set.
func TestStrictTools_DemotionIsWarnedOncePerToolSet(t *testing.T) {
	h := &warnRecords{}
	logger.SetLogger(slog.New(h))
	t.Cleanup(func() { logger.SetLogger(nil) })
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	var captured map[string]interface{}
	server := newParamCaptureServer(t, &captured, false)
	p, err := providers.CreateProviderFromSpec(newClaudeSpec(server.URL, nil))
	require.NoError(t, err)
	tp := p.(*ToolProvider)
	tools, err := tp.BuildTooling([]*providers.ToolDescriptor{
		tool("read", optionalProps(13)), tool("grep", optionalProps(13)),
	})
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		_, _, err = tp.PredictWithTools(context.Background(), providers.PredictionRequest{
			Messages: []types.Message{{Role: "user", Content: "hi"}},
		}, tools, "auto")
		require.NoError(t, err)
	}
	var demotions []string
	for _, m := range h.msgs {
		if strings.Contains(m, "without strict tool use") {
			demotions = append(demotions, m)
		}
	}
	require.Len(t, demotions, 1)
	assert.True(t, strings.HasSuffix(demotions[0], " tools=grep"), demotions[0])
}
