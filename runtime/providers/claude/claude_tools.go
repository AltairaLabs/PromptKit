package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

const (
	roleUser        = "user"
	roleAssistant   = "assistant"
	roleTool        = "tool"
	apiKeyHeader    = "x-api-key"
	providerNameLog = "Claude-Tools"

	// Claude content source types
	sourceTypeBase64 = "base64"
	sourceTypeURL    = "url"

	// contentTypeText is the "type" value of a Claude text content block.
	contentTypeText = "text"
	// contentTypeToolUse is the "type" value of a Claude tool_use content block.
	contentTypeToolUse = "tool_use"

	// Claude tool_choice "type" values.
	toolChoiceAuto = "auto"
	toolChoiceAny  = "any"
	toolChoiceNone = "none"

	// Shared error-wrapping formats for tool-path request building.
	errMarshalRequestFailed = "failed to marshal request: %w"
	errCreateRequestFailed  = "failed to create request: %w"

	// fieldType is the "type" discriminator key used in tool_choice objects.
	fieldType = "type"
)

// ToolProvider extends ClaudeProvider with tool support
type ToolProvider struct {
	*Provider
	// strictTools overrides the default opt-in to Anthropic's native strict
	// tool use. nil means the default (on); set from
	// additional_config.strict_tools.
	strictTools *bool
	// demotionsWarned records the sets of tools already reported as sent
	// without strict (warnDemoted). A pointer, so a WithMediaSettings view
	// shares it and copying the provider copies no lock.
	demotionsWarned *sync.Map
}

// NewToolProvider creates a new Claude provider with tool support
func NewToolProvider(
	id, model, baseURL string, defaults providers.ProviderDefaults, includeRawOutput bool,
) *ToolProvider {
	return &ToolProvider{
		Provider:        NewProvider(id, model, baseURL, defaults, includeRawOutput),
		demotionsWarned: &sync.Map{},
	}
}

// NewToolProviderWithCredential creates a Claude tool provider with explicit credential.
func NewToolProviderWithCredential(
	id, model, baseURL string, defaults providers.ProviderDefaults,
	includeRawOutput bool, cred providers.Credential,
	platform string, platformConfig *providers.PlatformConfig,
) *ToolProvider {
	return &ToolProvider{
		Provider: NewProviderWithCredential(
			id, model, baseURL, defaults, includeRawOutput, cred, platform, platformConfig,
		),
		demotionsWarned: &sync.Map{},
	}
}

// Claude-specific tool structures
type claudeTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	// Strict opts into Anthropic's native strict tool use: the API constrains
	// decoding to the schema, so tool_use.input is guaranteed to validate
	// rather than merely likely to. It is the same guarantee structured
	// outputs give the response, and it costs nothing but the schema rules —
	// which is why the schema is adapted before it goes out.
	Strict bool `json:"strict,omitempty"`
	// callerSchema is the caller's schema, sent instead of the adapted one when
	// the tool goes out without strict (fitStrictTools).
	callerSchema json.RawMessage
	// cost is what the adapted schema spends of the request's strict budget.
	cost strictCost
}

type claudeToolUse struct {
	Type         string              `json:"type"`
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Input        json.RawMessage     `json:"input"`
	CacheControl *claudeCacheControl `json:"cache_control,omitempty"`
}

type claudeToolResult struct {
	Type         string              `json:"type"`
	ToolUseID    string              `json:"tool_use_id"`
	Content      interface{}         `json:"content"` // string for text-only, []interface{} for multimodal
	CacheControl *claudeCacheControl `json:"cache_control,omitempty"`
}

type claudeToolMessage struct {
	Role    string `json:"role"`
	Content []any  `json:"content"`
}

type claudeTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
	// CacheControl marks a prompt-cache breakpoint. Per the Anthropic API,
	// cache_control is only valid on content blocks (and tools/system), never on
	// the message object itself.
	CacheControl *claudeCacheControl `json:"cache_control,omitempty"`
}

// BuildTooling converts tool descriptors to Claude format
func (p *ToolProvider) BuildTooling(descriptors []*providers.ToolDescriptor) (providers.ProviderTools, error) {
	if len(descriptors) == 0 {
		return nil, nil
	}

	strict := p.useStrictTools()

	tools := make([]claudeTool, len(descriptors))
	for i, desc := range descriptors {
		callerSchema := types.NormalizeRawMessage(desc.InputSchema)
		schema := callerSchema
		var cost strictCost
		if strict {
			// Strict tool use enforces the same schema rules structured
			// outputs do, so the same adaptation applies. The descriptor is
			// not touched: tools.SchemaValidator still validates the model's
			// arguments against what the caller actually wrote.
			schema = adaptSchemaForClaude(schema)
			cost = strictCostOf(schema)
		}
		tools[i] = claudeTool{
			Name:         desc.Name,
			Description:  desc.Description,
			InputSchema:  schema,
			Strict:       strict,
			callerSchema: callerSchema,
			cost:         cost,
		}
	}

	return tools, nil
}

