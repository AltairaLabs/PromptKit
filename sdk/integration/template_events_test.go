package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/variables"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// ---------------------------------------------------------------------------
// Template event integration tests
// ---------------------------------------------------------------------------

func TestTemplateEventsEmitted(t *testing.T) {
	bus, collector, _ := testMetricsSetup(t)
	ec := newEventCollector(bus)

	conv := openTestConv(t,
		sdk.WithEventBus(bus),
		sdk.WithMetrics(collector, nil),
		sdk.WithVariables(map[string]string{"name": "Alice"}),
	)

	_, err := conv.Send(context.Background(), "Hello")
	require.NoError(t, err)

	// Wait for template events (async delivery)
	found := ec.waitForEvents([]events.EventType{
		events.EventTemplateStarted,
		events.EventTemplateRendered,
	}, 5*time.Second)
	require.True(t, found, "expected template started and rendered events")

	// Verify started event
	started := ec.ofType(events.EventTemplateStarted)
	require.Len(t, started, 1)
	startedData, ok := started[0].Data.(*events.TemplateStartedData)
	require.True(t, ok)
	assert.NotEmpty(t, startedData.RawTemplate)
	assert.Greater(t, startedData.VariableCount, 0)

	// Verify rendered event
	rendered := ec.ofType(events.EventTemplateRendered)
	require.Len(t, rendered, 1)
	renderedData, ok := rendered[0].Data.(*events.TemplateRenderedData)
	require.True(t, ok)
	assert.NotEmpty(t, renderedData.SystemPrompt)
	assert.NotEmpty(t, renderedData.PromptHash)
	assert.GreaterOrEqual(t, renderedData.RenderPasses, 1)

	// Should NOT have template failed
	assert.False(t, ec.hasType(events.EventTemplateFailed))
}

func TestTemplateEventsWithVariableProvider(t *testing.T) {
	bus, collector, _ := testMetricsSetup(t)
	ec := newEventCollector(bus)

	provider := &testTimeProvider{}
	conv := openTestConv(t,
		sdk.WithEventBus(bus),
		sdk.WithMetrics(collector, nil),
		sdk.WithVariableProvider(provider),
	)

	_, err := conv.Send(context.Background(), "Hello")
	require.NoError(t, err)

	found := ec.waitForEvent(events.EventTemplateRendered, 5*time.Second)
	require.True(t, found, "expected template rendered event")

	rendered := ec.ofType(events.EventTemplateRendered)
	require.Len(t, rendered, 1)
	renderedData := rendered[0].Data.(*events.TemplateRenderedData)
	assert.NotEmpty(t, renderedData.SystemPrompt)
}

// testTimeProvider is a variables.Provider that injects a fixed current_time variable.
type testTimeProvider struct{}

func (p *testTimeProvider) Name() string { return "test-time" }
func (p *testTimeProvider) Provide(_ context.Context) (map[string]string, error) {
	return map[string]string{"current_time": "2026-04-01T12:00:00Z"}, nil
}

// Compile-time assertion that testTimeProvider implements variables.Provider.
var _ variables.Provider = (*testTimeProvider)(nil)
