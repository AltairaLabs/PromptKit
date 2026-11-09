package storage_test

import (
	"context"
	"encoding/base64"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/storage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/storage/local"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// TestInstrumentedStorage_RealFileStoreAndBus runs the wrapper over a real
// local FileStore and a real EventBus: what a subscriber sees must describe
// the operation the backend actually performed (the reference it returned,
// the run/message/part the media belongs to), and the media must survive the
// round trip through the wrapper unchanged.
func TestInstrumentedStorage_RealFileStoreAndBus(t *testing.T) {
	fs, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir: t.TempDir(), Organization: storage.OrganizationByRun,
	})
	require.NoError(t, err)

	bus := events.NewEventBus()
	var mu sync.Mutex
	var got []*events.Event
	bus.SubscribeAll(func(e *events.Event) {
		mu.Lock()
		got = append(got, e)
		mu.Unlock()
	})

	s := storage.NewInstrumentedStorage(fs, bus, "local")
	ctx := context.Background()

	raw := []byte("\x89PNG fake image bytes")
	data := base64.StdEncoding.EncodeToString(raw)
	content := &types.MediaContent{MIMEType: types.MIMETypeImagePNG, Data: &data}
	meta := &storage.MediaMetadata{
		RunID: "run-1", ConversationID: "conv-1", SessionID: "sess-1",
		MessageIdx: 3, PartIdx: 1, MIMEType: types.MIMETypeImagePNG, SizeBytes: int64(len(raw)),
	}

	ref, err := s.StoreMedia(ctx, content, meta)
	require.NoError(t, err)
	require.NotEmpty(t, ref)

	back, err := s.RetrieveMedia(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, types.MIMETypeImagePNG, back.MIMEType)
	backData, err := back.GetBase64Data()
	require.NoError(t, err)
	assert.Equal(t, data, backData)

	require.NoError(t, s.DeleteMedia(ctx, ref))
	_, err = s.RetrieveMedia(ctx, ref)
	require.Error(t, err, "deleted media must be gone from the backend")

	bus.Close()
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, got, 4)

	stored := got[0]
	assert.Equal(t, events.EventMediaStored, stored.Type)
	assert.Equal(t, "conv-1", stored.ConversationID)
	assert.Equal(t, "sess-1", stored.SessionID)
	sd := stored.Data.(*events.MediaStorageEventData)
	assert.Equal(t, "store", sd.Operation)
	assert.Equal(t, "local", sd.Backend)
	assert.Equal(t, string(ref), sd.Reference)
	assert.Equal(t, types.MIMETypeImagePNG, sd.MIMEType)
	assert.Equal(t, int64(len(raw)), sd.SizeBytes)
	assert.Equal(t, "run-1", sd.RunID)
	assert.Equal(t, 3, sd.MessageIdx)
	assert.Equal(t, 1, sd.PartIdx)

	assert.Equal(t, events.EventMediaRetrieved, got[1].Type)
	rd := got[1].Data.(*events.MediaStorageEventData)
	assert.Equal(t, string(ref), rd.Reference)
	assert.Equal(t, types.MIMETypeImagePNG, rd.MIMEType)

	assert.Equal(t, events.EventMediaDeleted, got[2].Type)
	dd := got[2].Data.(*events.MediaStorageEventData)
	assert.Equal(t, string(ref), dd.Reference)
	assert.Equal(t, "explicit", dd.Reason)

	assert.Equal(t, events.EventMediaStoreError, got[3].Type)
	ed := got[3].Data.(*events.MediaStorageEventData)
	assert.Equal(t, "retrieve", ed.Operation)
	assert.Equal(t, string(ref), ed.Reference)
	require.Error(t, ed.Error)
	var direct error
	_, direct = fs.RetrieveMedia(ctx, ref)
	assert.Equal(t, direct.Error(), ed.Error.Error(), "the event carries the backend's error")
}
