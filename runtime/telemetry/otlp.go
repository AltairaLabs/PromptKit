package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OTLPExporter exports spans to an OTLP endpoint over HTTP.
type OTLPExporter struct {
	endpoint   string
	headers    map[string]string
	client     *http.Client
	resource   *Resource
	batchSize  int
	pending    []*Span
	httpClient HTTPClient
}

// HTTPClient interface for testing.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// OTLPExporterOption configures an OTLPExporter.
type OTLPExporterOption func(*OTLPExporter)

// WithHeaders sets custom headers for OTLP requests.
func WithHeaders(headers map[string]string) OTLPExporterOption {
	return func(e *OTLPExporter) {
		e.headers = headers
	}
}

// WithResource sets the resource for exported spans.
func WithResource(resource *Resource) OTLPExporterOption {
	return func(e *OTLPExporter) {
		e.resource = resource
	}
}

// WithBatchSize sets the batch size for exports.
func WithBatchSize(size int) OTLPExporterOption {
	return func(e *OTLPExporter) {
		e.batchSize = size
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client HTTPClient) OTLPExporterOption {
	return func(e *OTLPExporter) {
		e.httpClient = client
	}
}

// OTLP exporter defaults.
const (
	defaultBatchSize          = 100
	defaultTimeout            = 30 * time.Second
	httpStatusMultipleChoices = 300 // First non-success status code
)

// NewOTLPExporter creates a new OTLP exporter.
func NewOTLPExporter(endpoint string, opts ...OTLPExporterOption) *OTLPExporter {
	e := &OTLPExporter{
		endpoint:  endpoint,
		headers:   make(map[string]string),
		batchSize: defaultBatchSize,
		resource:  DefaultResource(),
		client: &http.Client{
			Timeout: defaultTimeout,
		},
	}

	for _, opt := range opts {
		opt(e)
	}

	if e.httpClient == nil {
		e.httpClient = e.client
	}

	return e
}

// Export sends spans to the OTLP endpoint.
func (e *OTLPExporter) Export(ctx context.Context, spans []*Span) error {
	if len(spans) == 0 {
		return nil
	}

	// Convert to OTLP format
	payload := e.toOTLPPayload(spans)

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal OTLP payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range e.headers {
		req.Header.Set(k, v)
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= httpStatusMultipleChoices {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("OTLP export failed (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// Shutdown flushes pending spans and closes the exporter.
func (e *OTLPExporter) Shutdown(ctx context.Context) error {
	if len(e.pending) > 0 {
		if err := e.Export(ctx, e.pending); err != nil {
			return err
		}
		e.pending = nil
	}
	return nil
}

// otlpPayload is the OTLP JSON format for traces.
type otlpPayload struct {
	ResourceSpans []otlpResourceSpans `json:"resourceSpans"`
}

type otlpResourceSpans struct {
	Resource   otlpResource    `json:"resource"`
	ScopeSpans []otlpScopeSpan `json:"scopeSpans"`
}

type otlpResource struct {
	Attributes []otlpAttribute `json:"attributes"`
}

type otlpScopeSpan struct {
	Scope otlpScope  `json:"scope"`
	Spans []otlpSpan `json:"spans"`
}

type otlpScope struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type otlpSpan struct {
	TraceID           string          `json:"traceId"`
	SpanID            string          `json:"spanId"`
	ParentSpanID      string          `json:"parentSpanId,omitempty"`
	Name              string          `json:"name"`
	Kind              int             `json:"kind"`
	StartTimeUnixNano int64           `json:"startTimeUnixNano"`
	EndTimeUnixNano   int64           `json:"endTimeUnixNano"`
	Attributes        []otlpAttribute `json:"attributes,omitempty"`
	Status            *otlpStatus     `json:"status,omitempty"`
	Events            []otlpEvent     `json:"events,omitempty"`
}

type otlpAttribute struct {
	Key   string        `json:"key"`
	Value otlpAttrValue `json:"value"`
}

type otlpAttrValue struct {
	StringValue *string  `json:"stringValue,omitempty"`
	IntValue    *int64   `json:"intValue,omitempty"`
	DoubleValue *float64 `json:"doubleValue,omitempty"`
	BoolValue   *bool    `json:"boolValue,omitempty"`
}

type otlpStatus struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}

type otlpEvent struct {
	Name         string          `json:"name"`
	TimeUnixNano int64           `json:"timeUnixNano"`
	Attributes   []otlpAttribute `json:"attributes,omitempty"`
}

func (e *OTLPExporter) toOTLPPayload(spans []*Span) *otlpPayload {
	otlpSpans := make([]otlpSpan, 0, len(spans))

	for _, span := range spans {
		otlpSpans = append(otlpSpans, e.convertSpan(span))
	}

	return &otlpPayload{
		ResourceSpans: []otlpResourceSpans{
			{
				Resource: e.convertResource(),
				ScopeSpans: []otlpScopeSpan{
					{
						Scope: otlpScope{
							Name:    "promptkit-telemetry",
							Version: "1.0.0",
						},
						Spans: otlpSpans,
					},
				},
			},
		},
	}
}

func (e *OTLPExporter) convertResource() otlpResource {
	attrs := make([]otlpAttribute, 0, len(e.resource.Attributes))
	for k, v := range e.resource.Attributes {
		attrs = append(attrs, convertAttribute(k, v))
	}
	return otlpResource{Attributes: attrs}
}

func (e *OTLPExporter) convertSpan(span *Span) otlpSpan {
	s := otlpSpan{
		TraceID:           span.TraceID,
		SpanID:            span.SpanID,
		ParentSpanID:      span.ParentSpanID,
		Name:              span.Name,
		Kind:              int(span.Kind),
		StartTimeUnixNano: span.StartTime.UnixNano(),
		EndTimeUnixNano:   span.EndTime.UnixNano(),
	}

	if len(span.Attributes) > 0 {
		s.Attributes = make([]otlpAttribute, 0, len(span.Attributes))
		for k, v := range span.Attributes {
			s.Attributes = append(s.Attributes, convertAttribute(k, v))
		}
	}

	if span.Status != nil {
		s.Status = &otlpStatus{
			Code:    int(span.Status.Code),
			Message: span.Status.Message,
		}
	}

	if len(span.Events) > 0 {
		s.Events = make([]otlpEvent, 0, len(span.Events))
		for _, evt := range span.Events {
			s.Events = append(s.Events, e.convertEvent(evt))
		}
	}

	return s
}

func (e *OTLPExporter) convertEvent(evt *SpanEvent) otlpEvent {
	oe := otlpEvent{
		Name:         evt.Name,
		TimeUnixNano: evt.Time.UnixNano(),
	}

	if len(evt.Attributes) > 0 {
		oe.Attributes = make([]otlpAttribute, 0, len(evt.Attributes))
		for k, v := range evt.Attributes {
			oe.Attributes = append(oe.Attributes, convertAttribute(k, v))
		}
	}

	return oe
}

func convertAttribute(key string, value interface{}) otlpAttribute {
	attr := otlpAttribute{Key: key}

	switch v := value.(type) {
	case string:
		attr.Value.StringValue = &v
	case int:
		i := int64(v)
		attr.Value.IntValue = &i
	case int64:
		attr.Value.IntValue = &v
	case float64:
		attr.Value.DoubleValue = &v
	case bool:
		attr.Value.BoolValue = &v
	default:
		s := fmt.Sprintf("%v", v)
		attr.Value.StringValue = &s
	}

	return attr
}

// Ensure OTLPExporter implements Exporter.
var _ Exporter = (*OTLPExporter)(nil)
