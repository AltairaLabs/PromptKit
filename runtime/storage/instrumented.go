// Package storage exposes the MediaStorageService interface for media
// persistence, plus instrumentation that wires storage operations into
// the runtime's event bus and OpenTelemetry spans.
package storage

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// instrumentationTracerName is the OTel tracer name used for media storage spans.
const instrumentationTracerName = "github.com/AltairaLabs/PromptKit/runtime/storage"

// InstrumentedStorage wraps any MediaStorageService with telemetry —
// OTel spans on every call, runtime events on success/failure, and
// (when wired in by the consumer) bytes/latency/error metrics derived
// from the emitted events.
//
// The wrapper is nil-safe at every level: a nil bus or nil inner is a
// hard error, but missing fields on MediaMetadata never panic.
//
// Backend identification (`local`, `s3`, ...) is supplied by the
// caller at construction time so the same wrapper works regardless of
// the concrete inner implementation.
type InstrumentedStorage struct {
	inner   MediaStorageService
	tracer  trace.Tracer
	backend string

	busMu sync.RWMutex
	bus   events.Bus
}

// NewInstrumentedStorage wraps inner so every call publishes a media
// lifecycle event to bus and records an OTel span. backend names the
// concrete implementation (e.g. "local", "s3") and ends up on every
// emitted event and span.
//
// Passing a nil inner returns nil — the wrapper has nothing to wrap.
// A nil bus is allowed (events become no-ops); spans still emit via
// the global tracer provider.
func NewInstrumentedStorage(inner MediaStorageService, bus events.Bus, backend string) *InstrumentedStorage {
	if inner == nil {
		return nil
	}
	return &InstrumentedStorage{
		inner:   inner,
		bus:     bus,
		tracer:  otel.Tracer(instrumentationTracerName),
		backend: backend,
	}
}

// SetBus swaps the event bus the wrapper publishes to. Arena
// constructs the storage before the runtime event bus exists, then
// late-binds it via this method once the bus is wired up. Safe to
// call once before the wrapper sees any traffic; safe to call
// concurrently with operations because the bus pointer is guarded by
// busMu.
func (s *InstrumentedStorage) SetBus(bus events.Bus) {
	if s == nil {
		return
	}
	s.busMu.Lock()
	defer s.busMu.Unlock()
	s.bus = bus
}

func (s *InstrumentedStorage) currentBus() events.Bus {
	s.busMu.RLock()
	defer s.busMu.RUnlock()
	return s.bus
}

// Inner returns the wrapped MediaStorageService — useful for callers
// that need to type-assert to a backend-specific interface.
func (s *InstrumentedStorage) Inner() MediaStorageService {
	if s == nil {
		return nil
	}
	return s.inner
}

// StoreMedia traces, times, and announces a store operation.
func (s *InstrumentedStorage) StoreMedia(
	ctx context.Context, content *types.MediaContent, metadata *MediaMetadata,
) (Reference, error) {
	ctx, span := s.startSpan(ctx, "media.store", metadata)
	defer span.End()

	start := time.Now()
	ref, err := s.inner.StoreMedia(ctx, content, metadata)
	dur := time.Since(start)

	mime := mimeFromContent(content)
	size := sizeFromMetadata(metadata)
	span.SetAttributes(
		attribute.String("media.mime_type", mime),
		attribute.Int64("media.size_bytes", size),
	)

	if err != nil {
		s.recordFailure(span, "store", string(ref), mime, dur, metadata, err)
		return ref, err
	}

	span.SetAttributes(attribute.String("media.reference", string(ref)))
	s.publish(events.EventMediaStored, &events.MediaStorageEventData{
		Operation:  "store",
		Backend:    s.backend,
		Reference:  string(ref),
		MIMEType:   mime,
		SizeBytes:  size,
		Duration:   dur,
		RunID:      metadataRunID(metadata),
		MessageIdx: metadataMessageIdx(metadata),
		PartIdx:    metadataPartIdx(metadata),
	}, metadata)
	return ref, nil
}