// useStrictTools reports whether tool definitions opt into Anthropic's native
// strict tool use. Defaults to true; override with
// additional_config.strict_tools: false.
//
// Mirrors the OpenAI adapter's convention, including the escape hatch, because
// the guarantee and the trade are the same: arguments are constrained to the
// schema, at the price of the keywords Anthropic's grammar does not accept
// (which adaptSchemaForClaude moves into the description).
func (p *ToolProvider) useStrictTools() bool {
	if p.strictTools == nil {
		return true
	}
	return *p.strictTools
}

// PredictWithTools runs predictWithToolsOnce, retrying without any sampling
// parameter the API rejects for this model, and with the other thinking type
// if the model rejects the one sent.
func (p *ToolProvider) PredictWithTools(
	ctx context.Context, req providers.PredictionRequest, tools providers.ProviderTools, toolChoice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	var resp providers.PredictionResponse
	var calls []types.MessageToolCall
	err := p.withThinkingFallback(func() (err error) {
		resp, calls, err = providers.RetryToolCall(&p.BaseProvider, req,
			func(r providers.PredictionRequest) (providers.PredictionResponse, []types.MessageToolCall, error) {
				return p.predictWithToolsOnce(ctx, r, tools, toolChoice)
			})
		return err
	})
	return resp, calls, err
}

// predictWithToolsOnce performs a predict request with tool support
func (p *ToolProvider) predictWithToolsOnce(
	ctx context.Context,
	req providers.PredictionRequest,
	tools providers.ProviderTools,
	toolChoice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	// Track total latency including API call time
	start := time.Now()

	// Build Claude request with tools
	claudeReq, err := p.buildToolRequest(ctx, req, tools, toolChoice)
	if err != nil {
		return providers.PredictionResponse{}, nil, err
	}

	// Prepare response with raw request if configured (set early to preserve on error)
	predictResp := providers.PredictionResponse{}
	if p.ShouldIncludeRawOutput() {
		predictResp.RawRequest = claudeReq
	}

	// Make the API call
	respBytes, err := p.makeRequest(ctx, &claudeReq)
	if err != nil {
		predictResp.Latency = time.Since(start)
		return predictResp, nil, err
	}

	// Capture total latency
	latency := time.Since(start)

	// Parse response and extract tool calls
	return p.parseToolResponse(respBytes, &predictResp, latency)
}

// processClaudeToolResult converts a tool message to Claude's tool_result format.
// For text-only results, content is a plain string.
// For multimodal results (containing images, documents, etc.), content is an array of content blocks.
// Media that cannot be loaded is an error, not a block silently left out.
func (p *Provider) processClaudeToolResult(ctx context.Context, msg types.Message) (claudeToolResult, error) {
	result := claudeToolResult{
		Type:      "tool_result",
		ToolUseID: msg.ToolResult.ID,
	}

	// If the tool result has media parts, serialize as an array of content blocks
	if msg.ToolResult.HasMedia() {
		blocks, err := p.buildToolResultContentBlocks(ctx, msg.ToolResult)
		if err != nil {
			return claudeToolResult{}, fmt.Errorf("tool result %s: %w", msg.ToolResult.ID, err)
		}
		result.Content = blocks
		return result, nil
	}

	// Text-only: use plain string to avoid unnecessary array wrapping
	result.Content = msg.ToolResult.GetTextContent()
	return result, nil
}

// buildToolResultContentBlocks converts one tool result's parts to a Claude
// content block array. The parts share one MediaLoader, so the result's media
// is held to providers.MaxMediaItems and providers.MaxAggregateMediaSize.
// Images and PDFs within Claude's limits convert as they do in a user
// message; any other media is described in a text block (toolResultMediaBlock).
func (p *Provider) buildToolResultContentBlocks(
	ctx context.Context, result *types.MessageToolResult,
) ([]interface{}, error) {
	loader := p.MediaLoader()
	blocks := make([]interface{}, 0, len(result.Parts))
	for i := range result.Parts {
		part := result.Parts[i]
		switch {
		case part.Type == types.ContentTypeText:
			if part.Text != nil && *part.Text != "" {
				blocks = append(blocks, claudeTextContent{Type: contentTypeText, Text: *part.Text})
			}
		case part.Media == nil:
		default:
			block, err := p.toolResultMediaBlock(ctx, loader, result, &result.Parts[i])
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, block)
		}
	}
	return blocks, nil
}

// toolResultMediaBlock converts a tool result's media part: an image or PDF
// within Claude's limits converts as it does in a user message; anything else
// becomes a text block saying what the tool returned and why it was not sent.
func (p *Provider) toolResultMediaBlock(
	ctx context.Context, loader *providers.MediaLoader, result *types.MessageToolResult, part *types.ContentPart,
) (interface{}, error) {
	if why := providers.ToolMediaRefusal("Claude", p.SendableMultimodalCapabilities(), part); why != "" {
		return claudeTextContent{Type: contentTypeText, Text: providers.UnsentToolMedia(p.ID(), result, part, why)}, nil
	}
	if part.Type == types.ContentTypeImage {
		return p.convertImagePartToClaude(ctx, loader, *part)
	}
	return p.convertDocumentPartToClaude(ctx, loader, *part)
}

