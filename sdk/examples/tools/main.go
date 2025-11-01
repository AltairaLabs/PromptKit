package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// This example demonstrates how to use tools with the SDK.
// It shows:
// 1. Creating and registering tool descriptors
// 2. Using mock tools for testing
// 3. Tool execution during conversations
// 4. Handling tool results

func main() {
	// Check for API key
	apiKey := os.Getenv("OPENAI_API_KEY") // NOSONAR: Example code showing environment variable usage
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required") // NOSONAR: Example code
	}

	// Create provider
	provider := providers.NewOpenAIProvider(
		"openai",
		"gpt-4",
		"",
		providers.ProviderDefaults{
			Temperature: 0.7,
			MaxTokens:   500,
		},
		false,
	)

	// Create tool registry
	toolRegistry := tools.NewRegistry()

	// Register mock tools
	if err := registerTools(toolRegistry); err != nil {
		log.Fatalf("Failed to register tools: %v", err)
	}

	// Create conversation manager with tools
	manager, err := sdk.NewConversationManager(
		sdk.WithProvider(provider),
		sdk.WithToolRegistry(toolRegistry),
	)
	if err != nil {
		log.Fatalf("Failed to create conversation manager: %v", err)
	}

	// Create a test pack with tool policy
	pack := createTestPack()

	// Create conversation
	ctx := context.Background()
	conv, err := manager.CreateConversation(ctx, pack, sdk.ConversationConfig{
		UserID:     "user123",
		PromptName: "assistant",
		Variables: map[string]interface{}{
			"name": "Tool Demo",
		},
	})
	if err != nil {
		log.Fatalf("Failed to create conversation: %v", err)
	}

	fmt.Printf("🤖 Tool Demo Conversation (ID: %s)\n\n", conv.GetID())

	// Example 1: Simple tool call
	fmt.Println("=== Example 1: Get Current Time ===")
	fmt.Println("User: What time is it?")
	resp, err := conv.Send(ctx, "What time is it?")
	if err != nil {
		log.Fatalf("Failed to send message: %v", err)
	}
	fmt.Printf("Assistant: %s\n", resp.Content)
	if len(resp.ToolCalls) > 0 {
		fmt.Printf("🔧 Tools called: %d\n", len(resp.ToolCalls))
		for _, tc := range resp.ToolCalls {
			fmt.Printf("  - %s\n", tc.Name)
		}
	}
	fmt.Printf("💰 Cost: $%.4f | ⏱️  Latency: %dms\n\n", resp.Cost, resp.LatencyMs)

	// Example 2: Tool with parameters
	time.Sleep(1 * time.Second)
	fmt.Println("=== Example 2: Weather Query ===")
	fmt.Println("User: What's the weather in San Francisco?")
	resp, err = conv.Send(ctx, "What's the weather in San Francisco?")
	if err != nil {
		log.Fatalf("Failed to send message: %v", err)
	}
	fmt.Printf("Assistant: %s\n", resp.Content)
	if len(resp.ToolCalls) > 0 {
		fmt.Printf("🔧 Tools called: %d\n", len(resp.ToolCalls))
		for _, tc := range resp.ToolCalls {
			fmt.Printf("  - %s with args: %s\n", tc.Name, string(tc.Args))
		}
	}
	fmt.Printf("💰 Cost: $%.4f | ⏱️  Latency: %dms\n\n", resp.Cost, resp.LatencyMs)

	// Example 3: Calculator tool
	time.Sleep(1 * time.Second)
	fmt.Println("=== Example 3: Calculator ===")
	fmt.Println("User: What is 42 + 58?")
	resp, err = conv.Send(ctx, "What is 42 + 58?")
	if err != nil {
		log.Fatalf("Failed to send message: %v", err)
	}
	fmt.Printf("Assistant: %s\n", resp.Content)
	if len(resp.ToolCalls) > 0 {
		fmt.Printf("🔧 Tools called: %d\n", len(resp.ToolCalls))
		for _, tc := range resp.ToolCalls {
			fmt.Printf("  - %s with args: %s\n", tc.Name, string(tc.Args))
		}
	}
	fmt.Printf("💰 Cost: $%.4f | ⏱️  Latency: %dms\n\n", resp.Cost, resp.LatencyMs)

	// Show conversation history
	fmt.Println("📜 Conversation History:")
	history := conv.GetHistory()
	for i, msg := range history {
		preview := msg.Content
		if len(preview) > 60 {
			preview = preview[:60] + "..."
		}
		fmt.Printf("  %d. [%s] %s\n", i+1, msg.Role, preview)
	}
}

