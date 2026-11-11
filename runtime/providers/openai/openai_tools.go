package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/schemaadapt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// ToolProvider extends OpenAIProvider with tool support
type ToolProvider struct {
	*Provider
}

// NewToolProvider creates a new OpenAI provider with tool support
func NewToolProvider(
	id, model, baseURL string,
	defaults providers.ProviderDefaults,
	includeRawOutput bool,
	additionalConfig map[string]any,
	unsupportedParams []string,
) *ToolProvider {
	return &ToolProvider{
		Provider: NewProviderFromConfig(&ProviderConfig{
			ID: id, Model: model, BaseURL: baseURL, Defaults: defaults,
			IncludeRawOutput: includeRawOutput, AdditionalConfig: additionalConfig,
			UnsupportedParams: unsupportedParams,
		}),
	}
}

// NewToolProviderWithCredential creates an OpenAI tool provider with explicit credential.
func NewToolProviderWithCredential(
	id, model, baseURL string, defaults providers.ProviderDefaults,
	includeRawOutput bool, additionalConfig map[string]any, cred providers.Credential,
	platform string, platformConfig *providers.PlatformConfig,
	unsupportedParams []string,
) *ToolProvider {
	return &ToolProvider{
		Provider: NewProviderFromConfig(&ProviderConfig{
			ID: id, Model: model, BaseURL: baseURL, Defaults: defaults,
			IncludeRawOutput: includeRawOutput, Credential: cred,
			AdditionalConfig: additionalConfig, Platform: platform,
			PlatformConfig: platformConfig, UnsupportedParams: unsupportedParams,
		}),
	}
}

// OpenAI-specific tool structures
type openAITool struct {
	Type     string             `json:"type"`
	Function openAIToolFunction `json:"function"`
}

type openAIToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      bool            `json:"strict,omitempty"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // OpenAI returns this as a string, not RawMessage
}

// BuildTooling converts tool descriptors to OpenAI format.
// By default, strict mode is enabled for reliable argument generation.
// Set additional_config.strict_tools: false in provider config to disable.
func (p *ToolProvider) BuildTooling(descriptors []*providers.ToolDescriptor) (providers.ProviderTools, error) {
	if len(descriptors) == 0 {
		return nil, nil
	}

	strict := p.useStrictTools()
	tools := make([]openAITool, len(descriptors))
	for i, desc := range descriptors {
		params := types.NormalizeRawMessage(desc.InputSchema)
		if strict {
			params = ensureStrictSchema(params)
		}
		tools[i] = openAITool{
			Type: typeFunction,
			Function: openAIToolFunction{
				Name:        desc.Name,
				Description: desc.Description,
				Parameters:  params,
				Strict:      strict,
			},
		}
	}

	return tools, nil
}

// strictResponseSchema prepares a caller's response schema for the wire.
//
// Strict mode applies the same two rules to a response schema as to a tool
// schema, and OpenAI rejects the request when either is broken:
// additionalProperties: false on every object, and every property listed in
// required. BuildTooling has run ensureStrictSchema over tool schemas since
// strict mode was added; both response-format builders posted the caller's
// schema verbatim, so a schema with a nested object — the shape in #2055 —
// 400s on the one path and works on the other.
//
// A non-strict response format is sent as written: without strict: true
// OpenAI treats the schema as guidance and enforces neither rule, so
// rewriting it would change the caller's contract for no gain.
func strictResponseSchema(rf *providers.ResponseFormat) json.RawMessage {
	if rf == nil {
		return nil
	}
	if !rf.Strict {
		return rf.JSONSchema
	}
	return ensureStrictSchema(rf.JSONSchema)
}

// ensureStrictSchema modifies a JSON schema for OpenAI strict mode:
// - Sets additionalProperties: false on all object types (recursively)
// - Ensures all properties are listed in required
//
// Traversal is schemaadapt.Walk rather than a private recursion. The private
// one descended through "properties" and a map-valued "items" and nothing
// else, so an object under $defs, under an anyOf branch, or under a
// tuple-form items was left non-compliant and the API rejected the request —
// the same nested-node miss #2055 reported on the Anthropic side.
func ensureStrictSchema(schema json.RawMessage) json.RawMessage {
	return schemaadapt.Rewrite(schema, applyStrictToObject)
}

// applyStrictToObject applies OpenAI strict mode's two rules to one node.
func applyStrictToObject(obj map[string]any) {
	props, ok := obj["properties"].(map[string]any)
	if !ok {
		if !schemaadapt.IsObjectNode(obj) {
			return
		}
		// Bare object with no properties — add empty properties and
		// additionalProperties:false for strict compatibility.
		obj["properties"] = map[string]any{}
		obj["additionalProperties"] = false
		obj["required"] = []string{}
		return
	}

	obj["additionalProperties"] = false

	// Strict mode requires every property in required. Sorted, because map
	// iteration order is random and an unstable required list rewrites the
	// request bytes on every call, which throws away the prompt cache.
	allKeys := make([]string, 0, len(props))
	for k := range props {
		allKeys = append(allKeys, k)
	}
	sort.Strings(allKeys)
	obj["required"] = allKeys
}

// useStrictTools returns whether tools should use strict schema mode.
// Defaults to true; override with additional_config.strict_tools: false.
func (p *ToolProvider) useStrictTools() bool {
	if p.additionalConfig == nil {
		return true
	}
	if v, ok := p.additionalConfig["strict_tools"].(bool); ok {
		return v
	}
	return true
}

// PredictWithTools runs predictWithToolsOnce, retrying without any sampling
// parameter the API rejects for this model.
func (p *ToolProvider) PredictWithTools(
	ctx context.Context, req providers.PredictionRequest, tools providers.ProviderTools, toolChoice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	return providers.RetryToolCall(&p.BaseProvider, req,
		func(r providers.PredictionRequest) (providers.PredictionResponse, []types.MessageToolCall, error) {
			return p.predictWithToolsOnce(ctx, r, tools, toolChoice)
		})
}

// predictWithToolsOnce performs a prediction request with tool support
func (p *ToolProvider) predictWithToolsOnce(
	ctx context.Context,
	req providers.PredictionRequest,
	tools providers.ProviderTools,
	toolChoice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	// Route to appropriate API based on mode
	if p.apiMode == APIModeResponses {
		return p.predictWithResponses(ctx, req, tools, toolChoice)
	}

	// Legacy chat completions API
	return p.predictWithCompletions(ctx, req, tools, toolChoice)
}

// predictWithCompletions performs a prediction using the chat completions API
func (p *ToolProvider) predictWithCompletions(
	ctx context.Context,
	req providers.PredictionRequest,
	tools providers.ProviderTools,
	toolChoice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	// Track latency - START timing
	start := time.Now()

	// Build OpenAI request with tools
	openaiReq, err := p.buildToolRequest(ctx, req, tools, toolChoice)
	if err != nil {
		return providers.PredictionResponse{}, nil, err
	}
	providers.ApplyExtraBody(p.ID(), openaiReq, p.extraBody)

	// Prepare response with raw request if configured (set early to preserve on error)
	predictResp := providers.PredictionResponse{}
	if p.ShouldIncludeRawOutput() {
		predictResp.RawRequest = openaiReq
	}

	// Make the API call
	respBytes, err := p.makeRequest(ctx, openaiReq)
	if err != nil {
		predictResp.Latency = time.Since(start)
		return predictResp, nil, err
	}

	// Calculate latency immediately after API call completes
	latency := time.Since(start)

	// Parse response
	resp, toolCalls, err := p.parseToolResponse(respBytes)
	if err != nil {
		resp.Latency = latency
		resp.RawRequest = predictResp.RawRequest
		return resp, nil, err
	}

	// Set latency on response
	resp.Latency = latency
	resp.RawRequest = predictResp.RawRequest

	return resp, toolCalls, nil
}

// buildToolRequest constructs the OpenAI API request with tools
func (p *ToolProvider) buildToolRequest(
	ctx context.Context, req providers.PredictionRequest, tools interface{}, toolChoice string,
) (map[string]interface{}, error) {
	messages, err := p.convertRequestMessagesToOpenAI(ctx, req)
	if err != nil {
		return nil, err
	}

	// Build request
	openaiReq := map[string]interface{}{
		keyModel:    p.model,
		keyMessages: messages,
	}

	// Every field shared with the non-tool paths — max tokens, sampling params,
	// seed, response_format and audio modalities — comes from the one builder
	// both paths use. This hand-rolled its own copy until #1742, and the copy had
	// drifted: response_format was set only in enrichRequest, so a pack combining
	// structured output with tools silently lost its schema. Adding a field here
	// by hand is how that happens; add it to enrichRequest instead.
	p.enrichRequest(openaiReq, &req, audioFormatWAV)

	// Add tools if provided. parallel_tool_calls belongs strictly inside this
	// guard: OpenAI rejects it when no tools are declared ("only allowed when
	// 'tools' are specified"), and an emptied tool set still reaches this
	// builder since #1735 routes tool-carrying history down the tool path.
	if tools != nil {
		openaiReq["tools"] = tools
		p.addToolChoiceToRequest(openaiReq, toolChoice)
		if p.parallelToolCalls != nil {
			openaiReq["parallel_tool_calls"] = *p.parallelToolCalls
		}
	}

	return openaiReq, nil
}

// convertRequestMessagesToOpenAI converts all messages in a request to OpenAI format
func (p *ToolProvider) convertRequestMessagesToOpenAI(
	ctx context.Context, req providers.PredictionRequest,
) ([]map[string]interface{}, error) {
	messages := make([]map[string]interface{}, 0, len(req.Messages)+1)

	// Add system message if present
	if req.System != "" {
		messages = append(messages, map[string]interface{}{
			keyRole:    roleSystem,
			keyContent: req.System,
		})
	}

	// Add conversation messages. Media from a run of tool results follows the
	// run, one user message per result (see liftedMediaMessages).
	var round []*types.MessageToolResult
	flushRound := func() error {
		lifted := p.liftedMediaMessages(round)
		round = nil
		for i := range lifted {
			openaiMsg, err := p.convertSingleMessageForTools(ctx, lifted[i])
			if err != nil {
				return fmt.Errorf("tool result media: %w", err)
			}
			messages = append(messages, openaiMsg)
		}
		return nil
	}
	for i := range req.Messages {
		isToolResult := req.Messages[i].Role == roleToolResult && req.Messages[i].ToolResult != nil
		if !isToolResult {
			if err := flushRound(); err != nil {
				return nil, err
			}
		}
		openaiMsg, err := p.convertSingleMessageForTools(ctx, req.Messages[i])
		if err != nil {
			return nil, fmt.Errorf("failed to convert message %d: %w", i, err)
		}
		messages = append(messages, openaiMsg)
		if isToolResult {
			round = append(round, req.Messages[i].ToolResult)
		}
	}
	if err := flushRound(); err != nil {
		return nil, err
	}

	return messages, nil
}

// convertSingleMessageForTools converts a single message to OpenAI format including tool metadata.
// A part that cannot be converted is an error, as on the non-tool path: falling
// back to the message's text used to send the model the question without its media.
func (p *ToolProvider) convertSingleMessageForTools(
	ctx context.Context, msg types.Message,
) (map[string]interface{}, error) {
	// Convert message to OpenAI format (handles both legacy and multimodal)
	convertedMsg, err := p.convertMessageToOpenAI(ctx, msg)
	if err != nil {
		return nil, err
	}

	// Build the message map with content
	openaiMsg := map[string]interface{}{
		keyRole:    convertedMsg.Role,
		keyContent: convertedMsg.Content,
	}

	// Add tool-related fields if present
	if len(msg.ToolCalls) > 0 {
		openaiMsg["tool_calls"] = p.convertToolCallsToOpenAI(msg.ToolCalls)
	}

	// Handle tool result messages
	if msg.Role == "tool" && msg.ToolResult != nil {
		openaiMsg["tool_call_id"] = msg.ToolResult.ID
		openaiMsg[keyName] = msg.ToolResult.Name
		openaiMsg[keyContent] = p.toolMessageText(msg.ToolResult)
	}

	return openaiMsg, nil
}

// replayArgs renders a tool call's arguments for replay in history. Arguments
// the model produced that are not valid JSON (some models answer a
// zero-argument tool with "{") replay as "{}": OpenAI-compatible servers parse
// history strictly, so replaying them verbatim fails every later request in
// the conversation, not just the round that produced them (#2103). The
// model already saw the validation error as that call's result.
func replayArgs(args json.RawMessage) string {
	return string(types.NormalizeRawMessage(args))
}

// convertToolCallsToOpenAI converts ToolCalls to OpenAI format
func (p *ToolProvider) convertToolCallsToOpenAI(toolCalls []types.MessageToolCall) []map[string]interface{} {
	result := make([]map[string]interface{}, len(toolCalls))
	for i, tc := range toolCalls {
		result[i] = map[string]interface{}{
			"id":    tc.ID,
			keyType: typeFunction,
			keyFunction: map[string]interface{}{
				keyName:     tc.Name,
				"arguments": replayArgs(tc.Args),
			},
		}
	}
	return result
}

// addToolChoiceToRequest adds tool_choice parameter based on the choice string
func (p *ToolProvider) addToolChoiceToRequest(openaiReq map[string]interface{}, toolChoice string) {
	if toolChoice == "" || toolChoice == toolChoiceAuto {
		return
	}

	switch toolChoice {
	case toolChoiceRequired:
		openaiReq["tool_choice"] = toolChoiceRequired
	case toolChoiceNone:
		openaiReq["tool_choice"] = toolChoiceNone
	default:
		// Specific function name
		openaiReq["tool_choice"] = map[string]interface{}{
			keyType: typeFunction,
			keyFunction: map[string]string{
				keyName: toolChoice,
			},
		}
	}
}

// parseToolResponse parses the OpenAI response and extracts tool calls
func (p *ToolProvider) parseToolResponse(
	respBytes []byte,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	var openaiResp struct {
		Choices []struct {
			Message struct {
				Content   string               `json:"content"`
				Audio     *openAIAudioResponse `json:"audio,omitempty"`
				ToolCalls []openAIToolCall     `json:"tool_calls,omitempty"`
				// ReasoningContent is the reasoning summary a reasoning model
				// returns. This is the TOOL-CALLING round, where the model
				// explains why it chose the calls it did — the reasoning a
				// consumer most wants and the terminal response cannot supply.
				ReasoningContent string `json:"reasoning_content,omitempty"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage openAIUsage `json:"usage"`
	}

	if err := json.Unmarshal(respBytes, &openaiResp); err != nil {
		return providers.PredictionResponse{}, nil, fmt.Errorf("failed to parse OpenAI response: %w", err)
	}

	if len(openaiResp.Choices) == 0 {
		return providers.PredictionResponse{}, nil, fmt.Errorf("no choices in OpenAI response")
	}

	choice := openaiResp.Choices[0]

	// Calculate cost breakdown. Route the full wire usage through
	// costFromUsage (not the CalculateCost wrapper) so reasoning tokens are
	// priced rather than silently dropped.
	costBreakdown := p.costFromUsage(openaiResp.Usage)

	resp := providers.PredictionResponse{
		Content:      choice.Message.Content,
		CostInfo:     &costBreakdown,
		Raw:          respBytes,
		FinishReason: providers.NormalizeOpenAIFinishReason(choice.FinishReason),
		Reasoning:    reasoningFromContent(choice.Message.ReasoningContent),
	}

	if audio := choice.Message.Audio; audio != nil {
		if audio.Transcript != "" {
			resp.Content = audio.Transcript
			resp.Parts = append(resp.Parts, types.NewTextPart(audio.Transcript))
		}
		if audio.Data != "" {
			audioFormat := getStringConfigOrDefault(p.additionalConfig, "audio_format", audioFormatWAV)
			resp.Parts = append(resp.Parts, types.NewAudioPartFromData(audio.Data, audioFormatToMIME(audioFormat)))
		}
	}

	// Extract tool calls
	var toolCalls []types.MessageToolCall
	for _, tc := range choice.Message.ToolCalls {
		toolCalls = append(toolCalls, types.MessageToolCall{
			Name: tc.Function.Name,
			Args: json.RawMessage(tc.Function.Arguments), // Convert string to RawMessage
			ID:   tc.ID,
		})
	}

	return resp, toolCalls, nil
}