// buildClaudeMessageContent creates content array for a message including text, images, and tool calls
func (p *Provider) buildClaudeMessageContent(
	ctx context.Context,
	msg types.Message,
	pendingToolResults []claudeToolResult,
) ([]interface{}, error) {
	content := make([]interface{}, 0)

	// Add pending tool results first if this is a user message
	if msg.Role == roleUser && len(pendingToolResults) > 0 {
		for _, tr := range pendingToolResults {
			content = append(content, tr)
		}
	}

	// Add message content (text and/or media)
	blocks, err := p.buildMessageContentBlocks(ctx, msg)
	if err != nil {
		return nil, err
	}
	content = append(content, blocks...)

	// Add tool calls if this is an assistant message
	if msg.Role == roleAssistant && len(msg.ToolCalls) > 0 {
		// With extended thinking on, a tool-using assistant turn goes back
		// with its thinking blocks first, unmodified, or the API refuses it.
		if p.claudeThinkingFor() != nil {
			content = append(thinkingBlocks(msg.Reasoning), content...)
		}
		for _, toolCall := range msg.ToolCalls {
			// Normalize empty/null/truncated args to {} — a streamed tool call cut
			// off at max_tokens leaves non-empty-but-invalid JSON that would crash
			// the request marshal on replay.
			content = append(content, claudeToolUse{
				Type:  contentTypeToolUse,
				ID:    toolCall.ID,
				Name:  toolCall.Name,
				Input: types.NormalizeRawMessage(toolCall.Args),
			})
		}
	}

	return content, nil
}

// claudeThinkingBlock replays a thinking block with its signature.
type claudeThinkingBlock struct {
	Type      string `json:"type"`
	Thinking  string `json:"thinking"`
	Signature string `json:"signature"`
}

// claudeRedactedThinkingBlock replays a redacted_thinking block.
type claudeRedactedThinkingBlock struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

// thinkingBlocks rebuilds an assistant turn's thinking and redacted_thinking
// blocks, in the order they arrived, from what extractReasoning captured. The
// trace holds one text for all thinking blocks, so only the first thinking
// signature is replayed, with that text; a later one would not match the
// text it was issued for, and the API refuses modified thinking.
func thinkingBlocks(rt *types.ReasoningTrace) []any {
	if rt == nil {
		return nil
	}
	var blocks []any
	thought := false
	for _, op := range rt.Opaque {
		switch {
		case op.Provider != providerClaude || op.Data == "":
		case op.Kind == thinkingSignatureKind && !thought:
			thought = true
			blocks = append(blocks, claudeThinkingBlock{
				Type: types.ContentTypeThinking, Thinking: rt.Text, Signature: op.Data})
		case op.Kind == redactedThinking:
			blocks = append(blocks, claudeRedactedThinkingBlock{Type: redactedThinking, Data: op.Data})
		}
	}
	return blocks
}

// buildMessageContentBlocks creates content blocks for text and media parts of a message.
// This is extracted to reduce cognitive complexity of buildClaudeMessageContent.
// A part that cannot be converted is an error, as on the non-tool path:
// falling back to the message's text used to send the model the question
// without its media.
func (p *Provider) buildMessageContentBlocks(ctx context.Context, msg types.Message) ([]interface{}, error) {
	// Claude takes no audio: a spoken reply goes as its transcript.
	msg = providers.WithoutAssistantAudio(msg)
	// Check if message has multimodal content (images, etc.)
	if msg.HasMediaContent() {
		return p.convertPartsToClaudeBlocks(ctx, msg.Parts)
	}

	// Text-only path
	textContent := msg.GetContent()
	if textContent != "" {
		return []interface{}{claudeTextContent{
			Type: contentTypeText,
			Text: textContent,
		}}, nil
	}

	return nil, nil
}

// addClaudeToolConfig sets the tools and tool_choice fields on the request based
// on toolChoice.
func addClaudeToolConfig(request *claudeRequest, tools any, toolChoice string) {
	request.Tools = tools

	switch toolChoice {
	case "":
		// No forced selection: ToolChoice stays unset.
	case toolChoiceNone:
		// The tools are still sent, and the model may call none of them.
		request.ToolChoice = map[string]any{fieldType: toolChoiceNone}
	case toolChoiceAuto:
		request.ToolChoice = map[string]any{fieldType: toolChoiceAuto}
	case "required", toolChoiceAny:
		request.ToolChoice = map[string]any{fieldType: toolChoiceAny}
	default:
		request.ToolChoice = map[string]any{
			fieldType: "tool",
			"name":    toolChoice,
		}
	}
}

// flushPendingToolResults creates a user message from pending tool results
func flushPendingToolResults(pendingToolResults []claudeToolResult) claudeToolMessage {
	toolResultContent := make([]interface{}, len(pendingToolResults))
	for i, tr := range pendingToolResults {
		toolResultContent[i] = tr
	}
	return claudeToolMessage{
		Role:    "user",
		Content: toolResultContent,
	}
}

// processMessageForTools appends one non-tool message to messages, carrying
// any pending tool results: they lead a user message, or go out in a user
// message of their own before any other role.
func (p *Provider) processMessageForTools(
	ctx context.Context,
	msg types.Message,
	pendingToolResults []claudeToolResult,
	messages []claudeToolMessage,
) ([]claudeToolMessage, []claudeToolResult, error) {
	// If we have pending tool results and this is not a user message, create a user message for them
	if len(pendingToolResults) > 0 && msg.Role != roleUser {
		messages = append(messages, flushPendingToolResults(pendingToolResults))
		pendingToolResults = nil
	}

	content, err := p.buildClaudeMessageContent(ctx, msg, pendingToolResults)
	if err != nil {
		return nil, nil, err
	}
	if msg.Role == roleUser {
		pendingToolResults = nil
	}

	if len(content) == 0 {
		return messages, pendingToolResults, nil
	}

	messages = append(messages, claudeToolMessage{
		Role:    msg.Role,
		Content: content,
	})
	return messages, pendingToolResults, nil
}