// RetrieveMedia traces, times, and announces a retrieve operation.
func (s *InstrumentedStorage) RetrieveMedia(
	ctx context.Context, reference Reference,
) (*types.MediaContent, error) {
	ctx, span := s.startSpan(ctx, "media.retrieve", nil)
	defer span.End()
	span.SetAttributes(attribute.String("media.reference", string(reference)))

	start := time.Now()
	content, err := s.inner.RetrieveMedia(ctx, reference)
	dur := time.Since(start)

	mime := mimeFromContent(content)
	if err != nil {
		s.recordFailure(span, "retrieve", string(reference), mime, dur, nil, err)
		return content, err
	}

	span.SetAttributes(attribute.String("media.mime_type", mime))
	s.publish(events.EventMediaRetrieved, &events.MediaStorageEventData{
		Operation: "retrieve",
		Backend:   s.backend,
		Reference: string(reference),
		MIMEType:  mime,
		Duration:  dur,
	}, nil)
	return content, nil
}

// DeleteMedia traces, times, and announces a delete operation.
func (s *InstrumentedStorage) DeleteMedia(ctx context.Context, reference Reference) error {
	ctx, span := s.startSpan(ctx, "media.delete", nil)
	defer span.End()
	span.SetAttributes(attribute.String("media.reference", string(reference)))

	start := time.Now()
	err := s.inner.DeleteMedia(ctx, reference)
	dur := time.Since(start)

	if err != nil {
		s.recordFailure(span, "delete", string(reference), "", dur, nil, err)
		return err
	}

	s.publish(events.EventMediaDeleted, &events.MediaStorageEventData{
		Operation: "delete",
		Backend:   s.backend,
		Reference: string(reference),
		Duration:  dur,
		Reason:    "explicit",
	}, nil)
	return nil
}

// GetURL traces and times URL generation. No success event is emitted
// — URL generation is a metadata operation that doesn't move bytes —
// but failures still publish an error event so opaque 404s are visible.
func (s *InstrumentedStorage) GetURL(
	ctx context.Context, reference Reference, expiry time.Duration,
) (string, error) {
	ctx, span := s.startSpan(ctx, "media.get_url", nil)
	defer span.End()
	span.SetAttributes(attribute.String("media.reference", string(reference)))

	start := time.Now()
	url, err := s.inner.GetURL(ctx, reference, expiry)
	dur := time.Since(start)

	if err != nil {
		s.recordFailure(span, "get_url", string(reference), "", dur, nil, err)
		return url, err
	}
	return url, nil
}

func (s *InstrumentedStorage) startSpan(
	ctx context.Context, name string, metadata *MediaMetadata,
) (context.Context, trace.Span) {
	ctx, span := s.tracer.Start(ctx, name)
	span.SetAttributes(attribute.String("media.backend", s.backend))
	if runID := metadataRunID(metadata); runID != "" {
		span.SetAttributes(attribute.String("media.run_id", runID))
	}
	return ctx, span
}

func (s *InstrumentedStorage) recordFailure(
	span trace.Span, op, ref, mime string, dur time.Duration,
	metadata *MediaMetadata, err error,
) {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	s.publish(events.EventMediaStoreError, &events.MediaStorageEventData{
		Operation:  op,
		Backend:    s.backend,
		Reference:  ref,
		MIMEType:   mime,
		Duration:   dur,
		Error:      err,
		RunID:      metadataRunID(metadata),
		MessageIdx: metadataMessageIdx(metadata),
		PartIdx:    metadataPartIdx(metadata),
	}, metadata)
}

func (s *InstrumentedStorage) publish(
	eventType events.EventType, data *events.MediaStorageEventData, metadata *MediaMetadata,
) {
	bus := s.currentBus()
	if bus == nil {
		return
	}
	bus.Publish(&events.Event{
		Type:           eventType,
		Timestamp:      time.Now(),
		ConversationID: metadataConversationID(metadata),
		SessionID:      metadataSessionID(metadata),
		Data:           data,
	})
}

func mimeFromContent(content *types.MediaContent) string {
	if content == nil {
		return ""
	}
	return content.MIMEType
}

func sizeFromMetadata(metadata *MediaMetadata) int64 {
	if metadata == nil {
		return 0
	}
	return metadata.SizeBytes
}

func metadataRunID(metadata *MediaMetadata) string {
	if metadata == nil {
		return ""
	}
	return metadata.RunID
}

func metadataConversationID(metadata *MediaMetadata) string {
	if metadata == nil {
		return ""
	}
	return metadata.ConversationID
}

func metadataSessionID(metadata *MediaMetadata) string {
	if metadata == nil {
		return ""
	}
	return metadata.SessionID
}

func metadataMessageIdx(metadata *MediaMetadata) int {
	if metadata == nil {
		return 0
	}
	return metadata.MessageIdx
}

func metadataPartIdx(metadata *MediaMetadata) int {
	if metadata == nil {
		return 0
	}
	return metadata.PartIdx
}
