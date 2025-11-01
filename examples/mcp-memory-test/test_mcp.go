package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

func main() {
	fmt.Println("🧪 MCP Integration Test")
	fmt.Println("========================\n")

	// Step 1: Create MCP Registry
	fmt.Println("Step 1: Creating MCP Registry...")
	registry := mcp.NewRegistry()

	// Step 2: Register Memory Server
	fmt.Println("Step 2: Registering memory server...")
	serverConfig := mcp.ServerConfig{
		Name:    "memory",
		Command: "npx",
		Args:    []string{"-y", "@modelcontextprotocol/server-memory"},
		Env: map[string]string{
			"PATH": fmt.Sprintf("%s:%s", os.Getenv("PATH"), "/usr/local/bin"),
		},
	}

	if err := registry.RegisterServer(serverConfig); err != nil {
		log.Fatalf("Failed to register server: %v", err)
	}
	fmt.Println("✓ Server registered\n")

	// Step 3: Initialize Client (lazy - happens on first GetClient)
	fmt.Println("Step 3: Initializing MCP client...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := registry.GetClient(ctx, "memory")
	if err != nil {
		log.Fatalf("Failed to get client: %v", err)
	}
	fmt.Println("✓ Client initialized\n")

	// Step 4: List Tools
	fmt.Println("Step 4: Listing available tools...")
	toolMap, err := registry.ListAllTools(ctx)
	if err != nil {
		log.Fatalf("Failed to list tools: %v", err)
	}

	for serverName, mcpTools := range toolMap {
		fmt.Printf("Server: %s\n", serverName)
		for _, tool := range mcpTools {
			fmt.Printf("  - %s: %s\n", tool.Name, tool.Description)
		}
	}
	fmt.Println()

	// Step 5: Test Tool Execution (via MCPExecutor)
	fmt.Println("Step 5: Testing tool execution...")

	toolRegistry := tools.NewRegistry()
	mcpExecutor := tools.NewMCPExecutor(registry)
	toolRegistry.RegisterExecutor(mcpExecutor)

	// Register discovered tools
	for _, mcpTools := range toolMap {
		for _, mcpTool := range mcpTools {
			toolDesc := &tools.ToolDescriptor{
				Name:        mcpTool.Name,
				Description: mcpTool.Description,
				InputSchema: mcpTool.InputSchema,
				Mode:        "mcp",
			}
			if err := toolRegistry.Register(toolDesc); err != nil {
				log.Fatalf("Failed to register tool: %v", err)
			}
		}
	}
	fmt.Println("✓ Tools registered in tool registry\n")

	// Step 6: Call create_entities (create a knowledge node)
	fmt.Println("Step 6: Calling create_entities(entities=[{name: 'Alice', entityType: 'person', observations: ['likes blue']}])...")
	createArgs := map[string]interface{}{
		"entities": []map[string]interface{}{
			{
				"name":         "Alice",
				"entityType":   "person",
				"observations": []string{"likes blue"},
			},
		},
	}

	createArgsJSON, err := json.Marshal(createArgs)
	if err != nil {
		log.Fatalf("Failed to marshal args: %v", err)
	}

	createResult, err := toolRegistry.Execute("create_entities", createArgsJSON)
	if err != nil {
		log.Fatalf("Failed to call create_entities: %v", err)
	}
	fmt.Printf("Result: %s\n\n", string(createResult.Result))

	// Step 7: Call read_graph (retrieve the knowledge graph)
	fmt.Println("Step 7: Calling read_graph()...")
	readArgs := map[string]interface{}{}

	readArgsJSON, err := json.Marshal(readArgs)
	if err != nil {
		log.Fatalf("Failed to marshal args: %v", err)
	}

	readResult, err := toolRegistry.Execute("read_graph", readArgsJSON)
	if err != nil {
		log.Fatalf("Failed to call read_graph: %v", err)
	}
	fmt.Printf("Result: %s\n\n", string(readResult.Result))

	// Step 8: Cleanup
	fmt.Println("Step 8: Cleaning up...")
	if err := registry.Close(); err != nil {
		log.Fatalf("Failed to close registry: %v", err)
	}
	fmt.Println("✓ Registry closed\n")

	fmt.Println("✅ All tests passed!")
	fmt.Println("\nMCP Integration Status:")
	fmt.Println("  ✓ Server registration")
	fmt.Println("  ✓ Client initialization")
	fmt.Println("  ✓ Tool discovery")
	fmt.Println("  ✓ Tool execution (create_entities)")
	fmt.Println("  ✓ Tool execution (read_graph)")
	fmt.Println("  ✓ Cleanup")
}
