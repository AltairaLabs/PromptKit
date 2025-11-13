package mock

import (
	"context"
	"fmt"
	"os"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"gopkg.in/yaml.v3"
)

// MockResponseRepository provides an interface for retrieving mock responses.
// This abstraction allows mock data to come from various sources (files, databases, etc.)
// and makes MockProvider reusable across different contexts (Arena, SDK examples, unit tests).
type MockResponseRepository interface {
	// GetResponse retrieves a mock response for the given context.
	// Parameters can include scenario ID, turn number, provider ID, etc.
	// Returns the response text and any error encountered.
	GetResponse(ctx context.Context, params MockResponseParams) (string, error)

	// GetTurn retrieves a mock turn response that may include tool calls.
	// This extends GetResponse to support structured turn data with tool call simulation.
	GetTurn(ctx context.Context, params MockResponseParams) (*MockTurn, error)
}

// MockResponseParams contains parameters for looking up mock responses.
// Different implementations may use different subsets of these fields.
type MockResponseParams struct {
	ScenarioID string // Optional: ID of the scenario being executed
	TurnNumber int    // Optional: Turn number in a multi-turn conversation
	ProviderID string // Optional: ID of the provider being mocked
	ModelName  string // Optional: Model name being mocked
}

// MockTurn represents a structured mock response that may include tool calls and multimodal content.
// This extends simple text responses to support tool call simulation and multimodal content parts.
type MockTurn struct {
	Type      string             `yaml:"type"`                 // "text", "tool_calls", or "multimodal"
	Content   string             `yaml:"content,omitempty"`    // Text content for the response (backward compatibility)
	Text      string             `yaml:"text,omitempty"`       // Text content (preferred for multimodal responses)
	Parts     []MockContentPart  `yaml:"parts,omitempty"`      // Multimodal content parts (text, image, audio, video)
	ToolCalls []MockToolCall     `yaml:"tool_calls,omitempty"` // Tool calls to simulate
}

// MockContentPart represents a single content part in a multimodal mock response.
// This mirrors the structure of types.ContentPart but with YAML-friendly field names.
type MockContentPart struct {
	Type     string          `yaml:"type"`               // "text", "image", "audio", or "video"
	Text     string          `yaml:"text,omitempty"`     // Text content (for type="text")
	ImageURL *MockImageURL   `yaml:"image_url,omitempty"` // Image URL (for type="image")
	AudioURL *MockAudioURL   `yaml:"audio_url,omitempty"` // Audio URL (for type="audio")
	VideoURL *MockVideoURL   `yaml:"video_url,omitempty"` // Video URL (for type="video")
	Metadata map[string]interface{} `yaml:"metadata,omitempty"` // Additional metadata
}

// MockImageURL represents image content in a mock response.
type MockImageURL struct {
	URL    string  `yaml:"url"`              // URL to the image (can be mock://, http://, https://, data:, or file path)
	Detail *string `yaml:"detail,omitempty"` // Detail level: "low", "high", "auto"
}

// MockAudioURL represents audio content in a mock response.
type MockAudioURL struct {
	URL string `yaml:"url"` // URL to the audio file (can be mock://, http://, https://, data:, or file path)
}

// MockVideoURL represents video content in a mock response.
type MockVideoURL struct {
	URL string `yaml:"url"` // URL to the video file (can be mock://, http://, https://, data:, or file path)
}

// MockToolCall represents a simulated tool call from the LLM.
type MockToolCall struct {
	Name      string                 `yaml:"name"`      // Name of the tool to call
	Arguments map[string]interface{} `yaml:"arguments"` // Arguments to pass to the tool
}

// MockConfig represents the structure of a mock configuration file.
// This allows scenario-specific and turn-specific responses to be defined.
type MockConfig struct {
	// Default response if no specific match is found
	DefaultResponse string `yaml:"defaultResponse"`

	// Scenario-specific responses keyed by scenario ID
	Scenarios map[string]ScenarioMockConfig `yaml:"scenarios,omitempty"`
}

