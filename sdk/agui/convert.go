// Package agui serves PromptKit conversations over the AG-UI protocol (1.0),
// using the AG-UI community Go SDK's types. It provides bidirectional
// converters between PromptKit and AG-UI messages and tools, and an
// EventAdapter that turns one conversation turn into one AG-UI run.
//
// Not yet produced: token-by-token text streaming (each message's text
// arrives in one TEXT_MESSAGE_CONTENT), reasoning events, STATE_DELTA,
// MESSAGES_SNAPSHOT, and the pendingToolCallIds of a run that leaves frontend
// tool calls unanswered. Message ids are minted per conversion, so converting
// the same message twice gives two ids.
package agui

import (
	"encoding/json"
	"strings"

	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// PromptKit role strings.
const (
	roleAssistant = "assistant"
	roleTool      = "tool"
	roleUser      = "user"
	roleSystem    = "system"
)

// toolModeClient routes a tool to the conversation's client executor, which
// suspends the turn until the application supplies the result.
const toolModeClient = "client"

// toolFailedPrefix matches the text the runtime gives the model for a failed
// tool, so an error reported by the application reads the same way.
const toolFailedPrefix = "Tool execution failed: "

// AG-UI part source types. "file" is a provider file handle; the community Go
// SDK names the other two.
const (
	sourceTypeData = aguitypes.InputContentSourceTypeData
	sourceTypeURL  = aguitypes.InputContentSourceTypeURL
	sourceTypeFile = "file"
)

// roleToAGUI maps PromptKit role strings to AG-UI Role constants.
var roleToAGUI = map[string]aguitypes.Role{
	roleUser:      aguitypes.RoleUser,
	roleAssistant: aguitypes.RoleAssistant,
	roleTool:      aguitypes.RoleTool,
	roleSystem:    aguitypes.RoleSystem,
}

// roleFromAGUI maps AG-UI Role constants to PromptKit role strings.
var roleFromAGUI = map[aguitypes.Role]string{
	aguitypes.RoleUser:      roleUser,
	aguitypes.RoleAssistant: roleAssistant,
	aguitypes.RoleTool:      roleTool,
	aguitypes.RoleSystem:    roleSystem,
	aguitypes.RoleDeveloper: roleSystem,
}

// mediaPartTypes maps the AG-UI media part types to PromptKit content types.
var mediaPartTypes = map[string]string{
	aguitypes.InputContentTypeImage:    types.ContentTypeImage,
	aguitypes.InputContentTypeAudio:    types.ContentTypeAudio,
	aguitypes.InputContentTypeVideo:    types.ContentTypeVideo,
	aguitypes.InputContentTypeDocument: types.ContentTypeDocument,
}

// idGen is the default ID generator used for creating message IDs.
var idGen = events.NewDefaultIDGenerator()

// MessageToAGUI converts a PromptKit Message to an AG-UI Message.
// It maps roles, content, tool calls, and tool results.
//
// User and tool messages carry content parts (text, image, audio, video,
// document) when they hold media; assistant and system content is text, as
// AG-UI 1.0 defines it. A media part whose bytes are neither inline nor at a
// URL (a local file path or a storage reference) has no AG-UI form and is
// dropped with a warning.
func MessageToAGUI(msg *types.Message) aguitypes.Message {
	aguiMsg := aguitypes.Message{
		ID:   idGen.GenerateMessageID(),
		Role: mapRoleToAGUI(msg.Role),
	}

	// Handle tool result messages
	if msg.Role == roleTool && msg.ToolResult != nil {
		aguiMsg.ToolCallID = msg.ToolResult.ID
		aguiMsg.Error = msg.ToolResult.Error
		aguiMsg.Content = toolContentToAGUI(msg.ToolResult)
		return aguiMsg
	}

	if msg.Role == roleUser && hasMedia(msg.Parts) {
		aguiMsg.Content = partsToAGUI(msg.Parts)
	} else {
		aguiMsg.Content = msg.GetContent()
	}

	// Map tool calls
	if len(msg.ToolCalls) > 0 {
		aguiMsg.ToolCalls = toolCallsToAGUI(msg.ToolCalls)
	}

	return aguiMsg
}

// toolContentToAGUI is a tool message's content: its parts when the result
// holds media, otherwise its text.
func toolContentToAGUI(r *types.MessageToolResult) any {
	if r.HasMedia() {
		return partsToAGUI(r.Parts)
	}
	return r.GetTextContent()
}

func hasMedia(parts []types.ContentPart) bool {
	for _, p := range parts {
		if p.Type != types.ContentTypeText {
			return true
		}
	}
	return false
}

// MessageFromAGUI converts an AG-UI Message to a PromptKit Message.
// It maps roles, content (text or multimodal), tool calls, and tool call IDs.
//
// A reasoning message becomes an assistant message whose Reasoning holds the
// text, never user input. [MessagesFromAGUI] folds it into the assistant
// message that follows it instead.
func MessageFromAGUI(msg *aguitypes.Message) types.Message {
	if msg.Role == aguitypes.RoleReasoning {
		return types.Message{Role: roleAssistant, Reasoning: &types.ReasoningTrace{Text: contentString(msg)}}
	}

	pkMsg := types.Message{
		Role: mapRoleFromAGUI(msg.Role),
	}

	// Handle tool result messages
	if msg.Role == aguitypes.RoleTool {
		result := toolResultFromAGUI(msg)
		pkMsg.Content = result.GetTextContent()
		pkMsg.ToolResult = &result
		return pkMsg
	}

	// Handle multimodal content
	if inputContents := contentInputContents(msg); inputContents != nil {
		pkMsg.Parts = partsFromAGUI(inputContents)
	} else {
		pkMsg.Content = contentString(msg)
	}

	// Map tool calls
	if len(msg.ToolCalls) > 0 {
		pkMsg.ToolCalls = toolCallsFromAGUI(msg.ToolCalls)
	}

	return pkMsg
}

// toolResultFromAGUI converts an AG-UI tool message to a PromptKit tool
// result. An error with no content becomes the text the runtime gives the
// model for a failed tool, so the model still sees why the call failed.
func toolResultFromAGUI(msg *aguitypes.Message) types.MessageToolResult {
	var result types.MessageToolResult
	if contents := contentInputContents(msg); contents != nil {
		result = types.MessageToolResult{ID: msg.ToolCallID, Parts: partsFromAGUI(contents)}
	} else {
		result = types.NewTextToolResult(msg.ToolCallID, "", contentString(msg))
	}
	if msg.Error != "" {
		result.Error = msg.Error
		if result.GetTextContent() == "" && !result.HasMedia() {
			result = types.NewTextToolResult(msg.ToolCallID, "", toolFailedPrefix+msg.Error)
			result.Error = msg.Error
		}
	}
	return result
}

// MessagesToAGUI converts a slice of PromptKit Messages to AG-UI Messages.
func MessagesToAGUI(msgs []types.Message) []aguitypes.Message {
	result := make([]aguitypes.Message, len(msgs))
	for i := range msgs {
		result[i] = MessageToAGUI(&msgs[i])
	}
	return result
}

// MessagesFromAGUI converts a slice of AG-UI Messages to PromptKit Messages.
//
// A reasoning message is attached to the assistant message that follows it,
// as that message's Reasoning; with no assistant message after it, it is
// dropped. Activity messages are UI material that AG-UI says never travels
// back to the agent, and are dropped too.
func MessagesFromAGUI(msgs []aguitypes.Message) []types.Message {
	result := make([]types.Message, 0, len(msgs))
	var reasoning []string
	for i := range msgs {
		if msgs[i].Role == aguitypes.RoleReasoning {
			reasoning = append(reasoning, contentString(&msgs[i]))
			continue
		}
		if msgs[i].Role == aguitypes.RoleActivity {
			continue
		}
		pkMsg := MessageFromAGUI(&msgs[i])
		if pkMsg.Role == roleAssistant && len(reasoning) > 0 {
			pkMsg.Reasoning = &types.ReasoningTrace{Text: strings.Join(reasoning, "\n\n")}
			reasoning = nil
		}
		result = append(result, pkMsg)
	}
	return result
}

// ToolResultsFromAGUI returns the answers a RunAgentInput carries for the
// client tool calls a previous run left pending: the tool messages at the end
// of msgs, after the last message of any other role. Pass them to
// [EventAdapter.RunResume]. It returns nil when msgs does not end with a tool
// message.
//
// The trailing tool messages can include results the agent itself produced in
// that round (a server tool's TOOL_CALL_RESULT the application kept in its
// history). An sdk.Conversation resumes with an answer only for a call it has
// no result for yet, so passing them along is harmless.
//
// A tool message's content becomes the Result: text that is a JSON document
// is passed through as JSON, other text as a string, and content parts as
// []types.ContentPart. Its error becomes Error.
func ToolResultsFromAGUI(msgs []aguitypes.Message) []ToolResult {
	start := len(msgs)
	for start > 0 && msgs[start-1].Role == aguitypes.RoleTool {
		start--
	}
	if start == len(msgs) {
		return nil
	}
	results := make([]ToolResult, 0, len(msgs)-start)
	for i := start; i < len(msgs); i++ {
		m := &msgs[i]
		results = append(results, ToolResult{CallID: m.ToolCallID, Result: toolResultValue(m), Error: m.Error})
	}
	return results
}

func toolResultValue(msg *aguitypes.Message) any {
	if contents := contentInputContents(msg); contents != nil {
		return partsFromAGUI(contents)
	}
	text := contentString(msg)
	if text == "" {
		return nil
	}
	if json.Valid([]byte(text)) {
		return json.RawMessage(text)
	}
	return text
}

// ToolsToAGUI converts a slice of PromptKit ToolDescriptors to AG-UI Tool definitions.
func ToolsToAGUI(descs []tools.ToolDescriptor) []aguitypes.Tool {
	if len(descs) == 0 {
		return nil
	}
	result := make([]aguitypes.Tool, len(descs))
	for i := range descs {
		var params any
		if len(descs[i].InputSchema) > 0 {
			var m map[string]any
			if err := json.Unmarshal(descs[i].InputSchema, &m); err == nil {
				params = m
			}
		}
		result[i] = aguitypes.Tool{
			Name:        descs[i].Name,
			Description: descs[i].Description,
			Parameters:  params,
		}
	}
	return result
}

// ToolsFromAGUI converts a slice of AG-UI Tool definitions to PromptKit ToolDescriptors.
// Each tool's Parameters (JSON Schema as any) is marshaled to json.RawMessage for InputSchema.
//
// The tools in RunAgentInput.tools are the application's own, executed by the
// application, so each descriptor has Mode "client": a call to one suspends
// the turn until the application answers it.
func ToolsFromAGUI(aguiTools []aguitypes.Tool) []*tools.ToolDescriptor {
	result := make([]*tools.ToolDescriptor, len(aguiTools))
	for i, t := range aguiTools {
		result[i] = toolFromAGUI(t)
	}
	return result
}

// mapRoleToAGUI converts a PromptKit role string to an AG-UI Role constant.
// Falls back to RoleUser for unrecognized roles.
func mapRoleToAGUI(role string) aguitypes.Role {
	if r, ok := roleToAGUI[role]; ok {
		return r
	}
	return aguitypes.RoleUser
}

// mapRoleFromAGUI converts an AG-UI Role constant to a PromptKit role string.
// Falls back to "user" for unrecognized roles.
func mapRoleFromAGUI(role aguitypes.Role) string {
	if r, ok := roleFromAGUI[role]; ok {
		return r
	}
	return roleUser
}

// partsToAGUI converts PromptKit ContentParts to AG-UI content parts.
func partsToAGUI(parts []types.ContentPart) []aguitypes.InputContent {
	result := make([]aguitypes.InputContent, 0, len(parts))
	for _, part := range parts {
		if ic, ok := contentPartToAGUI(part); ok {
			result = append(result, ic)
		}
	}
	return result
}

// contentPartToAGUI converts a single PromptKit ContentPart to an AG-UI part.
// Returns false if the part has no AG-UI form.
func contentPartToAGUI(part types.ContentPart) (aguitypes.InputContent, bool) {
	if part.Type == types.ContentTypeText {
		if part.Text == nil {
			return aguitypes.InputContent{}, false
		}
		return aguitypes.InputContent{Type: aguitypes.InputContentTypeText, Text: *part.Text}, true
	}
	partType, ok := aguiPartType(part.Type)
	if !ok || part.Media == nil {
		return aguitypes.InputContent{}, false
	}
	source, ok := mediaSourceToAGUI(part.Media)
	if !ok {
		logger.Warn("AG-UI: dropping media part with no inline data or URL",
			"type", part.Type, "mime_type", part.Media.MIMEType)
		return aguitypes.InputContent{}, false
	}
	return aguitypes.InputContent{Type: partType, Source: source}, true
}

func aguiPartType(contentType string) (string, bool) {
	for aguiType, pkType := range mediaPartTypes {
		if pkType == contentType {
			return aguiType, true
		}
	}
	return "", false
}

// mediaSourceToAGUI picks the AG-UI source for media: inline data, else URL.
func mediaSourceToAGUI(media *types.MediaContent) (*aguitypes.InputContentSource, bool) {
	switch {
	case media.Data != nil && *media.Data != "":
		return &aguitypes.InputContentSource{Type: sourceTypeData, Value: *media.Data, MimeType: media.MIMEType}, true
	case media.URL != nil && *media.URL != "":
		return &aguitypes.InputContentSource{Type: sourceTypeURL, Value: *media.URL, MimeType: media.MIMEType}, true
	default:
		return nil, false
	}
}

// partsFromAGUI converts AG-UI content parts to PromptKit ContentParts. A part
// PromptKit cannot use (a provider file handle, an unknown type, a part with
// no bytes) is skipped with a warning, as AG-UI requires of a producer.
func partsFromAGUI(contents []aguitypes.InputContent) []types.ContentPart {
	result := make([]types.ContentPart, 0, len(contents))
	for i := range contents {
		ic := &contents[i]
		if ic.Type == aguitypes.InputContentTypeText {
			text := ic.Text
			result = append(result, types.ContentPart{Type: types.ContentTypeText, Text: &text})
			continue
		}
		if part, ok := mediaPartFromAGUI(ic); ok {
			result = append(result, part)
			continue
		}
		logger.Warn("AG-UI: skipping content part PromptKit cannot use", "type", ic.Type)
	}
	return result
}

// mediaPartFromAGUI converts an AG-UI media part: a 1.0 typed part with a
// source, or a legacy binary part, whose content type follows its MIME type.
func mediaPartFromAGUI(ic *aguitypes.InputContent) (types.ContentPart, bool) {
	if ic.Type == aguitypes.InputContentTypeBinary {
		return binaryInputContentToPart(ic)
	}
	contentType, ok := mediaPartTypes[ic.Type]
	if !ok || ic.Source == nil {
		return types.ContentPart{}, false
	}
	media := &types.MediaContent{MIMEType: ic.Source.MimeType}
	value := ic.Source.Value
	switch ic.Source.Type {
	case sourceTypeData:
		media.Data = &value
	case sourceTypeURL:
		media.URL = &value
	default: // a provider file handle, or a source type AG-UI added later
		return types.ContentPart{}, false
	}
	return types.ContentPart{Type: contentType, Media: media}, true
}

// binaryInputContentToPart converts a legacy AG-UI binary part, choosing the
// content type from its MIME type. A part with only an id has no bytes to use.
func binaryInputContentToPart(ic *aguitypes.InputContent) (types.ContentPart, bool) {
	media := &types.MediaContent{MIMEType: ic.MimeType}
	if ic.URL != "" {
		url := ic.URL
		media.URL = &url
	}
	if ic.Data != "" {
		data := ic.Data
		media.Data = &data
	}
	if media.URL == nil && media.Data == nil {
		return types.ContentPart{}, false
	}
	return types.ContentPart{Type: contentTypeForMIME(ic.MimeType), Media: media}, true
}

func contentTypeForMIME(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return types.ContentTypeImage
	case strings.HasPrefix(mime, "audio/"):
		return types.ContentTypeAudio
	case strings.HasPrefix(mime, "video/"):
		return types.ContentTypeVideo
	default:
		return types.ContentTypeDocument
	}
}