// convertMessagesToClaudeFormat converts conversation messages to the Messages
// API shape. It is the one serializer for every request path, with or without
// tools: an assistant's tool calls become tool_use blocks and each tool
// message becomes a tool_result block, carrying its tool_use_id, in the user
// message that follows. It places no cache breakpoints; each path adds its own.
//
// The API refuses a tool_use without a tool_result right after it, and a
// tool_result for no tool_use before it. So a call with no result in the tool
// messages that directly follow it (held for approval, or a turn cut short)
// is left out, the assistant's text going alone; and a tool message that
// answers no such call, or has no ToolResult, is sent as the user's text.
//
// A media part that cannot be converted is an error: falling back to the
// message's text used to send the model the question without its image.
func (p *Provider) convertMessagesToClaudeFormat(
	ctx context.Context, msgs []types.Message,
) ([]claudeToolMessage, error) {
	messages := make([]claudeToolMessage, 0, len(msgs))
	var pendingToolResults []claudeToolResult
	var answered map[string]bool

	for i := range msgs {
		msg := msgs[i]
		if msg.Role == roleTool && msg.ToolResult != nil && answered[msg.ToolResult.ID] {
			result, err := p.processClaudeToolResult(ctx, msg)
			if err != nil {
				return nil, fmt.Errorf("failed to convert message %d: %w", i, err)
			}
			pendingToolResults = append(pendingToolResults, result)
			continue
		}
		answered = nil
		switch msg.Role {
		case roleTool:
			msg = types.Message{Role: roleUser, Content: msg.GetContent()}
		case roleAssistant:
			answered = answeredCalls(msgs, i)
			msg.ToolCalls = slices.DeleteFunc(slices.Clone(msg.ToolCalls), func(tc types.MessageToolCall) bool {
				return !answered[tc.ID]
			})
		}

		var err error
		messages, pendingToolResults, err = p.processMessageForTools(ctx, msg, pendingToolResults, messages)
		if err != nil {
			return nil, fmt.Errorf("failed to convert message %d: %w", i, err)
		}
	}

	// If there are still pending tool results at the end, add them as a final user message
	if len(pendingToolResults) > 0 {
		messages = append(messages, flushPendingToolResults(pendingToolResults))
	}
	return messages, nil
}

// answeredCalls returns the IDs of the assistant message msgs[i]'s tool calls
// that the tool messages directly after it answer. The run ends at the first
// message that is not a tool result for one of those calls.
func answeredCalls(msgs []types.Message, i int) map[string]bool {
	calls := make(map[string]bool, len(msgs[i].ToolCalls))
	for _, tc := range msgs[i].ToolCalls {
		calls[tc.ID] = true
	}
	answered := make(map[string]bool, len(calls))
	for j := i + 1; j < len(msgs); j++ {
		tr := msgs[j].ToolResult
		if msgs[j].Role != roleTool || tr == nil || !calls[tr.ID] {
			break
		}
		answered[tr.ID] = true
	}
	return answered
}

// markFirstUserTextCacheable caches the prefix through the first message when
// it is a user message ending in text. cache_control must go on a CONTENT
// BLOCK (the last one) — the Anthropic API rejects cache_control on a message
// object ("messages.N.cache_control: Extra inputs are not permitted").
func markFirstUserTextCacheable(messages []claudeToolMessage) {
	if len(messages) == 0 || messages[0].Role != roleUser {
		return
	}
	first := &messages[0]
	last := len(first.Content) - 1
	if tc, ok := first.Content[last].(claudeTextContent); ok {
		tc.CacheControl = &claudeCacheControl{Type: cacheTypeEphemeral}
		first.Content[last] = tc
	}
}

// markLastBlockCacheable sets a cache_control breakpoint on the final content
// block of the most recent message so Anthropic caches the whole conversation
// prefix up to that point. It walks back within the last message, past blocks
// that cannot carry cache_control (thinking), to the last text, tool_use or
// tool_result block. A text block shorter than minTextChars is left unmarked,
// for a path that caches only a long final message; tool blocks are always
// marked. Both request paths place their rolling breakpoint with it.
func markLastBlockCacheable(messages []claudeToolMessage, minTextChars int) {
	if len(messages) == 0 {
		return
	}
	last := &messages[len(messages)-1]
	bp := &claudeCacheControl{Type: cacheTypeEphemeral}
	for i := len(last.Content) - 1; i >= 0; i-- {
		switch b := last.Content[i].(type) {
		case claudeTextContent:
			if len(b.Text) >= minTextChars {
				b.CacheControl = bp
				last.Content[i] = b
			}
			return
		case claudeToolUse:
			b.CacheControl = bp
			last.Content[i] = b
			return
		case claudeToolResult:
			b.CacheControl = bp
			last.Content[i] = b
			return
		}
	}
}

