package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/storage/local"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// TestSmoke proves the media-by-reference wiring compiles and validates using
// the mock provider — no API keys and no network access. The mock ignores the
// media itself, so the assertion is simply that Open + Send succeed with a
// stored image sent by durable reference.
func TestSmoke(t *testing.T) {
	ctx := context.Background()

	store, err := local.NewFileStore(local.FileStoreConfig{BaseDir: t.TempDir()})
	require.NoError(t, err)

	ref, err := store.StoreMedia(ctx,
		&types.MediaContent{Data: pngDataURLPayload(), MIMEType: "image/png"},
		&storage.MediaMetadata{SessionID: "example", MIMEType: "image/png"},
	)
	require.NoError(t, err)

	provider := mock.NewProvider("mock", "mock-model", false)
	conv, err := sdk.Open("media-storage-ref.pack.json", "vision-analyst",
		sdk.WithProvider(provider),
		sdk.WithMediaStorage(store),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	defer conv.Close()

	resp, err := conv.Send(ctx, "hi", sdk.WithImageStorageRef(string(ref), "image/png"))
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Text())
}