// makeRequest makes an HTTP request to the OpenAI API
func (p *ToolProvider) makeRequest(ctx context.Context, request interface{}) ([]byte, error) {
	url := p.chatCompletionsURL()
	if p.credential != nil {
		return p.makeCredentialRequest(ctx, url, request)
	}
	headers := providers.RequestHeaders{
		contentTypeHeader:   applicationJSON,
		authorizationHeader: bearerPrefix + p.apiKey,
	}
	return p.MakeJSONRequest(ctx, url, request, headers, "OpenAI")
}

// makeCredentialRequest sends the request authenticated through the
// configured credential and returns the response body.
func (p *ToolProvider) makeCredentialRequest(ctx context.Context, url string, request interface{}) ([]byte, error) {
	reqBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set(contentTypeHeader, applicationJSON)
	if authErr := p.AuthorizeRequest(ctx, httpReq); authErr != nil {
		return nil, fmt.Errorf("failed to apply authentication: %w", authErr)
	}
	if hdrErr := p.ApplyCustomHeaders(httpReq); hdrErr != nil {
		return nil, hdrErr
	}
	resp, doErr := p.GetHTTPClient().Do(httpReq)
	if doErr != nil {
		return nil, &providers.ProviderTransportError{Cause: doErr, Provider: p.ID()}
	}
	defer resp.Body.Close()
	body, readErr := providers.ReadResponseBody(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("failed to read response: %w", readErr)
	}
	if resp.StatusCode != http.StatusOK {
		if p.platform != "" {
			return nil, providers.ParsePlatformHTTPError(p.platform, resp.StatusCode, body)
		}
		return nil, &providers.ProviderHTTPError{
			StatusCode: resp.StatusCode, URL: url,
			Body: string(body), Provider: p.ID(),
		}
	}
	return body, nil
}

