// Package agui provides bidirectional converters between PromptKit internal types
// and the AG-UI Go SDK types, enabling interoperability between the two systems.
package agui

import (
	"encoding/json"

	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// roleToAGUI maps PromptKit role strings to AG-UI Role constants.
var roleToAGUI = map[string]aguitypes.Role{
	"user":      aguitypes.RoleUser,
	"assistant": aguitypes.RoleAssistant,
	"tool":      aguitypes.RoleTool,
	"system":    aguitypes.RoleSystem,
}

// roleFromAGUI maps AG-UI Role constants to PromptKit role strings.
var roleFromAGUI = map[aguitypes.Role]string{
	aguitypes.RoleUser:      "user",
	aguitypes.RoleAssistant: "assistant",
	aguitypes.RoleTool:      "tool",
	aguitypes.RoleSystem:    "system",
	aguitypes.RoleDeveloper: "system",
}

// idGen is the default ID generator used for creating message IDs.
var idGen = events.NewDefaultIDGenerator()

// MessageToAGUI converts a PromptKit Message to an AG-UI Message.
// It maps roles, content (text or multimodal), tool calls, and tool results.
func MessageToAGUI(msg *types.Message) aguitypes.Message {
	aguiMsg := aguitypes.Message{
		ID:   idGen.GenerateMessageID(),
		Role: mapRoleToAGUI(msg.Role),
	}

	// Handle tool result messages
	if msg.Role == "tool" && msg.ToolResult != nil {
		aguiMsg.ToolCallID = msg.ToolResult.ID
		aguiMsg.Content = msg.ToolResult.Content
		return aguiMsg
	}

	// Handle multimodal content
	if len(msg.Parts) > 0 {
		aguiMsg.Content = partsToAGUI(msg.Parts)
	} else {
		aguiMsg.Content = msg.Content
	}

	// Map tool calls
	if len(msg.ToolCalls) > 0 {
		aguiMsg.ToolCalls = toolCallsToAGUI(msg.ToolCalls)
	}

	return aguiMsg
}

// MessageFromAGUI converts an AG-UI Message to a PromptKit Message.
// It maps roles, content (text or multimodal), tool calls, and tool call IDs.
func MessageFromAGUI(msg *aguitypes.Message) types.Message {
	pkMsg := types.Message{
		Role: mapRoleFromAGUI(msg.Role),
	}

	// Handle tool result messages
	if msg.Role == aguitypes.RoleTool {
		content := contentString(msg)
		pkMsg.Content = content
		pkMsg.ToolResult = &types.MessageToolResult{
			ID:      msg.ToolCallID,
			Content: content,
		}
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

// MessagesToAGUI converts a slice of PromptKit Messages to AG-UI Messages.
func MessagesToAGUI(msgs []types.Message) []aguitypes.Message {
	result := make([]aguitypes.Message, len(msgs))
	for i := range msgs {
		result[i] = MessageToAGUI(&msgs[i])
	}
	return result
}

// MessagesFromAGUI converts a slice of AG-UI Messages to PromptKit Messages.
func MessagesFromAGUI(msgs []aguitypes.Message) []types.Message {
	result := make([]types.Message, len(msgs))
	for i := range msgs {
		result[i] = MessageFromAGUI(&msgs[i])
	}
	return result
}

// roleAssistant is the PromptKit role string for assistant messages.
const roleAssistant = "assistant"

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
	return "user"
}

// partsToAGUI converts PromptKit ContentParts to AG-UI InputContent slices.
func partsToAGUI(parts []types.ContentPart) []aguitypes.InputContent {
	result := make([]aguitypes.InputContent, 0, len(parts))
	for _, part := range parts {
		if ic, ok := contentPartToAGUI(part); ok {
			result = append(result, ic)
		}
	}
	return result
}

// contentPartToAGUI converts a single PromptKit ContentPart to an AG-UI InputContent.
// Returns false if the part cannot be converted.
func contentPartToAGUI(part types.ContentPart) (aguitypes.InputContent, bool) {
	switch part.Type {
	case types.ContentTypeText:
		if part.Text == nil {
			return aguitypes.InputContent{}, false
		}
		return aguitypes.InputContent{
			Type: string(aguitypes.InputContentTypeText),
			Text: *part.Text,
		}, true
	case types.ContentTypeImage:
		if part.Media == nil {
			return aguitypes.InputContent{}, false
		}
		return imagePartToAGUI(part.Media), true
	default:
		return aguitypes.InputContent{}, false
	}
}

// imagePartToAGUI converts a PromptKit MediaContent to an AG-UI binary InputContent.
func imagePartToAGUI(media *types.MediaContent) aguitypes.InputContent {
	ic := aguitypes.InputContent{
		Type:     string(aguitypes.InputContentTypeBinary),
		MimeType: media.MIMEType,
	}
	if media.URL != nil {
		ic.URL = *media.URL
	}
	if media.Data != nil {
		ic.Data = *media.Data
	}
	return ic
}

// partsFromAGUI converts AG-UI InputContent slices to PromptKit ContentParts.
func partsFromAGUI(contents []aguitypes.InputContent) []types.ContentPart {
	result := make([]types.ContentPart, 0, len(contents))
	for _, ic := range contents {
		switch ic.Type {
		case string(aguitypes.InputContentTypeText):
			text := ic.Text
			result = append(result, types.ContentPart{
				Type: types.ContentTypeText,
				Text: &text,
			})
		case string(aguitypes.InputContentTypeBinary):
			result = append(result, binaryInputContentToPart(&ic))
		}
	}
	return result
}

// binaryInputContentToPart converts an AG-UI binary InputContent to a PromptKit ContentPart.
func binaryInputContentToPart(ic *aguitypes.InputContent) types.ContentPart {
	media := &types.MediaContent{
		MIMEType: ic.MimeType,
	}
	if ic.URL != "" {
		url := ic.URL
		media.URL = &url
	}
	if ic.Data != "" {
		data := ic.Data
		media.Data = &data
	}
	return types.ContentPart{
		Type:  types.ContentTypeImage,
		Media: media,
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
	}

	if t.Parameters != nil {
		if data, err := json.Marshal(t.Parameters); err == nil {
			td.InputSchema = data
		}
	}

	return td
}

// contentString extracts the text content from an AG-UI Message.
// It handles the Message.Content field which is typed as any.
func contentString(msg *aguitypes.Message) string {
	if msg.Content == nil {
		return ""
	}
	switch v := msg.Content.(type) {
	case string:
		return v
	default:
		// Try JSON marshaling for other types
		data, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(data)
	}
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
		if c.Type == string(aguitypes.InputContentTypeText) ||
			c.Type == string(aguitypes.InputContentTypeBinary) {
			return contents
		}
	}

	return nil
}
