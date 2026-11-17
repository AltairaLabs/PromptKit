package sdk

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/memory"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// deleteOnlyCallerStore takes the caller on delete but not on save.
type deleteOnlyCallerStore struct{ *memory.InMemoryStore }

func (s deleteOnlyCallerStore) DeleteWithOptions(
	ctx context.Context, scope map[string]string, id string, _ memory.DeleteOptions,
) error {
	return s.Delete(ctx, scope, id)
}

// fullCallerStore takes the caller on save and delete.
type fullCallerStore struct{ deleteOnlyCallerStore }

func (s fullCallerStore) SaveWithOptions(ctx context.Context, m *memory.Memory, _ memory.SaveOptions) error {
	return s.Save(ctx, m)
}

func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	logger.SetLogger(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { logger.SetLogger(nil) })
	return &buf
}

const callerGapWarning = "memory store takes the caller on some operations but not all"

// A store that is caller-aware on one write but not the other is warned
// about when its tools are registered, naming what it lacks.
func TestMemoryCapability_WarnsAboutAPartlyCallerAwareStore(t *testing.T) {
	buf := captureWarnings(t)
	store := deleteOnlyCallerStore{memory.NewInMemoryStore()}

	NewMemoryCapability(store, map[string]string{"user_id": "u1"}).RegisterTools(tools.NewRegistry())

	assert.Contains(t, buf.String(), callerGapWarning)
	assert.Contains(t, buf.String(), `"missing":["memory.OptionsSaver"]`)
}

// A caller-aware retriever over a store that cannot take the caller on
// writes is warned about as well.
func TestMemoryCapability_WarnsAboutACallerAwareRetrieverOverAPlainStore(t *testing.T) {
	buf := captureWarnings(t)
	capability := NewMemoryCapability(memory.NewInMemoryStore(), map[string]string{"user_id": "u1"}).
		WithRetriever(&callerRetriever{})

	capability.RegisterTools(tools.NewRegistry())

	assert.Contains(t, buf.String(), callerGapWarning)
	assert.Contains(t, buf.String(), `"missing":["memory.OptionsSaver","memory.ExtrasDeleter"]`)
}

func TestMemoryCapability_NoCallerWarningForConsistentStores(t *testing.T) {
	for name, store := range map[string]memory.Store{
		"plain":        memory.NewInMemoryStore(),
		"caller-aware": fullCallerStore{deleteOnlyCallerStore{memory.NewInMemoryStore()}},
	} {
		t.Run(name, func(t *testing.T) {
			buf := captureWarnings(t)
			NewMemoryCapability(store, map[string]string{"user_id": "u1"}).RegisterTools(tools.NewRegistry())
			assert.NotContains(t, buf.String(), callerGapWarning)
		})
	}
}