func (p *ToolProvider) buildToolRequest(
	ctx context.Context, req providers.PredictionRequest, tools any, toolChoice string,
) (claudeRequest, error) {
	messages, err := p.convertMessagesToClaudeFormat(ctx, req.Messages)
	if err != nil {
		return claudeRequest{}, err
	}

	// For caching with tools: cache the prefix through the first user message,
	// and the GROWING conversation prefix. A cache_control breakpoint caches the
	// entire prefix up to it, and Anthropic auto-matches earlier cached prefixes
	// within a 20-block lookback, so marking the LAST block rolls the cache forward
	// each round: round N reads round N-1's full prefix and writes the new one.
	// (A static first-message breakpoint alone only cached the base, leaving
	// the growing tool-result history re-billed at full price every round.)
	if p.supportsCaching() && tools != nil {
		markFirstUserTextCacheable(messages)
		markLastBlockCacheable(messages, 0)
	}

	// Common fields (model, max_tokens, gated temperature, cache-aware system)
	// come from the shared base builder — same source of truth as
	// Predict/PredictStream.
	request := p.buildBaseRequest(req, messages)

	// Honor the caller's ResponseFormat here too. The tool paths used to drop
	// it, guarded on a presumed conflict between structured outputs and tool
	// use. There is none: verified live against claude-sonnet-4-6 that sending
	// tools and output_config together returns 200, the model calls tools
	// normally on tool-using rounds, and the final round returns
	// schema-conforming JSON (issue #1848).
	//
	// Dropping it silently was the real problem — a caller could not tell
	// "constraint applied" from "constraint discarded", and the difference
	// depended on whether a given round happened to carry tools.
	request.OutputConfig = outputConfigFor(req.ResponseFormat)

	if tools != nil {
		addClaudeToolConfig(&request, p.fitStrictTools(tools, request.OutputConfig), toolChoice)
	}

	return request, nil
}

// extractTextContentFromResponse extracts text content from Claude response
func extractTextContentFromResponse(content []claudeContent) string {
	for _, c := range content {
		if c.Type == contentTypeText {
			return c.Text
		}
	}
	return ""
}

// extractContentParts converts Claude content blocks to typed ContentParts.
// Returns text parts only; reasoning is extracted separately via extractReasoning
// (onto Message.Reasoning, off Parts), and tool_use blocks are handled separately.
func extractContentParts(content []claudeContent) []types.ContentPart {
	var parts []types.ContentPart
	for _, c := range content {
		if c.Type == contentTypeText {
			parts = append(parts, types.NewTextPart(c.Text))
		}
	}
	return parts
}

// Reasoning-block constants shared by the non-streaming (extractReasoning) and
// streaming (signature_delta) capture paths.
const (
	providerClaude        = "claude"
	thinkingSignatureKind = "thinking_signature"
	redactedThinking      = "redacted_thinking"
)

// extractReasoning gathers Claude reasoning blocks into a ReasoningTrace: thinking
// text (with its signature as an opaque round-trip token) and redacted_thinking
// (opaque, marked Redacted). Returns nil when the response carries no reasoning.
func extractReasoning(content []claudeContent) *types.ReasoningTrace {
	var rt types.ReasoningTrace
	for _, c := range content {
		switch c.Type {
		case types.ContentTypeThinking:
			rt.Text += c.Thinking
			if c.Signature != "" {
				rt.Opaque = append(rt.Opaque, types.OpaqueReasoning{
					Provider: providerClaude, Kind: thinkingSignatureKind, Data: c.Signature,
				})
			}
		case redactedThinking:
			rt.Redacted = true
			rt.Opaque = append(rt.Opaque, types.OpaqueReasoning{
				Provider: providerClaude, Kind: redactedThinking, Data: c.Data,
			})
		}
	}
	if rt.Text == "" && len(rt.Opaque) == 0 {
		return nil
	}
	return &rt
}

// parseToolCallsFromRawResponse extracts tool calls from raw JSON response
func parseToolCallsFromRawResponse(respBytes []byte) []types.MessageToolCall {
	var toolCalls []types.MessageToolCall
	var rawResp map[string]interface{}

	if err := json.Unmarshal(respBytes, &rawResp); err != nil {
		return toolCalls
	}

	content, ok := rawResp["content"].([]interface{})
	if !ok {
		return toolCalls
	}

	for _, item := range content {
		itemMap, ok := item.(map[string]interface{})
		if !ok || itemMap["type"] != contentTypeToolUse {
			continue
		}

		toolCall := types.MessageToolCall{
			ID:   itemMap["id"].(string),
			Name: itemMap["name"].(string),
		}

		if input, ok := itemMap["input"]; ok {
			// Marshal only errors on unsupported types, impossible with map[string]interface{}
			inputBytes, _ := json.Marshal(input) // NOSONAR
			toolCall.Args = json.RawMessage(inputBytes)
		}

		toolCalls = append(toolCalls, toolCall)
	}

	return toolCalls
}

