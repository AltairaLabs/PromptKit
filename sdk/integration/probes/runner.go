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
}

// numDefaultOptions tracks the count of options Run installs internally.
const numDefaultOptions = 5

// Run wires probes around a fresh sdk.Conversation and returns both. Counters
// are reset before Run returns, so the caller observes only post-Open traffic.
//
// Cleanup (closing the conv and the bus) is registered with t.Cleanup.
//
// RunOptions is intentionally passed by value: tests benefit from the inline
// struct-literal call site (Run(t, RunOptions{SeedHistory: 5})), and the
// per-call cost of an 80-byte copy is negligible against the work Run does.
//
//nolint:gocritic // hugeParam suppression — see godoc above.
func Run(t *testing.T, ro RunOptions) (*Probes, *sdk.Conversation) {
	t.Helper()

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
		seedStore(t, inner, convID, ro.SeedHistory)
	}

	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Close() })

	p := &Probes{
		store:  newProbedStore(inner),
		bus:    bus,
		events: make(map[events.EventType]int),
	}
	bus.SubscribeAll(func(e *events.Event) {
		p.mu.Lock()
		p.events[e.Type]++
		p.mu.Unlock()
	})

	provider := mock.NewProvider("probes-mock", "probes-model", false)

	allOpts := make([]sdk.Option, 0, numDefaultOptions+len(ro.SDKOptions))
	allOpts = append(allOpts,
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
		sdk.WithStateStore(p.store),
		sdk.WithConversationID(convID),
		sdk.WithEventBus(bus),
	)
	allOpts = append(allOpts, ro.SDKOptions...)

	packPath := writePackFile(t, packJSON)
	conv, err := sdk.Open(packPath, promptName, allOpts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	p.ResetCounters()
	return p, conv
}

func seedStore(t *testing.T, store statestore.Store, convID string, n int) {
	t.Helper()
	msgs := make([]types.Message, n)
	for i := range n {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs[i] = types.Message{Role: role, Content: "prior"}
	}
	require.NoError(t, store.Save(context.Background(), &statestore.ConversationState{
		ID:       convID,
		Messages: msgs,
	}))
}

func writePackFile(t *testing.T, pack string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "probes.pack.json")
	require.NoError(t, os.WriteFile(path, []byte(pack), packFilePerms))
	return path
}