// toolCallsToAGUI converts PromptKit MessageToolCalls to AG-UI ToolCalls.
func toolCallsToAGUI(tcs []types.MessageToolCall) []aguitypes.ToolCall {
	result := make([]aguitypes.ToolCall, len(tcs))
	for i, tc := range tcs {
		result[i] = aguitypes.ToolCall{
			ID:   tc.ID,
			Type: string(aguitypes.ToolCallTypeFunction),
			Function: aguitypes.FunctionCall{
				Name:      tc.Name,
				Arguments: string(tc.Args),
			},
		}
	}
	return result
}

// toolCallsFromAGUI converts AG-UI ToolCalls to PromptKit MessageToolCalls.
func toolCallsFromAGUI(tcs []aguitypes.ToolCall) []types.MessageToolCall {
	result := make([]types.MessageToolCall, len(tcs))
	for i, tc := range tcs {
		result[i] = types.MessageToolCall{
			ID:   tc.ID,
			Name: tc.Function.Name,
			Args: json.RawMessage(tc.Function.Arguments),
		}
	}
	return result
}

// toolFromAGUI converts a single AG-UI Tool to a PromptKit ToolDescriptor.
func toolFromAGUI(t aguitypes.Tool) *tools.ToolDescriptor {
	td := &tools.ToolDescriptor{
		Name:        t.Name,
		Description: t.Description,
		Mode:        toolModeClient,
	}

	if t.Parameters != nil {
		if data, err := json.Marshal(t.Parameters); err == nil {
			td.InputSchema = data
		}
	}

	return td
}

