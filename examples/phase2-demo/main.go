package main

import (
	"context"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// Simple demo middleware that logs execution
type loggingMiddleware struct {
	name string
}

func (m *loggingMiddleware) Process(ctx *pipeline.ExecutionContext, next func() error) error {
	fmt.Printf("[%s] Processing request\n", m.name)

	// Continue to next middleware
	err := next()

	fmt.Printf("[%s] Completed processing\n", m.name)
	return err
}

func (m *loggingMiddleware) StreamChunk(ctx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	// No-op for demo
	return nil
}

func main() {
	fmt.Println("=== Phase 2 Demo: Scalability Controls ===\n")

	// Example 1: Default Configuration
	fmt.Println("1. Pipeline with default configuration:")
	defaultPipeline := pipeline.NewPipeline(
		&loggingMiddleware{name: "Logger1"},
		&loggingMiddleware{name: "Logger2"},
	)

	result, err := defaultPipeline.Execute(context.Background(), "user", "Hello!")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Success! Messages: %d\n", len(result.Messages))
	}
	fmt.Println()

	// Example 2: Custom Configuration
	fmt.Println("2. Pipeline with custom configuration:")
	customConfig := &pipeline.PipelineRuntimeConfig{
		MaxConcurrentExecutions: 50,
		StreamBufferSize:        200,
		ExecutionTimeout:        15 * time.Second,
		GracefulShutdownTimeout: 5 * time.Second,
	}

	customPipeline := pipeline.NewPipelineWithConfig(customConfig,
		&loggingMiddleware{name: "CustomLogger"},
	)

	result, err = customPipeline.Execute(context.Background(), "user", "Custom config test")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Success with custom config! Messages: %d\n", len(result.Messages))
	}
	fmt.Println()

	// Example 3: Concurrent Executions
	fmt.Println("3. Testing concurrent executions (max 5):")
	concurrentConfig := &pipeline.PipelineRuntimeConfig{
		MaxConcurrentExecutions: 5,
		StreamBufferSize:        100,
		ExecutionTimeout:        30 * time.Second,
		GracefulShutdownTimeout: 10 * time.Second,
	}

	concurrentPipeline := pipeline.NewPipelineWithConfig(concurrentConfig,
		&loggingMiddleware{name: "Concurrent"},
	)

	// Launch 10 concurrent executions (will be limited to 5 at a time)
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			_, err := concurrentPipeline.Execute(context.Background(), "user", fmt.Sprintf("Request %d", id))
			if err != nil {
				fmt.Printf("Request %d failed: %v\n", id, err)
			}
			done <- true
		}(i)
	}

	// Wait for all to complete
	for i := 0; i < 10; i++ {
		<-done
	}
	fmt.Println("All concurrent executions completed!")
	fmt.Println()

	// Example 4: Graceful Shutdown
	fmt.Println("4. Testing graceful shutdown:")
	shutdownPipeline := pipeline.NewPipeline(&loggingMiddleware{name: "Shutdown"})

	// Execute a request
	_, _ = shutdownPipeline.Execute(context.Background(), "user", "Before shutdown")

	// Shutdown the pipeline
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = shutdownPipeline.Shutdown(shutdownCtx)
	if err != nil {
		fmt.Printf("Shutdown error: %v\n", err)
	} else {
		fmt.Println("Pipeline shutdown successfully!")
	}

	// Try to use after shutdown
	_, err = shutdownPipeline.Execute(context.Background(), "user", "After shutdown")
	if err != nil {
		fmt.Printf("Expected error after shutdown: %v\n", err)
	}
	fmt.Println()

	fmt.Println("=== Phase 2 Demo Complete ===")
	fmt.Println("\nPhase 2 Features Demonstrated:")
	fmt.Println("✓ Default configuration with sensible defaults")
	fmt.Println("✓ Custom configuration for different environments")
	fmt.Println("✓ Semaphore-based concurrency control")
	fmt.Println("✓ Graceful shutdown with timeout")
	fmt.Println("✓ Execution rejection after shutdown")
}