// registerTools registers mock tools for demonstration
func registerTools(registry *tools.Registry) error {
	// Tool 1: Get current time
	getCurrentTimeSchema := json.RawMessage(`{
		"type": "object",
		"properties": {},
		"required": []
	}`)

	currentTimeResponse := json.RawMessage(fmt.Sprintf(`{
		"time": "%s",
		"timezone": "UTC"
	}`, time.Now().Format(time.RFC3339)))

	currentTimeTool := &tools.ToolDescriptor{
		Name:         "get_current_time",
		Description:  "Get the current time in UTC",
		InputSchema:  getCurrentTimeSchema,
		OutputSchema: getCurrentTimeSchema, // Any object is valid
		Mode:         "mock",
		MockResult:   currentTimeResponse,
		TimeoutMs:    1000,
	}
	if err := registry.Register(currentTimeTool); err != nil {
		return err
	}

	// Tool 2: Get weather
	getWeatherSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"location": {
				"type": "string",
				"description": "City name, e.g., 'San Francisco'"
			}
		},
		"required": ["location"]
	}`)

	weatherResponse := json.RawMessage(`{
		"temperature": 72,
		"conditions": "Sunny",
		"humidity": 65,
		"wind_speed": 10
	}`)

	weatherTool := &tools.ToolDescriptor{
		Name:         "get_weather",
		Description:  "Get current weather for a location",
		InputSchema:  getWeatherSchema,
		OutputSchema: getWeatherSchema, // Any object is valid
		Mode:         "mock",
		MockResult:   weatherResponse,
		TimeoutMs:    2000,
	}
	if err := registry.Register(weatherTool); err != nil {
		return err
	}

	// Tool 3: Calculator
	calculatorSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"operation": {
				"type": "string",
				"enum": ["add", "subtract", "multiply", "divide"],
				"description": "The mathematical operation to perform"
			},
			"a": {
				"type": "number",
				"description": "First operand"
			},
			"b": {
				"type": "number",
				"description": "Second operand"
			}
		},
		"required": ["operation", "a", "b"]
	}`)

	calculatorResponse := json.RawMessage(`{
		"result": 100
	}`)

	calculatorTool := &tools.ToolDescriptor{
		Name:         "calculator",
		Description:  "Perform basic mathematical operations",
		InputSchema:  calculatorSchema,
		OutputSchema: calculatorSchema, // Any object is valid
		Mode:         "mock",
		MockResult:   calculatorResponse,
		TimeoutMs:    1000,
	}
	if err := registry.Register(calculatorTool); err != nil {
		return err
	}

	return nil
}

// createTestPack creates a pack with tool policy configuration
func createTestPack() *sdk.Pack {
	return &sdk.Pack{
		ID:      "tools-demo",
		Name:    "Tools Demo Pack",
		Version: "1.0.0",
		TemplateEngine: sdk.TemplateEngine{
			Version:  "v1",
			Syntax:   "{{variable}}",
			Features: []string{"basic_substitution"},
		},
		Prompts: map[string]*sdk.Prompt{
			"assistant": {
				ID:   "assistant",
				Name: "Assistant",
				SystemTemplate: `You are a helpful assistant named {{name}}. You have access to tools that can help answer questions.
When the user asks for information that requires a tool, use the appropriate tool to get the data, then provide a natural response.`,
				Variables: []*sdk.Variable{
					{
						Name:     "name",
						Type:     "string",
						Required: true,
					},
				},
				Parameters: &sdk.Parameters{
					Temperature: 0.7,
					MaxTokens:   500,
				},
				// Enable tool usage with automatic tool selection
				ToolPolicy: &sdk.ToolPolicy{
					ToolChoice:          "auto",
					MaxToolCallsPerTurn: 5,
				},
			},
		},
	}
}