// contentString extracts the text content from an AG-UI Message.
// It handles the Message.Content field which is typed as any. Content that is
// a list of parts yields its text parts, concatenated.
func contentString(msg *aguitypes.Message) string {
	if msg.Content == nil {
		return ""
	}
	if v, ok := msg.Content.(string); ok {
		return v
	}
	if contents := contentInputContents(msg); contents != nil {
		var sb strings.Builder
		for i := range contents {
			if contents[i].Type == aguitypes.InputContentTypeText {
				sb.WriteString(contents[i].Text)
			}
		}
		return sb.String()
	}
	// Try JSON marshaling for other types
	data, err := json.Marshal(msg.Content)
	if err != nil {
		return ""
	}
	return string(data)
}

// contentInputContents extracts multimodal InputContent from an AG-UI Message.
// Returns nil if the content is not a slice of InputContent.
func contentInputContents(msg *aguitypes.Message) []aguitypes.InputContent {
	if msg.Content == nil {
		return nil
	}

	// Try direct type assertion for []InputContent
	if contents, ok := msg.Content.([]aguitypes.InputContent); ok {
		return contents
	}

	// Try []any (common from JSON unmarshaling) and re-marshal through JSON
	return tryDecodeInputContents(msg.Content)
}

// tryDecodeInputContents attempts to decode an any value as []InputContent
// by marshaling through JSON. This handles the common case where JSON
// unmarshaling produces []any instead of []InputContent.
func tryDecodeInputContents(content any) []aguitypes.InputContent {
	arr, ok := content.([]any)
	if !ok {
		return nil
	}

	data, err := json.Marshal(arr)
	if err != nil {
		return nil
	}

	var contents []aguitypes.InputContent
	if err := json.Unmarshal(data, &contents); err != nil {
		return nil
	}

	// Verify at least one element has a recognized type
	for _, c := range contents {
		if isKnownPartType(c.Type) {
			return contents
		}
	}

	return nil
}

func isKnownPartType(t string) bool {
	if t == aguitypes.InputContentTypeText || t == aguitypes.InputContentTypeBinary {
		return true
	}
	_, ok := mediaPartTypes[t]
	return ok
}