func (p *ToolProvider) parseToolResponse(
	respBytes []byte,
	predictResp *providers.PredictionResponse,
	latency time.Duration,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	var resp claudeResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		predictResp.Latency = latency
		predictResp.Raw = respBytes
		return *predictResp, nil, fmt.Errorf("failed to parse Claude response: %w", err)
	}

	if resp.Error != nil {
		predictResp.Latency = latency
		predictResp.Raw = respBytes
		return *predictResp, nil, fmt.Errorf("claude API error: %s", resp.Error.Message)
	}

	if len(resp.Content) == 0 {
		predictResp.Latency = latency
		predictResp.Raw = respBytes
		return *predictResp, nil, fmt.Errorf("no content in Claude response")
	}

	// Extract text content
	textContent := extractTextContentFromResponse(resp.Content)

	// Parse tool calls from raw response
	toolCalls := parseToolCallsFromRawResponse(respBytes)

	// Calculate cost breakdown. resp.Usage already carries
	// CacheCreationInputTokens (cache WRITE) alongside read/input/output, so
	// route the full wire usage through costFromUsage directly rather than
	// the CalculateCost wrapper (which only accepts cache reads).
	costBreakdown := p.costFromUsage(resp.Usage)

	predictResp.Content = textContent
	predictResp.Parts = extractContentParts(resp.Content)
	predictResp.Reasoning = extractReasoning(resp.Content)
	predictResp.CostInfo = &costBreakdown
	predictResp.Latency = latency
	predictResp.Raw = respBytes
	predictResp.ToolCalls = toolCalls
	// Normalize + surface the stop reason (tool_use / end_turn / max_tokens /
	// refusal); the non-streaming and streaming paths already do this — the tool
	// path was the only one dropping it, emitting an empty FinishReason.
	predictResp.FinishReason = normalizeFinishReason(resp.StopReason)

	return *predictResp, toolCalls, nil
}

// applyToolRequestHeaders sets content-type, anthropic auth, and custom
// headers on req. Credential-based auth (Bedrock SigV4 / Vertex Bearer /
// Azure Bearer) goes via the resolved Credential; the direct API uses
// x-api-key + anthropic-version headers. Azure pins the API contract via
// the api-version query parameter so it does not need the
// anthropic-version header. Centralizing this keeps the caller functions
// below cognitive-complexity limits and ensures every request path goes
// through the same custom-header collision check.
func (p *ToolProvider) applyToolRequestHeaders(ctx context.Context, req *http.Request) error {
	req.Header.Set(contentTypeHeader, applicationJSON)
	if p.usesCredentialAuth() {
		if err := p.AuthorizeRequest(ctx, req); err != nil {
			return fmt.Errorf("failed to apply authentication: %w", err)
		}
	} else {
		req.Header.Set(apiKeyHeader, p.apiKey)
		req.Header.Set(anthropicVersionKey, anthropicVersionValue)
	}
	return p.ApplyCustomHeaders(req)
}

