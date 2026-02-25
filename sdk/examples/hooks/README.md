# Hooks & Guardrails Example

Demonstrates how to use SDK hooks to enforce guardrails on LLM responses.

## What You'll Learn

- Registering built-in guardrails (`BannedWordsHook`, `LengthHook`) via `sdk.WithProviderHook()`
- Writing a custom `ProviderHook` with `ChunkInterceptor` for streaming support
- Detecting `HookDeniedError` with `errors.As` for graceful error handling
- Streaming with chunk-level guardrail enforcement

## Prerequisites

- Go 1.21+
- OpenAI API key

## Running the Example

```bash
export OPENAI_API_KEY=your-key
go run .
```

## How It Works

Hooks are registered as SDK options when opening a conversation:

```go
conv, err := sdk.Open("./hooks.pack.json", "chat",
    sdk.WithProviderHook(guardrails.NewBannedWordsHook([]string{"password", "secret"})),
    sdk.WithProviderHook(guardrails.NewLengthHook(500, 0)),
    sdk.WithProviderHook(NewPIIHook()),
)
```

Hooks execute in registration order. The first denial short-circuits — subsequent hooks are skipped. When a hook denies a response, `Send()` returns either a `*hooks.HookDeniedError` (non-streaming AfterCall) or a `*providers.ValidationAbortError` (streaming chunk interceptor):

```go
resp, err := conv.Send(ctx, "What is a good default password?")
if err != nil {
    var denied *hooks.HookDeniedError
    var aborted *providers.ValidationAbortError
    if errors.As(err, &denied) {
        fmt.Printf("Blocked: %s\n", denied.Reason)
    } else if errors.As(err, &aborted) {
        fmt.Printf("Blocked during streaming: %s\n", aborted.Reason)
    }
}
```

Hooks that implement `ChunkInterceptor` also enforce guardrails during streaming, checking each chunk as it arrives.

## Next Steps

- [Streaming Example](../streaming/) - Response streaming basics
- [Tools Example](../tools/) - Function calling
- [HITL Example](../hitl/) - Human-in-the-loop approval
