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

	useStrict := p.useStrictTools()
	tools := make([]openAITool, len(descriptors))
	for i, desc := range descriptors {
		params := types.NormalizeRawMessage(desc.InputSchema)
		// Strict is decided per tool: a schema strict mode cannot express is
		// sent non-strict and verbatim, and costs no other tool strict mode.
		strict := useStrict && strictCompatible(params)
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
// rewriting it would change the caller's contract for no gain. A strict one
// reaches here only after checkStrictResponseFormat has accepted it.
func strictResponseSchema(rf *providers.ResponseFormat) json.RawMessage {
	if rf == nil {
		return nil
	}
	if !rf.Strict {
		return rf.JSONSchema
	}
	return ensureStrictSchema(rf.JSONSchema)
}

// checkStrictResponseFormat refuses a strict response format whose schema
// strict mode cannot express, before any request is built.
//
// A tool with such a schema is sent non-strict (BuildTooling), because the
// tool's author never chose strict mode. A response format's caller did, so
// sending it non-strict would silently drop the guarantee they asked for, and
// sending it strict either 400s at OpenAI (a keyword strict mode refuses) or
// rewrites it into a different contract (a map-typed object closed to accept
// no keys). The error names the offending keyword and where it sits.
func checkStrictResponseFormat(rf *providers.ResponseFormat) error {
	if rf == nil || !rf.Strict || rf.Type != providers.ResponseFormatJSONSchema || len(rf.JSONSchema) == 0 {
		return nil
	}
	v := strictViolationOf(rf.JSONSchema)
	if v == nil {
		return nil
	}
	name := rf.SchemaName
	if name == "" {
		name = defaultResponseSchema
	}
	return fmt.Errorf("openai: response_format %q asks for strict mode, but strict mode cannot express its "+
		"schema: %q at %s %s; change the schema or send the response format with strict: false",
		name, v.keyword, v.path, v.problem)
}

// ensureStrictSchema modifies a JSON schema for OpenAI strict mode:
// - Sets additionalProperties: false on all object types (recursively)
// - Ensures all properties are listed in required
//
// Traversal is schemaadapt.Walk rather than a private recursion. The private
// one descended through "properties" and a map-valued "items" and nothing
// else, so an object under $defs or under an anyOf branch was left
// non-compliant and the API rejected the request — the same nested-node miss
// #2055 reported on the Anthropic side. Walk does not descend into a
// tuple-form (array-valued) items; strict mode refuses that form outright, so
// strictCompatible sends such a tool non-strict and it never reaches here.
func ensureStrictSchema(schema json.RawMessage) json.RawMessage {
	return schemaadapt.Rewrite(schema, applyStrictToObject)
}

// strictUnsupportedKeywords are keywords OpenAI strict mode refuses: a schema
// carrying one anywhere 400s the whole request ("'minProperties' is not
// permitted"), so the turn fails before the model is called (#2758).
//
// Source: "Supported schemas" in
// https://developers.openai.com/api/docs/guides/structured-outputs#supported-schemas
// lists allOf, not, dependentRequired, dependentSchemas, if, then and else as
// unsupported. The rest are keywords the docs omit from the supported list and
// the API refuses with "is not permitted" when sent strict (probed 2026-11).
//
// It is a denylist, not an allowlist, because the API ignores keywords it does
// not know (title, default, examples, x-* extensions, nullable): an allowlist
// would cost every tool carrying an annotation its strict mode for nothing.
var strictUnsupportedKeywords = []string{
	// Listed unsupported by the docs, plus "dependencies", the draft-07
	// spelling of dependentRequired/dependentSchemas: the API accepts it but
	// does not enforce it, and Walk does not descend into it.
	"allOf", "not", "dependentRequired", "dependentSchemas", "dependencies",
	"if", "then", "else",
	// Refused by the API under strict mode.
	"oneOf", "minProperties", "maxProperties", "propertyNames",
	"unevaluatedProperties", "unevaluatedItems", "uniqueItems",
	"contains", "minContains", "maxContains", "prefixItems", "$dynamicRef",
}

// strictSupportedFormats are the string formats strict mode accepts, from the
// same "Supported schemas" section; any other format value 400s the request
// ("'uri' is not a valid format").
var strictSupportedFormats = map[string]bool{
	"date-time": true, "time": true, "date": true, "duration": true, "email": true,
	"hostname": true, "ipv4": true, "ipv6": true, "uuid": true,
}

// strictCompatible reports whether a tool schema can be sent under strict mode
// without the API refusing it or ensureStrictSchema changing what it accepts.
//
// Any of these, on any node, rules it out:
//   - a keyword strict mode refuses, or a string format it does not support;
//   - a tuple-form (array-valued) items, which strict mode refuses;
//   - an object whose additionalProperties is not false — a map-typed object
//     (schema-valued additionalProperties) or one explicitly open (true).
//     Strict mode requires additionalProperties: false, so rewriting either
//     would change the tool's contract; a map would accept no keys at all;
//   - a nested free-form object: an object below the root that declares no
//     properties and no additionalProperties, so accepts any keys. Closing it
//     would leave it accepting none.
//
// An object that declares properties but no additionalProperties keeps the
// standard strict rewrite: that is how most hand-written schemas spell a
// closed object. So does a ROOT object with no properties: that is a tool
// taking no arguments, and closing it is what it means. A schema that does
// not decode is left to the strict path, which sends it unchanged for the API
// to report.
func strictCompatible(schema json.RawMessage) bool {
	return strictViolationOf(schema) == nil
}

// strictViolation is the first thing in a schema that strict mode cannot
// express: the keyword, the JSONPath of the node carrying it, and why.
type strictViolation struct {
	keyword string
	path    string
	problem string
}

// strictViolationOf returns the first node, in schemaadapt.WalkPath order,
// that breaks one of strictCompatible's rules, or nil when there is none.
func strictViolationOf(schema json.RawMessage) *strictViolation {
	var doc any
	if len(schema) == 0 || json.Unmarshal(schema, &doc) != nil {
		return nil
	}
	var found *strictViolation
	schemaadapt.WalkPath(doc, func(path string, node map[string]any) {
		if found != nil {
			return
		}
		if keyword, problem, bad := strictNodeViolation(node, path == schemaadapt.RootPath); bad {
			found = &strictViolation{keyword: keyword, path: path, problem: problem}
		}
	})
	return found
}

// strictNodeViolation applies strictCompatible's rules to one schema node.
func strictNodeViolation(node map[string]any, root bool) (keyword, problem string, bad bool) {
	for _, k := range strictUnsupportedKeywords {
		if _, present := node[k]; present {
			return k, "is not supported in strict mode", true
		}
	}
	if f, isString := node["format"].(string); isString && !strictSupportedFormats[f] {
		return "format", fmt.Sprintf("is %q, which strict mode does not support", f), true
	}
	if _, tuple := node["items"].([]any); tuple {
		return "items", "is a list (tuple form), which strict mode does not support", true
	}
	if ap, hasAP := node["additionalProperties"]; hasAP {
		if ap != false {
			return "additionalProperties", "is not false (strict mode requires every object to be closed)", true
		}
		return "", "", false
	}
	if !root && isFreeFormObject(node) {
		return "properties", "declares none on an object that sets no additionalProperties, so it accepts " +
			"any keys; strict mode would close it to accept none", true
	}
	return "", "", false
}

// isFreeFormObject reports whether node is an object schema that declares no
// properties (none, or an empty map) and says nothing about additional ones,
// so JSON Schema lets it hold any keys.
func isFreeFormObject(node map[string]any) bool {
	if !schemaadapt.IsObjectNode(node) {
		return false
	}
	props, _ := node["properties"].(map[string]any)
	return len(props) == 0
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
// parameter the API rejects for this model, and on the Responses API if Chat
// Completions rejects the model as Responses-only.
func (p *ToolProvider) PredictWithTools(
	ctx context.Context, req providers.PredictionRequest, tools providers.ProviderTools, toolChoice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	var resp providers.PredictionResponse
	var calls []types.MessageToolCall
	err := p.withResponsesFallback(func(v *Provider) (err error) {
		tp := &ToolProvider{Provider: v}
		resp, calls, err = providers.RetryToolCall(&v.BaseProvider, req,
			func(r providers.PredictionRequest) (providers.PredictionResponse, []types.MessageToolCall, error) {
				return tp.predictWithToolsOnce(ctx, r, tools, toolChoice)
			})
		return err
	})
	return resp, calls, err
}

// predictWithToolsOnce performs a prediction request with tool support
func (p *ToolProvider) predictWithToolsOnce(
	ctx context.Context,
	req providers.PredictionRequest,
	tools providers.ProviderTools,
	toolChoice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	// Route to appropriate API based on mode
	if p.currentAPIMode() == APIModeResponses {
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
	if err := checkStrictResponseFormat(req.ResponseFormat); err != nil {
		return nil, err
	}
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
func (p *Provider) convertRequestMessagesToOpenAI(
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
	for i := range req.Messages {
		isToolResult := req.Messages[i].Role == roleToolResult && req.Messages[i].ToolResult != nil
		if !isToolResult {
			var err error
			if messages, err = p.appendLiftedMedia(ctx, messages, round); err != nil {
				return nil, err
			}
			round = nil
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
	return p.appendLiftedMedia(ctx, messages, round)
}

// appendLiftedMedia appends the user messages carrying the media of a run of
// tool results (liftedMediaMessages) to messages.
func (p *Provider) appendLiftedMedia(
	ctx context.Context, messages []map[string]interface{}, round []*types.MessageToolResult,
) ([]map[string]interface{}, error) {
	lifted := p.liftedMediaMessages(round)
	for i := range lifted {
		openaiMsg, err := p.convertSingleMessageForTools(ctx, lifted[i])
		if err != nil {
			return nil, fmt.Errorf("tool result media: %w", err)
		}
		messages = append(messages, openaiMsg)
	}
	return messages, nil
}

// convertSingleMessageForTools converts a single message to OpenAI format including tool metadata.
// A part that cannot be converted is an error, as on the non-tool path: falling
// back to the message's text used to send the model the question without its media.
func (p *Provider) convertSingleMessageForTools(
	ctx context.Context, msg types.Message,
) (map[string]interface{}, error) {
	// A tool message is text: its media is described by toolMessageText and
	// lifted into a user message, so it is neither loaded nor encoded here.
	if msg.Role == roleToolResult && msg.ToolResult != nil {
		openaiMsg := map[string]interface{}{
			keyRole:        msg.Role,
			keyContent:     p.toolMessageText(msg.ToolResult),
			"tool_call_id": msg.ToolResult.ID,
		}
		if msg.ToolResult.Name != "" {
			openaiMsg[keyName] = msg.ToolResult.Name
		}
		return openaiMsg, nil
	}

	// Convert message to OpenAI format (handles both legacy and multimodal)
	convertedMsg, err := p.convertMessageToOpenAI(ctx, msg)
	if err != nil {
		return nil, err
	}

	openaiMsg := map[string]interface{}{
		keyRole:    convertedMsg.Role,
		keyContent: convertedMsg.Content,
	}
	if len(msg.ToolCalls) > 0 {
		openaiMsg["tool_calls"] = p.convertToolCallsToOpenAI(msg.ToolCalls)
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
func (p *Provider) convertToolCallsToOpenAI(toolCalls []types.MessageToolCall) []map[string]interface{} {
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
// any sampling parameter the API rejects for this model, and on the Responses
// API if Chat Completions rejects the model as Responses-only.
func (p *ToolProvider) PredictStreamWithTools(
	ctx context.Context, req providers.PredictionRequest, tools any, toolChoice string,
) (<-chan providers.StreamChunk, error) {
	var ch <-chan providers.StreamChunk
	err := p.withResponsesFallback(func(v *Provider) (err error) {
		tp := &ToolProvider{Provider: v}
		ch, err = providers.RetryCall(&v.BaseProvider, req,
			func(r providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
				return tp.predictStreamWithToolsOnce(ctx, r, tools, toolChoice)
			})
		return err
	})
	return ch, err
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
	if p.currentAPIMode() == APIModeResponses {
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
