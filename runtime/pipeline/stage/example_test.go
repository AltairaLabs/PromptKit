package stage_test

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ExamplePipelineBuilder demonstrates building a simple linear pipeline.
func ExamplePipelineBuilder() {
	// Create some simple stages
	inputStage := stage.NewPassthroughStage("input")
	processStage := stage.NewPassthroughStage("process")
	outputStage := stage.NewPassthroughStage("output")

	// Build a linear pipeline
	pipeline, err := stage.NewPipelineBuilder().
		Chain(inputStage, processStage, outputStage).
		Build()

	if err != nil {
		fmt.Printf("Error building pipeline: %v\n", err)
		return
	}

	// Create input channel with a message
	input := make(chan stage.StreamElement, 1)
	input <- stage.NewMessageElement(&types.Message{
		Role:    "user",
		Content: "Hello, world!",
	})
	close(input)

	// Execute pipeline
	ctx := context.Background()
	output, err := pipeline.Execute(ctx, input)
	if err != nil {
		fmt.Printf("Error executing pipeline: %v\n", err)
		return
	}

	// Consume output
	for elem := range output {
		if elem.Message != nil {
			fmt.Printf("Received message: %s\n", elem.Message.Content)
		}
	}
	// Output: Received message: Hello, world!
}

// ExampleStreamElement demonstrates creating different types of stream elements.
func ExampleStreamElement() {
	// Text element
	textElem := stage.NewTextElement("Hello")
	fmt.Printf("Text element: %v\n", *textElem.Text)

	// Message element
	msgElem := stage.NewMessageElement(&types.Message{
		Role:    "user",
		Content: "Hello",
	})
	fmt.Printf("Message element: %s\n", msgElem.Message.Content)

	// Error element
	errElem := stage.NewErrorElement(fmt.Errorf("test error"))
	fmt.Printf("Error element: %v\n", errElem.Error)

	// Output:
	// Text element: Hello
	// Message element: Hello
	// Error element: test error
}

// ExamplePipelineBuilder_withConfig demonstrates building a pipeline with custom configuration.
func ExamplePipelineBuilder_withConfig() {
	// Create custom config
	config := stage.DefaultPipelineConfig().
		WithChannelBufferSize(32).
		WithPriorityQueue(true).
		WithMetrics(true)

	// Build pipeline with config
	pipeline, err := stage.NewPipelineBuilderWithConfig(config).
		Chain(
			stage.NewPassthroughStage("stage1"),
			stage.NewPassthroughStage("stage2"),
		).
		Build()

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Pipeline created with %d stages\n", 2)
	_ = pipeline
	// Output: Pipeline created with 2 stages
}

// ExampleMapStage demonstrates using a map stage to transform elements.
func ExampleMapStage() {
	// Create a map stage that uppercases text
	uppercaseStage := stage.NewMapStage("uppercase", func(elem stage.StreamElement) (stage.StreamElement, error) {
		if elem.Message != nil {
			msg := *elem.Message
			msg.Content = "TRANSFORMED: " + msg.Content
			elem.Message = &msg
		}
		return elem, nil
	})

	// Build pipeline
	pipeline, _ := stage.NewPipelineBuilder().
		Chain(uppercaseStage).
		Build()

	// Execute
	input := make(chan stage.StreamElement, 1)
	input <- stage.NewMessageElement(&types.Message{
		Role:    "user",
		Content: "hello",
	})
	close(input)

	output, _ := pipeline.Execute(context.Background(), input)
	for elem := range output {
		if elem.Message != nil {
			fmt.Printf("%s\n", elem.Message.Content)
		}
	}
	// Output: TRANSFORMED: hello
}
