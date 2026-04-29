package storage_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingBus drains every Publish into a slice. Lets tests assert on
// the exact set of events the InstrumentedStorage emitted, without
// needing the full async EventBus machinery.
type recordingBus struct {
	mu     sync.Mutex
	events []*events.Event
}

func (b *recordingBus) Publish(e *events.Event) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, e)
	return true
}

func (b *recordingBus) Subscribe(_ events.EventType, _ events.Listener) func() {
	return func() {}
}

func (b *recordingBus) SubscribeAll(_ events.Listener) func() {
	return func() {}
}

func (b *recordingBus) Close() {}

func (b *recordingBus) snapshot() []*events.Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]*events.Event, len(b.events))
	copy(out, b.events)
	return out
}

// fakeStorage is a programmable MediaStorageService for the wrapper
// tests — no disk, no network, just controllable success/failure.
type fakeStorage struct {
	storeRef     string
	storeErr     error
	retrieveResp *types.MediaContent
	retrieveErr  error
	deleteErr    error
	urlResp      string
	urlErr       error
	calls        []string
}

func (f *fakeStorage) StoreMedia(_ context.Context, _ *types.MediaContent, _ *storage.MediaMetadata) (storage.Reference, error) {
	f.calls = append(f.calls, "store")
	return storage.Reference(f.storeRef), f.storeErr
}

func (f *fakeStorage) RetrieveMedia(_ context.Context, _ storage.Reference) (*types.MediaContent, error) {
	f.calls = append(f.calls, "retrieve")
	return f.retrieveResp, f.retrieveErr
}

func (f *fakeStorage) DeleteMedia(_ context.Context, _ storage.Reference) error {
	f.calls = append(f.calls, "delete")
	return f.deleteErr
}

func (f *fakeStorage) GetURL(_ context.Context, _ storage.Reference, _ time.Duration) (string, error) {
	f.calls = append(f.calls, "get_url")
	return f.urlResp, f.urlErr
}

func TestNewInstrumentedStorage_NilInner(t *testing.T) {
	got := storage.NewInstrumentedStorage(nil, nil, "local")
	assert.Nil(t, got, "nil inner should yield nil wrapper — caller can detect misuse early")
}

func TestInstrumentedStorage_StoreEmitsEvent(t *testing.T) {
	inner := &fakeStorage{storeRef: "out/media/run-1/abc.png"}
	bus := &recordingBus{}
	s := storage.NewInstrumentedStorage(inner, bus, "local")

	ref, err := s.StoreMedia(context.Background(), &types.MediaContent{MIMEType: "image/png"},
		&storage.MediaMetadata{
			RunID:          "run-1",
			ConversationID: "conv-1",
			SessionID:      "sess-1",
			MIMEType:       "image/png",
			SizeBytes:      1024,
			MessageIdx:     2,
			PartIdx:        0,
		})
	require.NoError(t, err)
	assert.Equal(t, storage.Reference("out/media/run-1/abc.png"), ref)
	assert.Equal(t, []string{"store"}, inner.calls)

	evs := bus.snapshot()
	require.Len(t, evs, 1)
	assert.Equal(t, events.EventMediaStored, evs[0].Type)
	assert.Equal(t, "conv-1", evs[0].ConversationID)
	assert.Equal(t, "sess-1", evs[0].SessionID)

	data, ok := evs[0].Data.(*events.MediaStorageEventData)
	require.True(t, ok, "event data should be *MediaStorageEventData")
	assert.Equal(t, "store", data.Operation)
	assert.Equal(t, "local", data.Backend)
	assert.Equal(t, "out/media/run-1/abc.png", data.Reference)
	assert.Equal(t, "image/png", data.MIMEType)
	assert.Equal(t, int64(1024), data.SizeBytes)
	assert.Equal(t, "run-1", data.RunID)
	assert.Equal(t, 2, data.MessageIdx)
	assert.Greater(t, data.Duration, time.Duration(0))
}

func TestInstrumentedStorage_StoreErrorEmitsErrorEvent(t *testing.T) {
	storeErr := errors.New("disk full")
	inner := &fakeStorage{storeErr: storeErr}
	bus := &recordingBus{}
	s := storage.NewInstrumentedStorage(inner, bus, "local")

	_, err := s.StoreMedia(context.Background(), &types.MediaContent{MIMEType: "image/png"},
		&storage.MediaMetadata{RunID: "run-1", MIMEType: "image/png"})
	assert.ErrorIs(t, err, storeErr)

	evs := bus.snapshot()
	require.Len(t, evs, 1)
	assert.Equal(t, events.EventMediaStoreError, evs[0].Type)
	data := evs[0].Data.(*events.MediaStorageEventData)
	assert.Equal(t, "store", data.Operation)
	assert.ErrorIs(t, data.Error, storeErr)
}