func (p *ToolProvider) makeRequest(ctx context.Context, request *claudeRequest) ([]byte, error) {
	url := p.messagesURL()

	// Partner-hosted (Bedrock/Vertex): inject anthropic_version into the request
	// body and drop the model field — both put the model in the URL path. The
	// shared partner marshaler also drops the (absent here) stream field; the
	// tools and tool_choice fields the builder set pass through unchanged.
	var reqBytes []byte
	var err error
	if p.isPartnerHosted() {
		reqBytes, err = p.marshalPartnerRequest(request)
	} else {
		reqBytes, err = json.Marshal(request)
	}
	if err != nil {
		return nil, fmt.Errorf(errMarshalRequestFailed, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf(errCreateRequestFailed, err)
	}

	if hdrErr := p.applyToolRequestHeaders(ctx, httpReq); hdrErr != nil {
		return nil, hdrErr
	}

	resp, err := p.GetHTTPClient().Do(httpReq)
	if err != nil {
		return nil, &providers.ProviderTransportError{Cause: err, Provider: p.ID()}
	}
	defer resp.Body.Close()

	respBody, err := providers.ReadResponseBody(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		switch {
		case p.isBedrock():
			return nil, parseBedrockHTTPError(resp.StatusCode, respBody)
		case p.isVertex(), p.isAzure():
			return nil, providers.ParsePlatformHTTPError(p.platform, resp.StatusCode, respBody)
		default:
			return nil, &providers.ProviderHTTPError{
				StatusCode: resp.StatusCode, URL: url,
				Body: string(respBody), Provider: p.ID(),
			}
		}
	}

	// Check for Bedrock body errors (e.g. UnknownOperationException on HTTP 200)
	if bodyErr := checkBedrockBodyError(respBody); bodyErr != nil {
		return nil, bodyErr
	}

	return respBody, nil
}

// PredictStreamWithTools runs predictStreamWithToolsOnce, retrying without
// any sampling parameter the API rejects for this model, and with the other
// thinking type if the model rejects the one sent.
func (p *ToolProvider) PredictStreamWithTools(
	ctx context.Context, req providers.PredictionRequest, tools any, toolChoice string,
) (<-chan providers.StreamChunk, error) {
	var ch <-chan providers.StreamChunk
	err := p.withThinkingFallback(func() (err error) {
		ch, err = providers.RetryCall(&p.BaseProvider, req,
			func(r providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
				return p.predictStreamWithToolsOnce(ctx, r, tools, toolChoice)
			})
		return err
	})
	return ch, err
}

// predictStreamWithToolsOnce performs a streaming predict request with tool support.
func (p *ToolProvider) predictStreamWithToolsOnce(
	ctx context.Context,
	req providers.PredictionRequest,
	tools interface{},
	toolChoice string,
) (<-chan providers.StreamChunk, error) {
	// Build Claude request with tools
	claudeReq, err := p.buildToolRequest(ctx, req, tools, toolChoice)
	if err != nil {
		return nil, err
	}

	// Add streaming flag
	claudeReq.Stream = true

	// Bedrock: binary event-stream format.
	if p.isBedrock() {
		return p.streamBedrockToolRequest(ctx, &claudeReq)
	}

	// Vertex: SSE via :streamRawPredict with the partner body shape.
	if p.isVertex() {
		return p.streamVertexToolRequest(ctx, &claudeReq)
	}

	return p.streamDirectToolRequest(ctx, &claudeReq)
}

// streamVertexToolRequest handles the Vertex AI Anthropic-partner
// tool-calling streaming path. URL is :streamRawPredict; body uses the
// shared partner shape (no model field, anthropic_version=vertex-2023-10-16,
// no stream flag); auth via the GCP credential. The response is plain SSE
// like the direct API, so the SSE helper is reused.
func (p *ToolProvider) streamVertexToolRequest(
	ctx context.Context,
	claudeReq *claudeRequest,
) (<-chan providers.StreamChunk, error) {
	reqBody, err := p.marshalPartnerRequest(claudeReq)
	if err != nil {
		return nil, fmt.Errorf(errMarshalRequestFailed, err)
	}
	return p.runSSEToolStream(ctx, reqBody)
}

// runSSEToolStream is the shared SSE streaming engine for tool-calling
// requests. Both the direct Anthropic API and the Vertex Anthropic-partner
// path call into this once they have produced a serialized body — they
// differ only in body encoding (direct: full claudeReq as-is; vertex: the
// partner-shape body with anthropic_version+no model+no stream). URL,
// header set, and SSE scanner are identical.
func (p *ToolProvider) runSSEToolStream(
	ctx context.Context, reqBody []byte,
) (<-chan providers.StreamChunk, error) {
	url := p.messagesStreamURL()
	requestFn := p.buildDirectStreamingRequestFn(url, reqBody)
	return p.RunStreamingRequest(ctx, &providers.StreamRetryRequest{
		Policy:       p.StreamRetryPolicy(),
		Budget:       p.StreamRetryBudget(),
		ProviderName: p.ID(),
		Host:         providers.HostFromURL(url),
		IdleTimeout:  p.StreamIdleTimeout(),
		RequestFn:    requestFn,
		Client:       p.GetStreamingHTTPClient(),
	}, func(ctx context.Context, body io.ReadCloser, outChan chan<- providers.StreamChunk) {
		idleBody := providers.NewIdleTimeoutReader(body, p.StreamIdleTimeout())
		scanner := providers.NewSSEScanner(idleBody)
		p.streamResponse(ctx, idleBody, scanner, outChan)
	})
}

// streamBedrockToolRequest handles the Bedrock tool-calling streaming path.
// Extracted from PredictStreamWithTools to keep that function under the
// cognitive-complexity threshold.
func (p *ToolProvider) streamBedrockToolRequest(
	ctx context.Context,
	claudeReq *claudeRequest,
) (<-chan providers.StreamChunk, error) {
	reqBody, err := p.marshalPartnerRequest(claudeReq)
	if err != nil {
		return nil, fmt.Errorf(errMarshalRequestFailed, err)
	}
	url := p.messagesStreamURL()
	requestFn := p.buildBedrockStreamingRequestFn(url, reqBody)
	return p.RunStreamingRequest(ctx, &providers.StreamRetryRequest{
		Policy:        p.StreamRetryPolicy(),
		Budget:        p.StreamRetryBudget(),
		ProviderName:  p.ID(),
		Host:          providers.HostFromURL(url),
		IdleTimeout:   p.StreamIdleTimeout(),
		RequestFn:     requestFn,
		Client:        p.GetStreamingHTTPClient(),
		FrameDetector: providers.BedrockEventStreamFrameDetector{},
	}, func(ctx context.Context, body io.ReadCloser, outChan chan<- providers.StreamChunk) {
		idleBody := providers.NewIdleTimeoutReader(body, p.StreamIdleTimeout())
		scanner := providers.NewBedrockEventScanner(idleBody)
		p.streamResponse(ctx, idleBody, scanner, outChan)
	})
}

// streamDirectToolRequest handles the direct Anthropic API tool-calling
// streaming path. Body is the full claudeReq (model, stream:true and all)
// serialized as-is; auth is x-api-key via applyToolRequestHeaders.
func (p *ToolProvider) streamDirectToolRequest(
	ctx context.Context,
	claudeReq *claudeRequest,
) (<-chan providers.StreamChunk, error) {
	requestBytes, err := json.Marshal(claudeReq)
	if err != nil {
		return nil, fmt.Errorf(errMarshalRequestFailed, err)
	}
	return p.runSSEToolStream(ctx, requestBytes)
}

// buildBedrockStreamingRequestFn returns a RequestFn for the Bedrock
// tool-calling streaming path. Headers (content type, SigV4 auth, custom
// headers) are applied via applyToolRequestHeaders to share the same
// centralized header logic as the non-streaming path.
func (p *ToolProvider) buildBedrockStreamingRequestFn(
	url string, reqBody []byte,
) func(context.Context) (*http.Request, error) {
	return func(ctx context.Context) (*http.Request, error) {
		httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
		if err != nil {
			return nil, fmt.Errorf(errCreateRequestFailed, err)
		}
		httpReq.Header.Set("Accept", "application/vnd.amazon.eventstream")
		if err := p.applyToolRequestHeaders(ctx, httpReq); err != nil {
			return nil, err
		}
		return httpReq, nil
	}
}

// buildDirectStreamingRequestFn returns a RequestFn for the direct Anthropic
// API tool-calling streaming path.
func (p *ToolProvider) buildDirectStreamingRequestFn(
	url string, reqBody []byte,
) func(context.Context) (*http.Request, error) {
	return func(ctx context.Context) (*http.Request, error) {
		httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
		if err != nil {
			return nil, fmt.Errorf(errCreateRequestFailed, err)
		}
		httpReq.Header.Set("Accept", "text/event-stream")
		if err := p.applyToolRequestHeaders(ctx, httpReq); err != nil {
			return nil, err
		}
		return httpReq, nil
	}
}

// thinkingModeBudget and thinkingModeAdaptive are the values of
// additional_config.thinking_mode.
const (
	thinkingModeBudget   = "budget"
	thinkingModeAdaptive = "adaptive"
)

// applyThinkingModeConfig reads additional_config.thinking_mode, the thinking
// wire type the model takes: "adaptive", or "budget" (enabled + budget_tokens).
// Omitted, the type is guessed from the model name and corrected from the API.
func applyThinkingModeConfig(p *Provider, spec providers.ProviderSpec) error {
	v, ok := spec.AdditionalConfig["thinking_mode"]
	if !ok {
		return nil
	}
	switch v {
	case thinkingModeBudget:
		p.thinkingMode = thinkingTypeEnabled
	case thinkingModeAdaptive:
		p.thinkingMode = thinkingTypeAdaptive
	default:
		return fmt.Errorf("claude: additional_config.thinking_mode must be %q or %q, got %q",
			thinkingModeAdaptive, thinkingModeBudget, fmt.Sprint(v))
	}
	return nil
}

// applyThinkingConfig enables Claude extended thinking from additional_config:
//   - thinking_budget (int): reasoning-token budget (>= 1024). Sets the request's
//     thinking block; omit to leave thinking off.
func applyThinkingConfig(p *Provider, spec providers.ProviderSpec) {
	if spec.AdditionalConfig == nil {
		return
	}
	v, ok := spec.AdditionalConfig["thinking_budget"]
	if !ok {
		return
	}
	var budget int
	switch n := v.(type) {
	case int:
		budget = n
	case int64:
		budget = int(n)
	case float64:
		budget = int(n)
	default:
		return
	}
	p.thinkingBudget = &budget
}

// applyStrictToolsConfig reads additional_config.strict_tools, the escape
// hatch from Anthropic's native strict tool use. Absent or non-boolean leaves
// the default (on) in place.
func applyStrictToolsConfig(tp *ToolProvider, spec providers.ProviderSpec) {
	if spec.AdditionalConfig == nil {
		return
	}
	v, ok := spec.AdditionalConfig["strict_tools"].(bool)
	if !ok {
		return
	}
	tp.strictTools = &v
}

// applySpecConfig applies the provider spec's settings to a newly built tp.
func applySpecConfig(tp *ToolProvider, spec providers.ProviderSpec) (providers.Provider, error) {
	tp.SetUnsupportedParams(spec.UnsupportedParams)
	tp.setCapabilities(spec.Capabilities)
	if err := applyThinkingModeConfig(tp.Provider, spec); err != nil {
		return nil, err
	}
	applyThinkingConfig(tp.Provider, spec)
	applyStrictToolsConfig(tp, spec)
	applyContextWindowConfig(tp.Provider, spec)
	return tp, nil
}

//nolint:gochecknoinits // Factory registration requires init
func init() {
	factory := providers.CredentialFactory(
		func(spec providers.ProviderSpec) (providers.Provider, error) {
			tp := NewToolProviderWithCredential(
				spec.ID, spec.Model, spec.BaseURL, spec.Defaults,
				spec.IncludeRawOutput, spec.Credential,
				spec.Platform, spec.PlatformConfig,
			)
			return applySpecConfig(tp, spec)
		},
		func(spec providers.ProviderSpec) (providers.Provider, error) {
			tp := NewToolProvider(
				spec.ID, spec.Model, spec.BaseURL, spec.Defaults, spec.IncludeRawOutput,
			)
			return applySpecConfig(tp, spec)
		},
	)
	providers.RegisterProviderFactory("claude", func(spec providers.ProviderSpec) (providers.Provider, error) {
		if err := validateDefaults(spec.Defaults); err != nil {
			return nil, err
		}
		return factory(spec)
	})
}

// WithMediaSettings returns a view of p, and of the Provider it wraps, that
// uses m's media settings (providers.MediaScoped).
func (p *ToolProvider) WithMediaSettings(m providers.MediaSettings) providers.Provider {
	view := *p
	view.Provider = p.Provider.WithMediaSettings(m).(*Provider)
	return &view
}
