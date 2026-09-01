package gemini

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Streaming for the Interactions API.
//
// The wire is SSE with named events, and the step model makes the three kinds
// of output separable without heuristics — unlike generateContent, where a
// thought is a flag on an otherwise ordinary text part:
//
//	event: step.start   {"index":0,"step":{"type":"thought"}}
//	event: step.delta   {"index":0,"delta":{"type":"thought_signature","signature":"…"}}
//	event: step.stop    {"index":0}
//	event: step.start   {"index":1,"step":{"id":"call_1","type":"function_call","name":"probe"}}
//	event: step.delta   {"index":1,"delta":{"type":"arguments_delta","arguments":"{\"q\":\"x\"}"}}
//	event: step.start   {"index":1,"step":{"type":"model_output"}}
//	event: step.delta   {"index":1,"delta":{"type":"text","text":"…"}}
//	event: interaction.completed {"interaction":{…}}
//
// Shapes verified against the live API; see interactions_stream_test.go for the
// captured payloads.

const (
	sseEventPrefix = "event: "
	sseDataPrefix  = "data: "

	evStepStart            = "step.start"
	evStepDelta            = "step.delta"
	evInteractionCompleted = "interaction.completed"

	deltaTypeText      = "text"
	deltaTypeArguments = "arguments_delta"
	deltaTypeSignature = "thought_signature"

	// finishReasonToolCalls marks a turn that ended by handing off to tools
	// rather than answering.
	finishReasonToolCalls = "tool_calls"
	finishReasonStop      = "stop"

	streamChunkBuffer = 32
	sseInitialBuffer  = 64 * 1024
	sseMaxBuffer      = 8 * 1024 * 1024
)

// interactionsStreamEvent is one decoded SSE payload.
type interactionsStreamEvent struct {
	Index int              `json:"index"`
	Step  interactionsStep `json:"step"`
	Delta struct {
		Type      string `json:"type"`
		Text      string `json:"text"`
		Arguments string `json:"arguments"`
		Signature string `json:"signature"`
	} `json:"delta"`
	Interaction *interactionsResponse `json:"interaction"`
}

// streamStep tracks one in-flight step so deltas can be attributed to it.
// Deltas carry only an index, so the type must be remembered from step.start.
type streamStep struct {
	kind      string
	callID    string
	name      string
	arguments strings.Builder
}

// predictStreamWithInteractions runs a streaming turn through the Interactions
// API, which is the only way a response schema can coexist with tools.
func (p *ToolProvider) predictStreamWithInteractions(
	ctx context.Context, req providers.PredictionRequest, tools providers.ProviderTools,
) (<-chan providers.StreamChunk, error) {
	body := p.buildInteractionsRequest(req, tools)
	body.Stream = true

	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal interactions request: %w", err)
	}

	url := p.interactionsURL()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set(contentTypeHeader, applicationJSON)
	if authErr := p.applyAuth(ctx, httpReq); authErr != nil {
		return nil, fmt.Errorf("failed to apply authentication: %w", authErr)
	}
	if hdrErr := p.ApplyCustomHeaders(httpReq); hdrErr != nil {
		return nil, hdrErr
	}

	resp, err := p.GetStreamingHTTPClient().Do(httpReq)
	if err != nil {
		return nil, &providers.ProviderTransportError{Cause: err, Provider: p.ID()}
	}

	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		errBody, _ := providers.ReadResponseBody(resp.Body)
		if p.platform != "" {
			return nil, providers.ParsePlatformHTTPError(p.platform, resp.StatusCode, errBody)
		}
		return nil, &providers.ProviderHTTPError{
			StatusCode: resp.StatusCode, URL: logger.RedactSensitiveData(url),
			Body: string(errBody), Provider: p.ID(),
		}
	}

	out := make(chan providers.StreamChunk, streamChunkBuffer)
	go p.consumeInteractionsStream(ctx, resp.Body, out)
	return out, nil
}