func TestInstrumentedStorage_RetrieveEmitsEvent(t *testing.T) {
	inner := &fakeStorage{retrieveResp: &types.MediaContent{MIMEType: "audio/wav"}}
	bus := &recordingBus{}
	s := storage.NewInstrumentedStorage(inner, bus, "local")

	got, err := s.RetrieveMedia(context.Background(), "out/media/abc.wav")
	require.NoError(t, err)
	require.NotNil(t, got)

	evs := bus.snapshot()
	require.Len(t, evs, 1)
	assert.Equal(t, events.EventMediaRetrieved, evs[0].Type)
	data := evs[0].Data.(*events.MediaStorageEventData)
	assert.Equal(t, "retrieve", data.Operation)
	assert.Equal(t, "audio/wav", data.MIMEType)
	assert.Equal(t, "out/media/abc.wav", data.Reference)
}

func TestInstrumentedStorage_RetrieveErrorEmitsErrorEvent(t *testing.T) {
	retrieveErr := errors.New("not found")
	inner := &fakeStorage{retrieveErr: retrieveErr}
	bus := &recordingBus{}
	s := storage.NewInstrumentedStorage(inner, bus, "local")

	_, err := s.RetrieveMedia(context.Background(), "missing")
	assert.ErrorIs(t, err, retrieveErr)

	evs := bus.snapshot()
	require.Len(t, evs, 1)
	assert.Equal(t, events.EventMediaStoreError, evs[0].Type)
	assert.Equal(t, "retrieve", evs[0].Data.(*events.MediaStorageEventData).Operation)
}

func TestInstrumentedStorage_DeleteEmitsExplicitEvent(t *testing.T) {
	inner := &fakeStorage{}
	bus := &recordingBus{}
	s := storage.NewInstrumentedStorage(inner, bus, "local")

	require.NoError(t, s.DeleteMedia(context.Background(), "out/media/abc.png"))

	evs := bus.snapshot()
	require.Len(t, evs, 1)
	assert.Equal(t, events.EventMediaDeleted, evs[0].Type)
	data := evs[0].Data.(*events.MediaStorageEventData)
	assert.Equal(t, "delete", data.Operation)
	assert.Equal(t, "explicit", data.Reason)
}

func TestInstrumentedStorage_GetURLEmitsErrorOnFailure(t *testing.T) {
	urlErr := errors.New("not accessible")
	inner := &fakeStorage{urlErr: urlErr}
	bus := &recordingBus{}
	s := storage.NewInstrumentedStorage(inner, bus, "local")

	// Success: no event published — URL generation doesn't move bytes.
	innerOK := &fakeStorage{urlResp: "file:///tmp/abc.png"}
	busOK := &recordingBus{}
	sOK := storage.NewInstrumentedStorage(innerOK, busOK, "local")
	url, err := sOK.GetURL(context.Background(), "out/media/abc.png", 0)
	require.NoError(t, err)
	assert.Equal(t, "file:///tmp/abc.png", url)
	assert.Empty(t, busOK.snapshot(), "successful GetURL should not emit an event")

	// Failure: error event published.
	_, err = s.GetURL(context.Background(), "missing", 0)
	assert.ErrorIs(t, err, urlErr)
	evs := bus.snapshot()
	require.Len(t, evs, 1)
	assert.Equal(t, events.EventMediaStoreError, evs[0].Type)
	assert.Equal(t, "get_url", evs[0].Data.(*events.MediaStorageEventData).Operation)
}

func TestInstrumentedStorage_NilBusIsNoOp(t *testing.T) {
	inner := &fakeStorage{storeRef: "out/media/abc.png"}
	s := storage.NewInstrumentedStorage(inner, nil, "local")

	// Must not panic when bus is nil — spans still emit via the global
	// tracer provider, but events become no-ops.
	_, err := s.StoreMedia(context.Background(), &types.MediaContent{MIMEType: "image/png"},
		&storage.MediaMetadata{RunID: "run-1"})
	assert.NoError(t, err)
}

func TestInstrumentedStorage_NilMetadataDoesNotPanic(t *testing.T) {
	inner := &fakeStorage{storeRef: "ref"}
	bus := &recordingBus{}
	s := storage.NewInstrumentedStorage(inner, bus, "local")

	// Real callers should pass metadata, but the wrapper must tolerate
	// a nil — events still fire, with empty IDs.
	_, err := s.StoreMedia(context.Background(), &types.MediaContent{MIMEType: "image/png"}, nil)
	assert.NoError(t, err)

	evs := bus.snapshot()
	require.Len(t, evs, 1)
	data := evs[0].Data.(*events.MediaStorageEventData)
	assert.Empty(t, data.RunID)
	assert.Empty(t, evs[0].ConversationID)
}

func TestInstrumentedStorage_InnerExposesUnderlyingService(t *testing.T) {
	inner := &fakeStorage{}
	s := storage.NewInstrumentedStorage(inner, nil, "local")
	assert.Same(t, inner, s.Inner())
}

func TestInstrumentedStorage_NilSafeInner(t *testing.T) {
	var s *storage.InstrumentedStorage
	assert.Nil(t, s.Inner())
}