// ScenarioMockConfig defines mock responses for a specific scenario.
type ScenarioMockConfig struct {
	// Default response for this scenario (overrides global default)
	DefaultResponse string `yaml:"defaultResponse,omitempty"`

	// Turn-specific responses keyed by turn number (1-indexed)
	// Supports both simple string responses (backward compatibility) and structured MockTurn responses
	Turns map[int]interface{} `yaml:"turns,omitempty"`

	// Tool execution responses for repository-backed tool mocking
	ToolResponses map[string][]MockToolResponse `yaml:"tool_responses,omitempty"`
}

// MockToolResponse represents a configured response for tool execution.
type MockToolResponse struct {
	CallArgs map[string]interface{} `yaml:"call_args"`        // Match these arguments
	Result   interface{}            `yaml:"result,omitempty"` // Return this result
	Error    *MockToolError         `yaml:"error,omitempty"`  // Or return this error
}

// MockToolError represents an error response for tool execution.
type MockToolError struct {
	Type    string `yaml:"type"`    // Error type/category
	Message string `yaml:"message"` // Error message
}

// FileMockRepository loads mock responses from a YAML configuration file.
// This is the default implementation for file-based mock configurations.
type FileMockRepository struct {
	config *MockConfig
}

// NewFileMockRepository creates a repository that loads mock responses from a YAML file.
// The file should follow the MockConfig structure with scenarios and turn-specific responses.
func NewFileMockRepository(configPath string) (*FileMockRepository, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read mock config file: %w", err)
	}

	var config MockConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse mock config YAML: %w", err)
	}

	return &FileMockRepository{
		config: &config,
	}, nil
}

// GetResponse retrieves a mock response based on the provided parameters.
// It follows this priority order:
// 1. Scenario + Turn specific response
// 2. Scenario default response
// 3. Global default response
// 4. Generic fallback message
func (r *FileMockRepository) GetResponse(ctx context.Context, params MockResponseParams) (string, error) {
	// Use GetTurn to get structured response, then extract text content
	turn, err := r.GetTurn(ctx, params)
	if err != nil {
		return "", err
	}

	return turn.Content, nil
}

// GetTurn retrieves a structured mock turn response that may include tool calls.
// This method supports both backward-compatible string responses and new structured MockTurn responses.
func (r *FileMockRepository) GetTurn(ctx context.Context, params MockResponseParams) (*MockTurn, error) {
	logger.Debug("FileMockRepository GetTurn",
		"scenario_id", params.ScenarioID,
		"turn_number", params.TurnNumber,
		"provider_id", params.ProviderID,
		"model", params.ModelName,
		"available_scenarios", getScenarioKeys(r.config.Scenarios))

	// Try scenario + turn specific response
	if params.ScenarioID != "" {
		if scenario, exists := r.config.Scenarios[params.ScenarioID]; exists {
			logger.Debug("Found scenario in config", "scenario_id", params.ScenarioID)
			if params.TurnNumber > 0 {
				if turnResponse, ok := scenario.Turns[params.TurnNumber]; ok {
					logger.Debug("Using scenario+turn specific response",
						"scenario_id", params.ScenarioID,
						"turn_number", params.TurnNumber,
						"response_type", fmt.Sprintf("%T", turnResponse))

					return r.parseTurnResponse(turnResponse)
				}
				logger.Debug("No turn-specific response found", "turn_number", params.TurnNumber)
			}

			// Try scenario default
			if scenario.DefaultResponse != "" {
				logger.Debug("Using scenario default response", "scenario_id", params.ScenarioID, "response", scenario.DefaultResponse)
				return &MockTurn{
					Type:    "text",
					Content: scenario.DefaultResponse,
				}, nil
			}
			logger.Debug("No scenario default response configured", "scenario_id", params.ScenarioID)
		} else {
			logger.Debug("Scenario not found in config", "scenario_id", params.ScenarioID, "available_scenarios", getScenarioKeys(r.config.Scenarios))
		}
	}

	// Try global default
	if r.config.DefaultResponse != "" {
		logger.Debug("Using global default response", "response", r.config.DefaultResponse)
		return &MockTurn{
			Type:    "text",
			Content: r.config.DefaultResponse,
		}, nil
	}

	// Final fallback
	fallback := fmt.Sprintf("Mock response for provider %s model %s", params.ProviderID, params.ModelName)
	logger.Debug("Using final fallback response", "response", fallback)
	return &MockTurn{
		Type:    "text",
		Content: fallback,
	}, nil
}

