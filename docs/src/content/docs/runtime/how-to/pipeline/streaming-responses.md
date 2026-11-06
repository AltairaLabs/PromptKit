---
title: Stream Responses
description: Read a model reply as it is generated, from a stage pipeline or straight from a provider.
sidebar:
  order: 6
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - runtime/pipeline/stage/builder.go
    - runtime/pipeline/stage/config.go
    - runtime/pipeline/stage/element.go
    - runtime/pipeline/stage/pipeline.go
    - runtime/pipeline/stage/stages_provider.go
    - runtime/providers/provider.go
    - runtime/providers/streaming.go
    - runtime/types/message.go
---
Read an LLM reply fragment by fragment while the model generates it.

## Prerequisites

- A provider that supports streaming. Check with `provider.SupportsStreaming()`.
- A stage pipeline that contains a `ProviderStage`, built as in [Tutorial 1](/runtime/tutorials/01-first-pipeline/).

A `ProviderStage` streams on its own when its provider supports it. It emits each fragment as a text element on the pipeline output, then emits the complete assistant message.

## Stream through a pipeline

1. Send the request on an input channel and close the channel. `Execute` returns the output channel without waiting for the reply.

   ```go
   input := make(chan stage.StreamElement, 1)
   input <- stage.NewMessageElement(&types.Message{Role: "user", Content: prompt})
   close(input)

   output, err := pipe.Execute(ctx, input)
   if err != nil {
   	return err
   }
   ```

2. Range over the output channel. A text element (`elem.Text`) carries one fragment, not the accumulated reply. A message element (`elem.Message`) carries the complete assistant message with its token counts and cost.

3. Check `elem.Error` in the loop. `Execute` returns an error only when the execution cannot start: the pipeline is shutting down, or `ctx` ended while `Execute` waited for a free execution slot. A failure during the call arrives as an element with `Error` set.

The loop ends when the pipeline closes the output channel. Keep reading until it closes, even after you stop using the elements: the pipeline blocks on an output channel nobody reads.

The complete function:

```go
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// streamReply prints the reply as it arrives and returns the full text.
func streamReply(ctx context.Context, pipe *stage.StreamPipeline, prompt string) (string, error) {
	input := make(chan stage.StreamElement, 1)
	input <- stage.NewMessageElement(&types.Message{Role: "user", Content: prompt})
	close(input)

	output, err := pipe.Execute(ctx, input)
	if err != nil {
		return "", err
	}

	var reply strings.Builder
	var streamErr error
	for elem := range output {
		switch {
		case elem.Error != nil:
			streamErr = elem.Error
		case elem.Text != nil:
			fmt.Print(*elem.Text)
			reply.WriteString(*elem.Text)
		case elem.Message != nil && elem.Message.Role == "assistant" && elem.Message.CostInfo != nil:
			fmt.Printf("\n%d tokens out\n", elem.Message.CostInfo.OutputTokens)
		}
	}
	return reply.String(), streamErr
}
```

If the stream fails partway, `reply` holds the text received before the failure.

## Stop a stream early

Cancel the context you passed to `Execute`. The stages stop, and the output channel closes after the elements already in flight. Keep ranging over the channel to the end.

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

output, err := pipe.Execute(ctx, input)
if err != nil {
	return err
}

seen := 0
for elem := range output {
	if elem.Text == nil {
		continue
	}
	fmt.Print(*elem.Text)
	if seen++; seen == 10 {
		cancel() // stop generating; keep draining
	}
}
```

## Set timeouts

Pass a pipeline configuration to the builder. `IdleTimeout` cancels a stream that stops producing chunks and resets on every chunk. `ExecutionTimeout` caps the whole run.

```go
cfg := stage.DefaultPipelineConfig().
	WithIdleTimeout(15 * time.Second).
	WithExecutionTimeout(2 * time.Minute)

pipe, err := stage.NewPipelineBuilder().
	WithConfig(cfg).
	Chain(stage.NewProviderStage(provider, nil, nil, &stage.ProviderConfig{MaxTokens: 500})).
	Build()
```

`DefaultPipelineConfig` sets a 30-second idle timeout and no execution timeout. A `context.WithTimeout` on the context passed to `Execute` also ends the stream.

## Serve a stream over HTTP

Write each fragment as a server-sent event and flush after each one. Encode the fragment as JSON so newlines in the text do not break the event framing.

```go
func handleStream(pipe *stage.StreamPipeline) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")

		input := make(chan stage.StreamElement, 1)
		input <- stage.NewMessageElement(&types.Message{Role: "user", Content: r.URL.Query().Get("prompt")})
		close(input)

		output, err := pipe.Execute(r.Context(), input)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		for elem := range output {
			switch {
			case elem.Error != nil:
				data, _ := json.Marshal(elem.Error.Error())
				fmt.Fprintf(w, "event: error\ndata: %s\n\n", data)
			case elem.Text != nil:
				data, _ := json.Marshal(*elem.Text)
				fmt.Fprintf(w, "data: %s\n\n", data)
			}
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}
}
```

`r.Context()` ends when the client disconnects, which cancels the stream.

## Stream from a provider without a pipeline

`PredictStream` returns a channel of `providers.StreamChunk`. `Delta` holds the new text, `Content` the text so far, and `Error` a failure. The final chunk has `FinishReason` set and carries `CostInfo`.

```go
chunks, err := provider.PredictStream(ctx, providers.PredictionRequest{
	Messages:  []types.Message{{Role: "user", Content: prompt}},
	MaxTokens: 500,
})
if err != nil {
	return err
}

for chunk := range chunks {
	if chunk.Error != nil {
		return chunk.Error
	}
	fmt.Print(chunk.Delta)
}
```

A provider called this way bypasses the pipeline, so its idle timeout, history and hooks do not apply. Pass a context with a deadline.

## Troubleshoot

| Symptom | Cause | Fix |
|---|---|---|
| The reply arrives all at once | The provider does not stream, so no text elements precede the message | Check `provider.SupportsStreaming()`, or read `elem.Message.Content` |
| The program hangs after cancel | The output channel is not drained | Keep ranging until the channel closes |
| The stream ends early with `Error` set | The idle timeout fired or the provider failed | Raise `IdleTimeout`, or read the error |

## Related

- [Handle Errors](/runtime/how-to/pipeline/handle-errors/)
- [Configure Pipeline](/runtime/how-to/pipeline/configure-pipeline/)
- [Pipeline Reference](/runtime/reference/pipeline/)
- [Providers Reference](/runtime/reference/providers/)