// consumeInteractionsStream decodes the SSE body onto StreamChunks.
func (p *ToolProvider) consumeInteractionsStream(
	ctx context.Context, body io.ReadCloser, out chan<- providers.StreamChunk,
) {
	defer close(out)
	defer func() { _ = body.Close() }()

	steps := map[int]*streamStep{}
	var content strings.Builder

	emit := func(chunk providers.StreamChunk) bool {
		select {
		case out <- chunk:
			return true
		case <-ctx.Done():
			return false
		}
	}

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, sseInitialBuffer), sseMaxBuffer)

	var eventName string
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, sseEventPrefix):
			eventName = strings.TrimSpace(strings.TrimPrefix(line, sseEventPrefix))
			continue
		case !strings.HasPrefix(line, sseDataPrefix):
			continue
		}

		payload := strings.TrimPrefix(line, sseDataPrefix)
		var ev interactionsStreamEvent
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			continue
		}

		switch eventName {
		case evStepStart:
			steps[ev.Index] = &streamStep{
				kind:   ev.Step.Type,
				callID: firstNonEmpty(ev.Step.ID, ev.Step.CallID),
				name:   ev.Step.Name,
			}

		case evStepDelta:
			if !applyStepDelta(&ev, steps, &content, emit) {
				return
			}

		case evInteractionCompleted:
			emit(p.finalInteractionChunk(&ev, steps, content.String()))
			return
		}
	}

	if err := scanner.Err(); err != nil {
		emit(providers.StreamChunk{Error: fmt.Errorf("interactions stream read failed: %w", err)})
	}
}

// finalInteractionChunk builds the terminal chunk for a completed interaction.
//
// Tool calls are assembled here rather than at step.stop because their argument
// deltas are only guaranteed complete once the interaction finishes.
func (p *ToolProvider) finalInteractionChunk(
	ev *interactionsStreamEvent, steps map[int]*streamStep, content string,
) providers.StreamChunk {
	toolCalls := collectStreamToolCalls(steps)
	finish := finishReasonForInteraction(len(toolCalls) > 0)

	chunk := providers.StreamChunk{
		Content:      content,
		ToolCalls:    toolCalls,
		FinishReason: &finish,
	}
	if ev.Interaction != nil && ev.Interaction.Usage != nil {
		cost := p.CalculateCost(
			ev.Interaction.Usage.TotalInputTokens,
			ev.Interaction.Usage.TotalOutputTokens, 0)
		chunk.CostInfo = &cost
	}
	return chunk
}

// applyStepDelta routes one delta to the step it belongs to. Deltas carry only
// an index, so the step kind is remembered from step.start. Returns false when
// the consumer has gone away.
func applyStepDelta(
	ev *interactionsStreamEvent,
	steps map[int]*streamStep,
	content *strings.Builder,
	emit func(providers.StreamChunk) bool,
) bool {
	step := steps[ev.Index]
	if step == nil {
		// A delta for a step we never saw start. Ignoring it is deliberate:
		// appending would corrupt the answer with unattributable content.
		return true
	}

	switch ev.Delta.Type {
	case deltaTypeText:
		content.WriteString(ev.Delta.Text)
		return emit(providers.StreamChunk{
			Content: content.String(),
			Delta:   ev.Delta.Text,
		})

	case deltaTypeArguments:
		// Arguments arrive in fragments and are only complete at
		// interaction.completed, so they accumulate rather than emit.
		step.arguments.WriteString(ev.Delta.Arguments)
		return true

	case deltaTypeSignature:
		// A thought's signature is opaque and must be replayed on later rounds,
		// so it travels as OpaqueReasoning and never as text.
		return emit(providers.StreamChunk{
			Content: content.String(),
			OpaqueReasoning: []types.OpaqueReasoning{{
				Provider: providerNameGemini,
				Kind:     kindInteractionsThought,
				Data:     ev.Delta.Signature,
			}},
		})
	}
	return true
}

// collectStreamToolCalls assembles the function_call steps in index order.
func collectStreamToolCalls(steps map[int]*streamStep) []types.MessageToolCall {
	if len(steps) == 0 {
		return nil
	}
	maxIdx := 0
	for idx := range steps {
		if idx > maxIdx {
			maxIdx = idx
		}
	}
	var calls []types.MessageToolCall
	for i := 0; i <= maxIdx; i++ {
		step := steps[i]
		if step == nil || step.kind != stepTypeFunctionCall {
			continue
		}
		args := step.arguments.String()
		if args == "" {
			args = "{}"
		}
		calls = append(calls, types.MessageToolCall{
			ID:   step.callID,
			Name: step.name,
			Args: json.RawMessage(args),
		})
	}
	return calls
}

// finishReasonForInteraction reports why the turn ended, so a caller can tell a
// tool hand-off from a completed answer.
func finishReasonForInteraction(hasToolCalls bool) string {
	if hasToolCalls {
		return finishReasonToolCalls
	}
	return finishReasonStop
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
