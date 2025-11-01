package middleware

import (
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// debugMiddleware dumps execution context state at a specific pipeline stage
type debugMiddleware struct {
	stage string
}

// DebugMiddleware creates middleware that logs the full execution context as JSON.
// The stage parameter identifies where in the pipeline this middleware is placed.
// You can add this middleware multiple times at different stages to trace state changes.
//
// Example:
//
//	middleware.DebugMiddleware("after-prompt-assembly"),
//	middleware.DebugMiddleware("after-provider"),
//
// Note: This middleware serializes the entire context to JSON, which can be expensive.
// Only use in development/debugging scenarios.
func DebugMiddleware(stage string) pipeline.Middleware {
	return &debugMiddleware{stage: stage}
}

func (m *debugMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {

	logger.Warn("Debug middleware has been added to the pipeline with text: stage. This may affect performance.", "stage", m.stage)

	// Log before processing
	m.logContext(execCtx, "before")

	// Continue to next middleware
	err := next()

	// Log after processing (even if error occurred)
	m.logContext(execCtx, "after")

	return err
}

func (m *debugMiddleware) StreamChunk(execCtx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	// Don't log every chunk (too noisy), but you could add a flag to enable it
	return nil
}

func (m *debugMiddleware) logContext(execCtx *pipeline.ExecutionContext, timing string) {
	// Create a serializable snapshot of the context
	snapshot := m.createSnapshot(execCtx)
	// Marshal to JSON with indentation
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		logger.Debug(fmt.Sprintf("🐛 [%s:%s] Failed to marshal context: %v", m.stage, timing, err))
		return
	}

	// Log as debug message
	logger.Debug(fmt.Sprintf("🐛 [%s:%s] ExecutionContext:\n%s", m.stage, timing, string(data)))
}

// debugSnapshot is a serializable representation of ExecutionContext
type debugSnapshot struct {
	Stage           string                 `json:"stage"`
	Timing          string                 `json:"timing"`
	SystemPrompt    string                 `json:"system_prompt,omitempty"`
	Variables       map[string]string      `json:"variables,omitempty"`
	Messages        []messageSnapshot      `json:"messages,omitempty"`
	AllowedTools    []string               `json:"allowed_tools,omitempty"`
	ToolResults     []toolResultSnapshot   `json:"tool_results,omitempty"`
	Response        *responseSnapshot      `json:"response,omitempty"`
	CostInfo        costInfoSnapshot       `json:"cost_info"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
	TraceStats      traceStatsSnapshot     `json:"trace_stats"`
	RawResponseSize string                 `json:"raw_response_size,omitempty"`
}

type messageSnapshot struct {
	Role             string            `json:"role"`
	ContentLen       int               `json:"content_length"`
	ContentPreview   string            `json:"content_preview,omitempty"` // First 100 chars
	ToolCallsCount   int               `json:"tool_calls_count,omitempty"`
	LatencyMs        int64             `json:"latency_ms,omitempty"`
	CostInfo         *costInfoSnapshot `json:"cost_info,omitempty"`
	Timestamp        string            `json:"timestamp,omitempty"`
	ValidationsCount int               `json:"validations_count,omitempty"`
}

type toolResultSnapshot struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ContentLen int    `json:"content_length"`
	Error      string `json:"error,omitempty"`
	LatencyMs  int64  `json:"latency_ms"`
}

type responseSnapshot struct {
	Content       string   `json:"content,omitempty"`
	ToolCallNames []string `json:"tool_call_names,omitempty"`
	StopReason    string   `json:"stop_reason,omitempty"`
}

type costInfoSnapshot struct {
	InputTokens   int     `json:"input_tokens"`
	OutputTokens  int     `json:"output_tokens"`
	CachedTokens  int     `json:"cached_tokens,omitempty"`
	InputCostUSD  float64 `json:"input_cost_usd"`
	OutputCostUSD float64 `json:"output_cost_usd"`
	CachedCostUSD float64 `json:"cached_cost_usd,omitempty"`
	TotalCost     float64 `json:"total_cost_usd"`
}

type traceStatsSnapshot struct {
	LLMCallCount int    `json:"llm_call_count"`
	StartedAt    string `json:"started_at,omitempty"`
	CompletedAt  string `json:"completed_at,omitempty"`
	Duration     string `json:"duration,omitempty"`
}

func (m *debugMiddleware) createSnapshot(execCtx *pipeline.ExecutionContext) debugSnapshot {
	snapshot := debugSnapshot{
		Stage:        m.stage,
		SystemPrompt: execCtx.SystemPrompt,
		Variables:    execCtx.Variables,
		Metadata:     execCtx.Metadata,
		CostInfo: costInfoSnapshot{
			InputTokens:   execCtx.CostInfo.InputTokens,
			OutputTokens:  execCtx.CostInfo.OutputTokens,
			CachedTokens:  execCtx.CostInfo.CachedTokens,
			InputCostUSD:  execCtx.CostInfo.InputCostUSD,
			OutputCostUSD: execCtx.CostInfo.OutputCostUSD,
			CachedCostUSD: execCtx.CostInfo.CachedCostUSD,
			TotalCost:     execCtx.CostInfo.TotalCost,
		},
	}

	// Capture allowed tools (just names)
	snapshot.AllowedTools = execCtx.AllowedTools

	// Capture messages (with truncation)
	for _, msg := range execCtx.Messages {
		msgSnap := messageSnapshot{
			Role:             msg.Role,
			ContentLen:       len(msg.Content),
			ToolCallsCount:   len(msg.ToolCalls),
			LatencyMs:        msg.LatencyMs,
			ValidationsCount: len(msg.Validations),
		}

		// Preview first 100 chars
		if len(msg.Content) > 0 {
			preview := msg.Content
			if len(preview) > 100 {
				preview = preview[:100] + "..."
			}
			msgSnap.ContentPreview = preview
		}

		// Capture timestamp
		if !msg.Timestamp.IsZero() {
			msgSnap.Timestamp = msg.Timestamp.Format("2006-01-02T15:04:05.000Z")
		}

		// Capture cost info if present
		if msg.CostInfo != nil {
			msgSnap.CostInfo = &costInfoSnapshot{
				InputTokens:   msg.CostInfo.InputTokens,
				OutputTokens:  msg.CostInfo.OutputTokens,
				CachedTokens:  msg.CostInfo.CachedTokens,
				InputCostUSD:  msg.CostInfo.InputCostUSD,
				OutputCostUSD: msg.CostInfo.OutputCostUSD,
				CachedCostUSD: msg.CostInfo.CachedCostUSD,
				TotalCost:     msg.CostInfo.TotalCost,
			}
		}

		snapshot.Messages = append(snapshot.Messages, msgSnap)
	}

	// Capture tool results
	for _, tr := range execCtx.ToolResults {
		snapshot.ToolResults = append(snapshot.ToolResults, toolResultSnapshot{
			ID:         tr.ID,
			Name:       tr.Name,
			ContentLen: len(tr.Content),
			Error:      tr.Error,
			LatencyMs:  tr.LatencyMs,
		})
	}

	// Capture response
	if execCtx.Response != nil {
		respSnap := responseSnapshot{
			Content: execCtx.Response.Content,
		}
		for _, tc := range execCtx.Response.ToolCalls {
			respSnap.ToolCallNames = append(respSnap.ToolCallNames, tc.Name)
		}
		snapshot.Response = &respSnap
	}

	// Capture trace stats
	snapshot.TraceStats = traceStatsSnapshot{
		LLMCallCount: len(execCtx.Trace.LLMCalls),
	}
	if !execCtx.Trace.StartedAt.IsZero() {
		snapshot.TraceStats.StartedAt = execCtx.Trace.StartedAt.Format("2006-01-02T15:04:05.000Z")
	}
	if execCtx.Trace.CompletedAt != nil && !execCtx.Trace.CompletedAt.IsZero() {
		snapshot.TraceStats.CompletedAt = execCtx.Trace.CompletedAt.Format("2006-01-02T15:04:05.000Z")
		snapshot.TraceStats.Duration = execCtx.Trace.CompletedAt.Sub(execCtx.Trace.StartedAt).String()
	}

	// Note size of raw response without serializing it
	if execCtx.RawResponse != nil {
		snapshot.RawResponseSize = "present (interface{})"
	}

	return snapshot
}