// parseTurnResponse parses a turn response that could be either a string (backward compatibility)
// or a structured MockTurn object.
func (r *FileMockRepository) parseTurnResponse(response interface{}) (*MockTurn, error) {
	switch v := response.(type) {
	case string:
		return r.parseStringResponse(v), nil
	case map[string]interface{}:
		return r.parseStructuredResponse(v)
	default:
		return nil, fmt.Errorf("unsupported turn response type: %T", response)
	}
}

// parseStringResponse creates a simple text MockTurn from a string response.
func (r *FileMockRepository) parseStringResponse(content string) *MockTurn {
	return &MockTurn{
		Type:    "text",
		Content: content,
	}
}

// parseStructuredResponse parses a map into a MockTurn structure.
func (r *FileMockRepository) parseStructuredResponse(responseMap map[string]interface{}) (*MockTurn, error) {
	turn := MockTurn{
		Type: "text", // default type
	}

	// Parse type field
	if typeVal, ok := responseMap["type"].(string); ok {
		turn.Type = typeVal
	}

	// Parse content field (supports both "content" and "response" for backward compatibility)
	if contentVal, ok := responseMap["content"].(string); ok {
		turn.Content = contentVal
	} else if responseVal, ok := responseMap["response"].(string); ok {
		// Support legacy "response" field name
		turn.Content = responseVal
	}

	// Parse tool calls field
	if toolCallsVal, ok := responseMap["tool_calls"].([]interface{}); ok {
		toolCalls, err := r.parseToolCalls(toolCallsVal)
		if err != nil {
			return nil, fmt.Errorf("failed to parse tool calls: %w", err)
		}
		turn.ToolCalls = toolCalls
		// Automatically set type to tool_calls if tool calls are present
		if turn.Type == "text" {
			turn.Type = "tool_calls"
		}
	}

	return &turn, nil
}

// parseToolCalls parses tool call data from interface{} slice.
func (r *FileMockRepository) parseToolCalls(toolCallsData []interface{}) ([]MockToolCall, error) {
	toolCalls := make([]MockToolCall, len(toolCallsData))

	for i, tc := range toolCallsData {
		tcMap, ok := tc.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("tool call %d is not a map", i)
		}

		mockTC := MockToolCall{}

		if name, ok := tcMap["name"].(string); ok {
			mockTC.Name = name
		}

		if args, ok := tcMap["arguments"].(map[string]interface{}); ok {
			mockTC.Arguments = args
		}

		toolCalls[i] = mockTC
	}

	return toolCalls, nil
}

// InMemoryMockRepository stores mock responses in memory.
// This is useful for testing and programmatic configuration without files.
type InMemoryMockRepository struct {
	responses       map[string]string // key: "scenario:turn" -> response
	defaultResponse string
}

// NewInMemoryMockRepository creates an in-memory repository with a default response.
func NewInMemoryMockRepository(defaultResponse string) *InMemoryMockRepository {
	return &InMemoryMockRepository{
		responses:       make(map[string]string),
		defaultResponse: defaultResponse,
	}
}

// SetResponse sets a mock response for a specific scenario and turn.
// Use turnNumber = 0 for scenario default, or -1 for global default.
func (r *InMemoryMockRepository) SetResponse(scenarioID string, turnNumber int, response string) {
	if scenarioID == "" && turnNumber == -1 {
		r.defaultResponse = response
		return
	}

	key := fmt.Sprintf("%s:%d", scenarioID, turnNumber)
	r.responses[key] = response
}

