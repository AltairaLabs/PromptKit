package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/mcp"
)

func main() {
	fmt.Println("=== MCP Filesystem Server Integration Test ===\n")

	// Create test workspace
	workspace := "./test_workspace"
	if err := os.MkdirAll(workspace, 0755); err != nil {
		fmt.Printf("Failed to create workspace: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(workspace) // Cleanup

	absWorkspace, _ := filepath.Abs(workspace)
	fmt.Printf("Test workspace: %s\n\n", absWorkspace)

	// Test 1: Basic Connection
	fmt.Println("Test 1: Basic Connection and Tool Discovery")
	fmt.Println("=" + string(make([]byte, 50)) + "=")
	if err := testBasicConnection(absWorkspace); err != nil {
		fmt.Printf("❌ Test failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ Test 1 passed\n")

	// Test 2: File Operations
	fmt.Println("Test 2: File Read/Write Operations")
	fmt.Println("=" + string(make([]byte, 50)) + "=")
	if err := testFileOperations(absWorkspace); err != nil {
		fmt.Printf("❌ Test failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ Test 2 passed\n")

	// Test 3: Error Handling
	fmt.Println("Test 3: Error Handling and Timeout")
	fmt.Println("=" + string(make([]byte, 50)) + "=")
	if err := testErrorHandling(absWorkspace); err != nil {
		fmt.Printf("❌ Test failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ Test 3 passed\n")

	fmt.Println("=== All Tests Passed! ===")
}

func testBasicConnection(workspace string) error {
	// Create MCP registry
	registry := mcp.NewRegistry()
	defer registry.Close()

	// Register filesystem server
	config := mcp.ServerConfig{
		Name:    "filesystem",
		Command: "npx",
		Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", workspace},
		Env: map[string]string{
			"PATH": os.Getenv("PATH"),
		},
	}

	if err := registry.RegisterServer(config); err != nil {
		return fmt.Errorf("failed to register server: %w", err)
	}

	// Get client (lazy initialization happens here)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := registry.GetClient(ctx, "filesystem")
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("failed to list tools: %w", err)
	}

	fmt.Printf("Discovered %d tools:\n", len(tools))
	for _, tool := range tools {
		fmt.Printf("  - %s: %s\n", tool.Name, tool.Description)
	}

	// Verify expected tools exist
	expectedTools := []string{"read_file", "write_file", "list_directory"}
	for _, expected := range expectedTools {
		found := false
		for _, tool := range tools {
			if tool.Name == expected {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("expected tool not found: %s", expected)
		}
	}

	return nil
}

func testFileOperations(workspace string) error {
	// Create registry and client
	registry := mcp.NewRegistry()
	defer registry.Close()

	config := mcp.ServerConfig{
		Name:    "filesystem",
		Command: "npx",
		Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", workspace},
		Env:     map[string]string{"PATH": os.Getenv("PATH")},
	}

	if err := registry.RegisterServer(config); err != nil {
		return err
	}

	ctx := context.Background()
	client, err := registry.GetClient(ctx, "filesystem")
	if err != nil {
		return err
	}

	// Write a test file
	fmt.Println("Writing test file...")
	testContent := "Hello from PromptKit MCP integration!"
	testFilePath := filepath.Join(workspace, "test.txt")

	writeArgs, _ := json.Marshal(map[string]interface{}{
		"path":    testFilePath,
		"content": testContent,
	})

	writeResp, err := client.CallTool(ctx, "write_file", writeArgs)
	if err != nil {
		return fmt.Errorf("write_file failed: %w", err)
	}
	fmt.Printf("Write result: %s\n", string(writeResp.Content[0].Text))

	// Read the file back
	fmt.Println("Reading test file...")
	readArgs, _ := json.Marshal(map[string]interface{}{
		"path": testFilePath,
	})

	readResp, err := client.CallTool(ctx, "read_file", readArgs)
	if err != nil {
		return fmt.Errorf("read_file failed: %w", err)
	}

	// Verify content
	if len(readResp.Content) == 0 {
		return fmt.Errorf("read returned no content")
	}
	readContent := readResp.Content[0].Text
	fmt.Printf("Read content: %s\n", readContent)

	if readContent != testContent {
		return fmt.Errorf("content mismatch: expected %q, got %q", testContent, readContent)
	}

	// List directory
	fmt.Println("Listing directory...")
	listArgs, _ := json.Marshal(map[string]interface{}{
		"path": workspace,
	})

	listResp, err := client.CallTool(ctx, "list_directory", listArgs)
	if err != nil {
		return fmt.Errorf("list_directory failed: %w", err)
	}
	fmt.Printf("Directory listing: %s\n", string(listResp.Content[0].Text))

	return nil
}

func testErrorHandling(workspace string) error {
	// Test with custom timeout options
	options := mcp.ClientOptions{
		RequestTimeout:            5 * time.Second,
		InitTimeout:               5 * time.Second,
		MaxRetries:                2,
		RetryDelay:                100 * time.Millisecond,
		EnableGracefulDegradation: true,
	}

	registry := mcp.NewRegistry()
	defer registry.Close()

	config := mcp.ServerConfig{
		Name:    "filesystem",
		Command: "npx",
		Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", workspace},
		Env:     map[string]string{"PATH": os.Getenv("PATH")},
	}

	if err := registry.RegisterServer(config); err != nil {
		return err
	}

	// Create client with custom options
	client := mcp.NewStdioClientWithOptions(config, options)
	ctx := context.Background()

	// Initialize
	if _, err := client.Initialize(ctx); err != nil {
		return fmt.Errorf("initialization failed: %w", err)
	}
	defer client.Close()

	// Test 1: Nonexistent file
	fmt.Println("Testing nonexistent file...")
	invalidArgs, _ := json.Marshal(map[string]interface{}{
		"path": filepath.Join(workspace, "nonexistent.txt"),
	})

	_, err := client.CallTool(ctx, "read_file", invalidArgs)
	if err != nil {
		fmt.Printf("  Expected error received: %v\n", err)
	} else {
		fmt.Println("  Note: Server returned success for nonexistent file (may be server behavior)")
	}

	// Test 2: Context timeout
	fmt.Println("Testing context timeout...")
	shortCtx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(10 * time.Millisecond) // Ensure timeout fires

	validArgs, _ := json.Marshal(map[string]interface{}{
		"path": filepath.Join(workspace, "test.txt"),
	})

	_, err = client.CallTool(shortCtx, "read_file", validArgs)
	if err == nil {
		return fmt.Errorf("expected timeout error, got success")
	}
	fmt.Printf("  Expected timeout error: %v\n", err)

	// Test 3: Graceful degradation
	fmt.Println("Testing graceful degradation...")
	tools, err := client.ListTools(context.Background())
	if err != nil {
		return fmt.Errorf("ListTools should not fail with graceful degradation: %w", err)
	}
	fmt.Printf("  Successfully listed %d tools with graceful degradation enabled\n", len(tools))

	return nil
}
