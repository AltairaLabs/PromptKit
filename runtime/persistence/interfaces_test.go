package persistence

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// TestPromptRepositoryInterface verifies that the PromptRepository interface is correctly defined
func TestPromptRepositoryInterface(t *testing.T) {
	// This test ensures the interface exists and has the required methods
	// We'll test actual implementations in their respective test files
	var _ PromptRepository = (*mockPromptRepo)(nil)
}

// TestToolRepositoryInterface verifies that the ToolRepository interface is correctly defined
func TestToolRepositoryInterface(t *testing.T) {
	// This test ensures the interface exists and has the required methods
	// We'll test actual implementations in their respective test files
	var _ ToolRepository = (*mockToolRepo)(nil)
}

// Mock implementations for interface testing
type mockPromptRepo struct{}

func (m *mockPromptRepo) LoadPrompt(taskType string) (*prompt.Config, error) {
	return nil, nil
}

func (m *mockPromptRepo) LoadFragment(name string, relativePath string, baseDir string) (*prompt.Fragment, error) {
	return nil, nil
}

func (m *mockPromptRepo) ListPrompts() ([]string, error) {
	return nil, nil
}

func (m *mockPromptRepo) SavePrompt(config *prompt.Config) error {
	return nil
}

type mockToolRepo struct{}

func (m *mockToolRepo) LoadTool(name string) (*tools.ToolDescriptor, error) {
	return nil, nil
}

func (m *mockToolRepo) ListTools() ([]string, error) {
	return nil, nil
}

func (m *mockToolRepo) SaveTool(descriptor *tools.ToolDescriptor) error {
	return nil
}