// PredictStreamWithTools runs predictStreamWithToolsOnce, retrying without
// any sampling parameter the API rejects for this model.
func (p *ToolProvider) PredictStreamWithTools(
	ctx context.Context, req providers.PredictionRequest, tools any, toolChoice string,
) (<-chan providers.StreamChunk, error) {
	return providers.RetryCall(&p.BaseProvider, req,
		func(r providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
			return p.predictStreamWithToolsOnce(ctx, r, tools, toolChoice)
		})
}

// predictStreamWithToolsOnce performs a streaming predict request with tool support.
//
// Bedrock note: same fallback as PredictStream — Bedrock's streaming
// endpoint uses binary event-stream framing distinct from SSE; we run a
// single non-streaming call and surface it as one terminal chunk.
func (p *ToolProvider) predictStreamWithToolsOnce(
	ctx context.Context,
	req providers.PredictionRequest,
	tools interface{},
	toolChoice string,
) (<-chan providers.StreamChunk, error) {
	if p.isBedrock() {
		return p.predictStreamWithToolsBedrockFallback(ctx, req, tools, toolChoice)
	}

	// Route to appropriate API based on mode
	if p.apiMode == APIModeResponses {
		return p.predictStreamWithResponses(ctx, req, tools, toolChoice)
	}

	// Legacy chat completions API
	return p.predictStreamWithCompletions(ctx, req, tools, toolChoice)
}