// GetResponse retrieves a mock response based on the provided parameters.
func (r *InMemoryMockRepository) GetResponse(ctx context.Context, params MockResponseParams) (string, error) {
	turn, err := r.GetTurn(ctx, params)
	if err != nil {
		return "", err
	}
	return turn.Content, nil
}

// GetTurn retrieves a structured mock turn response.
// InMemoryMockRepository currently only supports simple text responses.
func (r *InMemoryMockRepository) GetTurn(ctx context.Context, params MockResponseParams) (*MockTurn, error) {
	var content string

	// Try scenario + turn specific
	if params.ScenarioID != "" && params.TurnNumber > 0 {
		key := fmt.Sprintf("%s:%d", params.ScenarioID, params.TurnNumber)
		if response, exists := r.responses[key]; exists {
			content = response
		}
	}

	// Try scenario default (turn 0)
	if content == "" && params.ScenarioID != "" {
		key := fmt.Sprintf("%s:0", params.ScenarioID)
		if response, exists := r.responses[key]; exists {
			content = response
		}
	}

	// Global default
	if content == "" && r.defaultResponse != "" {
		content = r.defaultResponse
	}

	// Final fallback
	if content == "" {
		content = fmt.Sprintf("Mock response for provider %s model %s", params.ProviderID, params.ModelName)
	}

	return &MockTurn{
		Type:    "text",
		Content: content,
	}, nil
}

// ToContentParts converts MockTurn to a slice of types.ContentPart.
// This handles both legacy text-only responses and new multimodal responses.
func (t *MockTurn) ToContentParts() []types.ContentPart {
	// If Parts are explicitly defined, convert them
	if len(t.Parts) > 0 {
		parts := make([]types.ContentPart, 0, len(t.Parts))
		for _, mockPart := range t.Parts {
			if part := mockPart.ToContentPart(); part != nil {
				parts = append(parts, *part)
			}
		}
		return parts
	}

	// Backward compatibility: if only text/content is set, return a single text part
	text := t.Text
	if text == "" {
		text = t.Content
	}
	if text != "" {
		return []types.ContentPart{types.NewTextPart(text)}
	}

	return nil
}

// ToContentPart converts a MockContentPart to types.ContentPart.
func (m *MockContentPart) ToContentPart() *types.ContentPart {
	switch m.Type {
	case "text":
		if m.Text != "" {
			part := types.NewTextPart(m.Text)
			return &part
		}

	case "image":
		if m.ImageURL != nil {
			return m.imageURLToContentPart()
		}

	case "audio":
		if m.AudioURL != nil {
			return m.audioURLToContentPart()
		}

	case "video":
		if m.VideoURL != nil {
			return m.videoURLToContentPart()
		}
	}

	return nil
}

// imageURLToContentPart converts MockImageURL to types.ContentPart
func (m *MockContentPart) imageURLToContentPart() *types.ContentPart {
	url := m.ImageURL.URL
	mimeType := inferMIMETypeFromURL(url)
	
	media := &types.MediaContent{
		URL:      &url,
		MIMEType: mimeType,
		Detail:   m.ImageURL.Detail,
	}
	
	// Apply metadata if present
	m.applyMetadataToMedia(media)
	
	return &types.ContentPart{
		Type:  types.ContentTypeImage,
		Media: media,
	}
}

// audioURLToContentPart converts MockAudioURL to types.ContentPart
func (m *MockContentPart) audioURLToContentPart() *types.ContentPart {
	url := m.AudioURL.URL
	mimeType := inferMIMETypeFromURL(url)
	
	media := &types.MediaContent{
		URL:      &url,
		MIMEType: mimeType,
	}
	
	// Apply metadata if present
	m.applyMetadataToMedia(media)
	
	return &types.ContentPart{
		Type:  types.ContentTypeAudio,
		Media: media,
	}
}

