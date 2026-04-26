package probes

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// minimalPackJSON is a tiny valid pack used by Run when the caller does not
// supply one. Mirrors the pack used in sdk/integration/helpers_test.go but
// kept here so probes can be used from any test package without coupling.
const minimalPackJSON = `{
	"id": "probes-default-pack",
	"version": "1.0.0",
	"description": "Default pack for probe-based contract tests",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a helpful assistant."
		}
	}
}`

const (
	defaultPromptName = "chat"
	defaultConvID     = "probes-conv"
	packFilePerms     = 0o600
)

// RunOptions configures a probed conversation.
//
// Zero values give a sensible default: a fresh in-memory store, the minimal
// pack, the "chat" prompt, a deterministic conversation ID, and a mock
// provider. Tests override only what they need.
type RunOptions struct {
	// PackJSON is the pack definition. If empty, a minimal default is used.
	PackJSON string
	// PromptName selects the prompt within the pack. Defaults to "chat".
	PromptName string
	// ConversationID is passed to sdk.WithConversationID. Defaults to
	// "probes-conv".
	ConversationID string
	// SeedHistory pre-populates the conversation with N prior messages
	// (alternating user/assistant). Used to exercise stages whose work
	// scales with conversation length.
	SeedHistory int
	// SDKOptions are appended after the probe-default options. Caller-supplied
	// options can override defaults (for example, providing a non-mock
	// provider or a different state store), but doing so will defeat the
	// corresponding probe.
	SDKOptions []sdk.Option
	// AutoSummarize, when non-nil, wires sdk.WithAutoSummarize using the
	// probes' counted summarizer provider, so summarizer.Predict shows up
	// in Snapshot.Count.
	AutoSummarize *AutoSummarizeOpts
}

// AutoSummarizeOpts configures probed auto-summarization.
type AutoSummarizeOpts struct {
	Threshold int
	BatchSize int
}

// numDefaultOptions tracks the count of options Run installs internally.
const numDefaultOptions = 5

// Run wires probes around a fresh sdk.Conversation and returns both. Counters
// are reset before Run returns, so the caller observes only post-Open traffic.
//
// Cleanup (closing the conv and the bus) is registered with tb.Cleanup.
//
// Accepts testing.TB so both *testing.T (unit / integration tests) and
// *testing.B (benchmarks) can use the helper.
//
// RunOptions is intentionally passed by value: tests benefit from the inline
// struct-literal call site (Run(t, RunOptions{SeedHistory: 5})), and the
// per-call cost of an 80-byte copy is negligible against the work Run does.
//
//nolint:gocritic // hugeParam suppression — see godoc above.
func Run(tb testing.TB, ro RunOptions) (*Probes, *sdk.Conversation) {
	tb.Helper()

	packJSON := ro.PackJSON
	if packJSON == "" {
		packJSON = minimalPackJSON
	}
	promptName := ro.PromptName
	if promptName == "" {
		promptName = defaultPromptName
	}
	convID := ro.ConversationID
	if convID == "" {
		convID = defaultConvID
	}

	inner := statestore.NewMemoryStore()

	if ro.SeedHistory > 0 {
		seedStore(tb, inner, convID, ro.SeedHistory)
	}

	bus := events.NewEventBus()
	tb.Cleanup(func() { bus.Close() })

	summarizerInner := mock.NewProvider("probes-summarizer", "probes-summarizer-model", false)

	p := &Probes{
		store:              newProbedStore(inner),
		bus:                bus,
		summarizerProvider: newProbedProvider(summarizerInner),
		events:             make(map[events.EventType]int),
		eventRecords:       make(map[events.EventType][]*events.Event),
	}
	bus.SubscribeAll(func(e *events.Event) {
		p.mu.Lock()
		p.events[e.Type]++
		p.eventRecords[e.Type] = append(p.eventRecords[e.Type], e)
		p.mu.Unlock()
	})

	provider := mock.NewProvider("probes-mock", "probes-model", false)

	extraSlots := len(ro.SDKOptions)
	if ro.AutoSummarize != nil {
		extraSlots++
	}
	allOpts := make([]sdk.Option, 0, numDefaultOptions+extraSlots)
	allOpts = append(allOpts,
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
		sdk.WithStateStore(p.store),
		sdk.WithConversationID(convID),
		sdk.WithEventBus(bus),
	)
	if ro.AutoSummarize != nil {
		allOpts = append(allOpts, sdk.WithAutoSummarize(
			p.summarizerProvider, ro.AutoSummarize.Threshold, ro.AutoSummarize.BatchSize,
		))
	}
	allOpts = append(allOpts, ro.SDKOptions...)

	packPath := writePackFile(tb, packJSON)
	conv, err := sdk.Open(packPath, promptName, allOpts...)
	require.NoError(tb, err)
	tb.Cleanup(func() { _ = conv.Close() })

	p.ResetCounters()
	return p, conv
}

func seedStore(tb testing.TB, store statestore.Store, convID string, n int) {
	tb.Helper()
	msgs := make([]types.Message, n)
	for i := range n {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs[i] = types.Message{Role: role, Content: "prior"}
	}
	require.NoError(tb, store.Save(context.Background(), &statestore.ConversationState{
		ID:       convID,
		Messages: msgs,
	}))
}

func writePackFile(tb testing.TB, pack string) string {
	tb.Helper()
	dir := tb.TempDir()
	path := filepath.Join(dir, "probes.pack.json")
	require.NoError(tb, os.WriteFile(path, []byte(pack), packFilePerms))
	return path
}
