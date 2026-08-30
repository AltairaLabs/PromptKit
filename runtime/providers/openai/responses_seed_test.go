package openai

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func intPtr(i int) *int { return &i }

func seedRequest() providers.PredictionRequest {
	return providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
		Seed:     intPtr(42),
	}
}

// TestResponsesRequest_SeedAbsentRegardlessOfUnsupportedParams pins that the
// fix is unconditional, by exercising BOTH sides of the guard the issue
// proposed.
//
// The issue suggested guarding seed behind unsupportedParams, matching
// temperature and top_p above it. That would only make the 400 SUPPRESSIBLE:
// seed is not a per-model capability on this endpoint, it is absent from it, so
// a guard leaves every caller to hit the failure and configure their way out.
//
// A guard-based implementation passes the nil case only by accident and fails
// it outright — which is what the first subtest catches.
func TestResponsesRequest_SeedAbsentRegardlessOfUnsupportedParams(t *testing.T) {
	cases := []struct {
		name              string
		unsupportedParams []string
	}{
		{"nothing declared unsupported", nil},
		{"other params declared unsupported", []string{"temperature", "top_p"}},
		{"seed itself declared unsupported", []string{"seed"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &Provider{unsupportedParams: tc.unsupportedParams}

			got := p.buildResponsesRequest(seedRequest(), nil, "")

			_, present := got["seed"]
			assert.False(t, present,
				"seed must be absent whatever unsupported_params says")
		})
	}
}

// TestCompletionsRequest_StillSendsSeed guards the other direction. Chat
// Completions DOES support seed, and #1742 added it for reproducibility —
// removing it there would undo that.
func TestCompletionsRequest_StillSendsSeed(t *testing.T) {
	p := &Provider{apiMode: APIModeCompletions}

	openAIReq := map[string]interface{}{}
	req := seedRequest()
	p.enrichRequest(openAIReq, &req, "")

	require.Contains(t, openAIReq, "seed",
		"Chat Completions supports seed; reproducibility must survive")
	assert.Equal(t, 42, openAIReq["seed"])
}

// seedWarnHandler captures slog records so the drop warning can be asserted.
type seedWarnHandler struct {
	mu       sync.Mutex
	messages []string
}

func (h *seedWarnHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *seedWarnHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.messages = append(h.messages, r.Message)
	return nil
}
func (h *seedWarnHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *seedWarnHandler) WithGroup(string) slog.Handler      { return h }

func (h *seedWarnHandler) all() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, len(h.messages))
	copy(out, h.messages)
	return out
}

const seedDropWarning = "openai: seed dropped — the Responses API has no seed parameter"

// TestResponsesRequest_WarnsWhenSeedIsDropped covers the other half of the fix.
//
// Dropping the parameter silently would leave a caller who set a seed for
// reproducibility with non-reproducible output and no signal — and they can
// reach this path without choosing it, since requiresResponsesAPI routes
// gpt-5-pro / o1-pro here regardless of configured api_mode.
func TestResponsesRequest_WarnsWhenSeedIsDropped(t *testing.T) {
	h := &seedWarnHandler{}
	logger.SetLogger(slog.New(h))
	t.Cleanup(func() { logger.SetLogger(nil) })

	p := &Provider{}
	p.buildResponsesRequest(seedRequest(), nil, "")

	assert.Contains(t, h.all(), seedDropWarning,
		"a dropped seed must be visible, not silent")
}

// TestResponsesRequest_SilentWhenNoSeedRequested guards against warning on the
// common path — most callers never set a seed.
func TestResponsesRequest_SilentWhenNoSeedRequested(t *testing.T) {
	h := &seedWarnHandler{}
	logger.SetLogger(slog.New(h))
	t.Cleanup(func() { logger.SetLogger(nil) })

	p := &Provider{}
	req := seedRequest()
	req.Seed = nil
	p.buildResponsesRequest(req, nil, "")

	assert.NotContains(t, h.all(), seedDropWarning)
}
