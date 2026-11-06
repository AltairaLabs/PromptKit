---
title: Handle errors
description: Detect, classify and recover from a failed pipeline turn.
sidebar:
  order: 5
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - runtime/pipeline/stage/builder.go
    - runtime/pipeline/stage/config.go
    - runtime/pipeline/stage/errors.go
    - runtime/pipeline/stage/pipeline.go
    - runtime/pipeline/stage/stages_provider.go
    - runtime/providers/errors.go
    - runtime/providers/stream_retry.go
---

Detect why a pipeline turn failed, and retry it or fall back to another provider.

## Prerequisites

- A built `*stage.StreamPipeline` with a provider stage. See [Configure a pipeline](/runtime/how-to/pipeline/configure-pipeline/).

The snippets use this helper:

```go
func send(ctx context.Context, pipe *stage.StreamPipeline, content string) (*stage.ExecutionResult, error) {
	msg := types.Message{Role: "user"}
	msg.AddTextPart(content)
	return pipe.ExecuteSync(ctx, stage.NewMessageElement(&msg))
}
```

## 1. Read the error

A stage that returns an error ends the turn. `ExecuteSync` returns that error with a result holding whatever the pipeline emitted before the failure. The result is nil only when the pipeline could not start.

```go
result, err := send(ctx, pipe, "Your message")
if err != nil {
	var stageErr *stage.StageError
	if errors.As(err, &stageErr) {
		log.Printf("stage %q (%s) failed: %v", stageErr.StageName, stageErr.StageType, stageErr.Err)
	}
	if result != nil && result.Response != nil {
		log.Printf("partial response: %s", result.Response.Content)
	}
	return err
}
```

A stage that returns an error without emitting it (a validation or template stage, for example) reaches you as a `*stage.StageError`. The provider stage emits its own error, so a provider failure reaches you as the provider's error, without the wrapper. Match on the cause with `errors.Is` and `errors.As`, which work in both cases.

## 2. Classify the error

```go
var httpErr *providers.ProviderHTTPError
switch {
case errors.Is(err, context.DeadlineExceeded):
	// The context deadline or the pipeline's ExecutionTimeout passed.
case errors.Is(err, stage.ErrMaxRoundsExceeded):
	// The model was still calling tools at the round limit. Send another message to continue.
case errors.As(err, &httpErr) && (httpErr.StatusCode == http.StatusUnauthorized || httpErr.StatusCode == http.StatusForbidden):
	// Check the API key. Do not retry.
case providers.IsTransient(err):
	// 429, 502, 503, 504 or a connection failure. Retry.
}
```

`providers.IsTransient` is false for context cancellation and deadlines, for stream idle timeouts, and for a provider credential that could not be applied.

## 3. Retry transient failures

Retry only errors that `providers.IsTransient` accepts, and stop when the context is done:

```go
func sendWithRetry(ctx context.Context, pipe *stage.StreamPipeline, content string, maxAttempts int) (*stage.ExecutionResult, error) {
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		var result *stage.ExecutionResult
		result, err = send(ctx, pipe, content)
		if err == nil {
			return result, nil
		}
		if !providers.IsTransient(err) {
			return result, err
		}
		select {
		case <-time.After(time.Duration(1<<attempt) * time.Second):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, fmt.Errorf("failed after %d attempts: %w", maxAttempts, err)
}
```

A provider's `ProviderHTTPError.RetryAfter` holds the delay a `Retry-After` header asked for. Use it instead of the computed backoff when it is non-zero.

## 4. Fall back to another provider (optional)

Build one pipeline per provider and try them in order:

```go
func sendWithFallback(ctx context.Context, pipes []*stage.StreamPipeline, content string) (*stage.ExecutionResult, error) {
	var errs []error
	for _, pipe := range pipes {
		result, err := send(ctx, pipe, content)
		if err == nil {
			return result, nil
		}
		errs = append(errs, err)
	}
	return nil, errors.Join(errs...)
}
```

## 5. Handle errors while streaming (optional)

`Execute` returns an error only when it cannot start (for example, while the pipeline shuts down). A failure during the turn arrives as the last element on the output channel, with `Error` set:

```go
output, err := pipe.Execute(ctx, input)
if err != nil {
	return err
}
var accumulated string
for elem := range output {
	if elem.Text != nil {
		accumulated += *elem.Text
	}
	if elem.Error != nil {
		log.Printf("partial %q, error: %v", accumulated, elem.Error)
		return elem.Error
	}
}
```

## 6. Bound a turn (optional)

Two pipeline settings bound a turn. When `ExecutionTimeout` passes, the running stage fails with an error that wraps both `stage.ErrExecutionTimeout` and `context.DeadlineExceeded`. When `IdleTimeout` fires, the error wraps `stage.ErrIdleTimeout`. Either way the turn fails and `ExecuteSync` returns the error. Only a cancellation of your own context ends a turn without one.

```go
cfg := stage.DefaultPipelineConfig().
	WithExecutionTimeout(2 * time.Minute). // hard ceiling per execution; disabled by default
	WithIdleTimeout(30 * time.Second)      // inactivity limit; 30 seconds by default
pipe, err := stage.NewPipelineBuilderWithConfig(cfg).Chain(providerStage).Build()
```

## Verify

Point the pipeline at a provider that fails, such as an invalid API key. `send` returns a non-nil error, and the classification switch takes the `401` branch. Then run a valid request and confirm `err` is nil.

## Troubleshoot

Persistent timeouts: raise `ExecutionTimeout` or the context deadline, or lower `MaxTokens` in the provider defaults.

Frequent rate-limit errors: add the retry above, and lower `WithMaxConcurrentPipelines`.

A turn fails with `ErrTemplateUnresolved`: the system prompt references a variable nothing supplied. The error names the variable. Supply it, or remove it from the prompt.

Release resources when you finish with them: call `provider.Close()` and `pipe.Shutdown(ctx)`.

## Related

- [Stream responses](/runtime/how-to/pipeline/streaming-responses/)
- [Monitor costs](/runtime/how-to/observability/monitor-costs/)
- [Set up providers](/runtime/how-to/providers/setup-providers/)
- [Pipeline reference](/runtime/reference/pipeline/)
- [Providers reference](/runtime/reference/providers/)
