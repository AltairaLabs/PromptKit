package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// ---------------------------------------------------------------------------
// OTel tracing integration tests
//
// These verify that spans are created end-to-end through a real SDK
// conversation for tool calls and evals — areas not covered by the
// white-box tests in sdk/otel_integration_test.go.
// ---------------------------------------------------------------------------

// testOTelSetup creates a TracerProvider with an in-memory exporter,
// an EventBus, and returns helpers for span assertions.
func testOTelSetup(t *testing.T) (*events.EventBus, *sdktrace.TracerProvider, *tracetest.InMemoryExporter) {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Close() })

	return bus, tp, exp
}

// flushAndGetSpans flushes the tracer and returns span name set.
func flushAndGetSpans(t *testing.T, tp *sdktrace.TracerProvider, exp *tracetest.InMemoryExporter) map[string]bool {
	t.Helper()
	require.NoError(t, tp.ForceFlush(context.Background()))
	spans := exp.GetSpans()
	names := make(map[string]bool, len(spans))
	for _, s := range spans {
		names[s.Name] = true
	}
	return names
}

// ---------------------------------------------------------------------------
// Tool call spans
// ---------------------------------------------------------------------------

func TestOTel_ToolCallSpans(t *testing.T) {
	bus, tp, exp := testOTelSetup(t)

	repo := newTestTurnRepository()
	repo.addTurn("default", 1, mock.Turn{
		Type:    "tool_calls",
		Content: "Let me check the weather",
		ToolCalls: []mock.ToolCall{{
			Name:      "get_weather",
			Arguments: map[string]interface{}{"city": "London"},
		}},
	})
	repo.addTurn("default", 2, mock.Turn{
		Type:    "text",
		Content: "The weather in London is sunny.",
	})

	conv := openToolConv(t, repo, map[string]func(args map[string]any) (any, error){
		"get_weather": func(args map[string]any) (any, error) {
			return map[string]any{"temperature": 22}, nil
		},
	},
		sdk.WithEventBus(bus),
		sdk.WithTracerProvider(tp),
	)

	_, err := conv.Send(context.Background(), "What is the weather?")
	require.NoError(t, err)

	// Allow async event dispatch.
	time.Sleep(200 * time.Millisecond)

	spanNames := flushAndGetSpans(t, tp, exp)

	assert.True(t, spanNames["execute_tool"], "expected execute_tool span for tool call")
	assert.True(t, spanNames["promptkit.pipeline"], "expected promptkit.pipeline span")

	// Provider span (e.g., "mock-test chat")
	hasProvider := false
	for name := range spanNames {
		if len(name) > 5 && name[len(name)-5:] == " chat" {
			hasProvider = true
			break
		}
	}
	assert.True(t, hasProvider, "expected provider span ending in ' chat'")
}

// ---------------------------------------------------------------------------
// Eval spans
// ---------------------------------------------------------------------------

func TestOTel_EvalSpans(t *testing.T) {
	bus, tp, exp := testOTelSetup(t)

	registry := evals.NewEvalTypeRegistry()
	runner := evals.NewEvalRunner(registry)

	conv := openTestConvWithPack(t, evalsPackJSON, "chat",
		sdk.WithEventBus(bus),
		sdk.WithTracerProvider(tp),
		sdk.WithEvalRunner(runner),
	)

	_, err := conv.Send(context.Background(), "Hello")
	require.NoError(t, err)

	// Evals run async — give them time.
	time.Sleep(300 * time.Millisecond)

	spanNames := flushAndGetSpans(t, tp, exp)

	// Each eval should produce a span named "promptkit.eval.{evalID}".
	assert.True(t, spanNames["promptkit.eval.check-response-length"],
		"expected span for check-response-length eval")
	assert.True(t, spanNames["promptkit.eval.check-no-forbidden"],
		"expected span for check-no-forbidden eval")
	assert.True(t, spanNames["promptkit.eval.check-groupB-only"],
		"expected span for check-groupB-only eval")
}

// ---------------------------------------------------------------------------
// Combined: tool + eval spans in same trace
// ---------------------------------------------------------------------------

func TestOTel_ToolAndEvalSpansCoexist(t *testing.T) {
	bus, tp, exp := testOTelSetup(t)

	// Pack with both tools and evals.
	const toolsAndEvalsPackJSON = `{
		"id": "otel-combined-test",
		"version": "1.0.0",
		"description": "Pack with tools and evals",
		"prompts": {
			"chat": {
				"id": "chat",
				"name": "Chat",
				"system_template": "You are a helpful assistant.",
				"tools": ["get_weather"]
			}
		},
		"tools": {
			"get_weather": {
				"name": "get_weather",
				"description": "Get weather",
				"mode": "local",
				"parameters": {
					"type": "object",
					"properties": { "city": {"type": "string"} },
					"required": ["city"]
				}
			}
		},
		"evals": [
			{
				"id": "check-length",
				"type": "min_length",
				"trigger": "every_turn",
				"params": { "min": 1 }
			}
		]
	}`

	repo := newTestTurnRepository()
	repo.addTurn("default", 1, mock.Turn{
		Type:    "tool_calls",
		Content: "Checking weather",
		ToolCalls: []mock.ToolCall{{
			Name:      "get_weather",
			Arguments: map[string]interface{}{"city": "Tokyo"},
		}},
	})
	repo.addTurn("default", 2, mock.Turn{
		Type:    "text",
		Content: "Tokyo weather is clear.",
	})

	registry := evals.NewEvalTypeRegistry()
	runner := evals.NewEvalRunner(registry)

	provider := mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)
	packPath := writePackFile(t, toolsAndEvalsPackJSON)

	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
		sdk.WithEventBus(bus),
		sdk.WithTracerProvider(tp),
		sdk.WithEvalRunner(runner),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	conv.OnTool("get_weather", func(args map[string]any) (any, error) {
		return map[string]any{"temp": 25}, nil
	})

	_, err = conv.Send(context.Background(), "Weather in Tokyo?")
	require.NoError(t, err)

	time.Sleep(300 * time.Millisecond)

	spanNames := flushAndGetSpans(t, tp, exp)

	assert.True(t, spanNames["promptkit.pipeline"], "expected pipeline span")
	assert.True(t, spanNames["execute_tool"], "expected tool span")
	assert.True(t, spanNames["promptkit.eval.check-length"], "expected eval span")
}
