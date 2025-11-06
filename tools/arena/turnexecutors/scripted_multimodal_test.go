package turnexecutors

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestScriptedExecutor_BuildUserMessage_LegacyTextContent(t *testing.T) {
	executor := NewScriptedExecutor(nil)

	req := TurnRequest{
		ScriptedContent: "Hello, world!",
	}

	msg, err := executor.buildUserMessage(req)
	if err != nil {
		t.Fatalf("buildUserMessage() error = %v", err)
	}

	if msg.Role != "user" {
		t.Errorf("Expected role 'user', got %s", msg.Role)
	}

	if msg.Content != "Hello, world!" {
		t.Errorf("Expected content 'Hello, world!', got %s", msg.Content)
	}

	if len(msg.Parts) != 0 {
		t.Errorf("Expected no parts for legacy content, got %d", len(msg.Parts))
	}
}

func TestScriptedExecutor_BuildUserMessage_MultimodalParts(t *testing.T) {
	executor := NewScriptedExecutor(nil)

	turnParts := []config.TurnContentPart{
		{Type: "text", Text: "What's in this image?"},
		{
			Type: "image",
			Media: &config.TurnMediaContent{
				URL:      "https://example.com/image.jpg",
				MIMEType: "image/jpeg",
			},
		},
	}

	req := TurnRequest{
		ScriptedContent: "ignored text content",
		ScriptedParts:   turnParts,
	}

	msg, err := executor.buildUserMessage(req)
	if err != nil {
		t.Fatalf("buildUserMessage() error = %v", err)
	}

	if msg.Role != "user" {
		t.Errorf("Expected role 'user', got %s", msg.Role)
	}

	if len(msg.Parts) != 2 {
		t.Fatalf("Expected 2 parts, got %d", len(msg.Parts))
	}

	// Verify text part
	if msg.Parts[0].Type != types.ContentTypeText {
		t.Errorf("Expected first part to be text, got %s", msg.Parts[0].Type)
	}

	// Verify image part
	if msg.Parts[1].Type != types.ContentTypeImage {
		t.Errorf("Expected second part to be image, got %s", msg.Parts[1].Type)
	}
}

func TestScriptedExecutor_BuildUserMessage_MultimodalWithFile(t *testing.T) {
	tmpDir := t.TempDir()
	imageFile := filepath.Join(tmpDir, "test.jpg")

	err := os.WriteFile(imageFile, []byte("fake image data"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	executor := NewScriptedExecutor(nil)

	turnParts := []config.TurnContentPart{
		{Type: "text", Text: "Describe this image:"},
		{
			Type: "image",
			Media: &config.TurnMediaContent{
				FilePath: imageFile, // Use absolute path
				MIMEType: "image/jpeg",
			},
		},
	}

	req := TurnRequest{
		ScriptedParts: turnParts,
	}

	msg, err := executor.buildUserMessage(req)
	if err != nil {
		t.Fatalf("buildUserMessage() error = %v", err)
	}

	if len(msg.Parts) != 2 {
		t.Fatalf("Expected 2 parts, got %d", len(msg.Parts))
	}

	// Verify image was loaded
	if msg.Parts[1].Media == nil || msg.Parts[1].Media.Data == nil {
		t.Error("Expected image data to be loaded")
	}
}

func TestScriptedExecutor_BuildUserMessage_InvalidParts(t *testing.T) {
	executor := NewScriptedExecutor(nil)

	turnParts := []config.TurnContentPart{
		{Type: "invalid_type", Text: "test"},
	}

	req := TurnRequest{
		ScriptedParts: turnParts,
	}

	_, err := executor.buildUserMessage(req)
	if err == nil {
		t.Error("Expected error for invalid part type")
	}
}

func TestScriptedExecutor_BuildUserMessage_FileNotFound(t *testing.T) {
	executor := NewScriptedExecutor(nil)

	turnParts := []config.TurnContentPart{
		{
			Type: "image",
			Media: &config.TurnMediaContent{
				FilePath: "/nonexistent/file.jpg",
			},
		},
	}

	req := TurnRequest{
		ScriptedParts: turnParts,
	}

	_, err := executor.buildUserMessage(req)
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}