// predictStreamWithToolsBedrockFallback runs PredictWithTools synchronously
// and emits a single terminal chunk that carries content, tool calls,
// finish reason and cost. See PredictStream for the rationale.
func (p *ToolProvider) predictStreamWithToolsBedrockFallback(
	ctx context.Context,
	req providers.PredictionRequest,
	tools interface{},
	toolChoice string,
) (<-chan providers.StreamChunk, error) {
	out := make(chan providers.StreamChunk, 1)
	resp, toolCalls, err := p.PredictWithTools(ctx, req, tools, toolChoice)
	if err != nil {
		close(out)
		return nil, err
	}
	finish := finishStop
	if len(toolCalls) > 0 {
		finish = types.FinishReasonToolUse
	}
	out <- providers.StreamChunk{
		Content:      resp.Content,
		Delta:        resp.Content,
		ToolCalls:    toolCalls,
		CostInfo:     resp.CostInfo,
		FinishReason: &finish,
	}
	close(out)
	return out, nil
}

// predictStreamWithCompletions performs a streaming predict using chat completions API
func (p *ToolProvider) predictStreamWithCompletions(
	ctx context.Context,
	req providers.PredictionRequest,
	tools interface{},
	toolChoice string,
) (<-chan providers.StreamChunk, error) {
	// Build OpenAI request with tools (same as non-streaming)
	openaiReq, err := p.buildToolRequest(ctx, req, tools, toolChoice)
	if err != nil {
		return nil, err
	}

	// Add streaming options
	openaiReq["stream"] = true
	openaiReq["stream_options"] = map[string]interface{}{
		keyIncludeUsage: true,
	}

	// When streaming, OpenAI chat/completions only accepts "pcm16" for audio.format.
	// buildToolRequest defaults to "wav" (valid for non-streaming); override here.
	if audio, ok := openaiReq["audio"].(map[string]interface{}); ok {
		audio[keyFormat] = audioPCM16Format
	}
	providers.ApplyExtraBody(p.ID(), openaiReq, p.extraBody)

	reqBody, err := json.Marshal(openaiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := p.chatCompletionsURL()
	requestFn := func(ctx context.Context) (*http.Request, error) {
		httpReq, reqErr := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
		if reqErr != nil {
			return nil, fmt.Errorf("failed to create request: %w", reqErr)
		}
		httpReq.Header.Set(contentTypeHeader, applicationJSON)
		if authErr := p.AuthorizeRequest(ctx, httpReq); authErr != nil {
			return nil, fmt.Errorf("failed to apply authentication: %w", authErr)
		}
		httpReq.Header.Set("Accept", "text/event-stream")
		if hdrErr := p.ApplyCustomHeaders(httpReq); hdrErr != nil {
			return nil, hdrErr
		}
		return httpReq, nil
	}

	return p.RunStreamingRequest(ctx, &providers.StreamRetryRequest{
		Policy:       p.StreamRetryPolicy(),
		Budget:       p.StreamRetryBudget(),
		ProviderName: p.ID(),
		Host:         providers.HostFromURL(url),
		IdleTimeout:  p.StreamIdleTimeout(),
		RequestFn:    requestFn,
		Client:       p.GetStreamingHTTPClient(),
	}, p.streamResponse)
}

//nolint:gochecknoinits // Factory registration requires init
func init() {
	// (openai, vertex) was rejected under #1009 because Vertex hosts no
	// OpenAI-shaped endpoint, so the pair could only 404 at the wire. That is
	// no longer true: Vertex exposes an OpenAI-compatible Chat Completions API
	// covering Model Garden, and the platform switch now builds its URL from
	// project/region with an ADC bearer token (#1768). The rejection is lifted
	// rather than narrowed — with a base URL and a credential, the combination
	// is real.
	providers.RegisterProviderFactory("openai",
		providers.CredentialFactory(
			func(spec providers.ProviderSpec) (providers.Provider, error) {
				tp := NewToolProviderWithCredential(
					spec.ID, spec.Model, spec.BaseURL, spec.Defaults,
					spec.IncludeRawOutput, spec.AdditionalConfig, spec.Credential,
					spec.Platform, spec.PlatformConfig, spec.UnsupportedParams,
				)
				tp.setCapabilities(spec.Capabilities)
				return tp, nil
			},
			func(spec providers.ProviderSpec) (providers.Provider, error) {
				tp := NewToolProvider(
					spec.ID, spec.Model, spec.BaseURL, spec.Defaults,
					spec.IncludeRawOutput, spec.AdditionalConfig, spec.UnsupportedParams,
				)
				tp.setCapabilities(spec.Capabilities)
				return tp, nil
			},
		),
	)
}

// WithMediaSettings returns a view of p, and of the Provider it wraps, that
// uses m's media settings (providers.MediaScoped).
func (p *ToolProvider) WithMediaSettings(m providers.MediaSettings) providers.Provider {
	view := *p
	view.Provider = p.Provider.WithMediaSettings(m).(*Provider)
	return &view
}