// videoURLToContentPart converts MockVideoURL to types.ContentPart
func (m *MockContentPart) videoURLToContentPart() *types.ContentPart {
	url := m.VideoURL.URL
	mimeType := inferMIMETypeFromURL(url)
	
	media := &types.MediaContent{
		URL:      &url,
		MIMEType: mimeType,
	}
	
	// Apply metadata if present
	m.applyMetadataToMedia(media)
	
	return &types.ContentPart{
		Type:  types.ContentTypeVideo,
		Media: media,
	}
}

// applyMetadataToMedia applies metadata fields to MediaContent
func (m *MockContentPart) applyMetadataToMedia(media *types.MediaContent) {
	if m.Metadata == nil {
		return
	}
	
	// Extract and apply common metadata fields
	if format, ok := m.Metadata["format"].(string); ok {
		media.Format = &format
	}
	if width, ok := m.Metadata["width"].(int); ok {
		media.Width = &width
	}
	if height, ok := m.Metadata["height"].(int); ok {
		media.Height = &height
	}
	if size, ok := m.Metadata["size"].(int); ok {
		size64 := int64(size)
		sizeKB := size64 / 1024
		media.SizeKB = &sizeKB
	}
	if sizeKB, ok := m.Metadata["size_kb"].(int); ok {
		sizeKB64 := int64(sizeKB)
		media.SizeKB = &sizeKB64
	}
	if duration, ok := m.Metadata["duration"].(int); ok {
		media.Duration = &duration
	}
	if durationSeconds, ok := m.Metadata["duration_seconds"].(int); ok {
		media.Duration = &durationSeconds
	}
	if durationFloat, ok := m.Metadata["duration_seconds"].(float64); ok {
		durationInt := int(durationFloat)
		media.Duration = &durationInt
	}
	if bitRate, ok := m.Metadata["bit_rate"].(int); ok {
		media.BitRate = &bitRate
	}
	if channels, ok := m.Metadata["channels"].(int); ok {
		media.Channels = &channels
	}
	if fps, ok := m.Metadata["fps"].(int); ok {
		media.FPS = &fps
	}
	if caption, ok := m.Metadata["caption"].(string); ok {
		media.Caption = &caption
	}
}

// inferMIMETypeFromURL infers MIME type from URL based on extension
func inferMIMETypeFromURL(url string) string {
	// Handle mock:// URLs - infer from extension
	if len(url) > 7 && url[:7] == "mock://" {
		url = url[7:] // Remove mock:// prefix
	}
	
	// Try to infer from extension
	ext := ""
	for i := len(url) - 1; i >= 0; i-- {
		if url[i] == '.' {
			ext = url[i:]
			break
		}
		if url[i] == '/' || url[i] == '?' {
			break
		}
	}
	
	switch ext {
	// Images
	case ".jpg", ".jpeg":
		return types.MIMETypeImageJPEG
	case ".png":
		return types.MIMETypeImagePNG
	case ".gif":
		return types.MIMETypeImageGIF
	case ".webp":
		return types.MIMETypeImageWebP
	
	// Audio
	case ".mp3":
		return types.MIMETypeAudioMP3
	case ".wav":
		return types.MIMETypeAudioWAV
	case ".ogg", ".oga":
		return types.MIMETypeAudioOgg
	case ".weba":
		return types.MIMETypeAudioWebM
	
	// Video
	case ".mp4":
		return types.MIMETypeVideoMP4
	case ".webm":
		return types.MIMETypeVideoWebM
	case ".ogv":
		return types.MIMETypeVideoOgg
	
	default:
		// Default fallback
		return "application/octet-stream"
	}
}

// Helper functions for debug logging
func getScenarioKeys(scenarios map[string]ScenarioMockConfig) []string {
	keys := make([]string, 0, len(scenarios))
	for k := range scenarios {
		keys = append(keys, k)
	}
	return keys
}
