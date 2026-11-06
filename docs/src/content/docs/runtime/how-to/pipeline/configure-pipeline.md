---
title: Configure Pipeline
description: Build a stage-based runtime pipeline, tune its timeouts and buffers, and add prompt assembly, hooks, state and voice stages
sidebar:
  order: 1
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - runtime/audio/interruption.go
    - runtime/hooks/guardrails/factory.go
    - runtime/hooks/registry.go
    - runtime/pipeline/stage/builder.go
    - runtime/pipeline/stage/config.go
    - runtime/pipeline/stage/errors.go
    - runtime/pipeline/stage/pipeline.go
    - runtime/pipeline/stage/stages_core.go
    - runtime/pipeline/stage/stages_duplex_provider_session.go
    - runtime/pipeline/stage/stages_provider.go
    - runtime/pipeline/stage/stages_speech.go
    - runtime/pipeline/stage/stages_utilities.go
    - runtime/pipeline/types.go
    - runtime/providers/claude/claude.go
    - runtime/providers/gemini/gemini.go
    - runtime/providers/mock/mock.go
    - runtime/providers/mock/mock_repository.go
    - runtime/providers/openai/openai.go
    - runtime/providers/openai/realtime_session_integration.go
    - runtime/statestore/memory.go
    - runtime/statestore/redis.go
---

Build a stage-based pipeline that sends a message to an LLM provider, then tune it and add the stages your application needs.

## Prerequisites

- Go 1.26 or later
- An API key for your LLM provider
- A `prompt.Registry` if you assemble prompts from templates

## 1. Create a provider

```go
provider := openai.NewProvider(
    "openai",
    "gpt-4o-mini",
    "", // default base URL
    providers.ProviderDefaults{Temperature: 0.7, MaxTokens: 2000},
    false, // omit raw provider output
)
defer provider.Close()
```

`claude.NewProvider` and `gemini.NewProvider` take the same arguments.

## 2. Build the pipeline

```go
pipe, err := stage.NewPipelineBuilder().
    Chain(
        stage.NewProviderStage(provider, nil, nil, &stage.ProviderConfig{
            MaxTokens:   1500,
            Temperature: 0.7,
        }),
    ).
    Build()
if err != nil {
    log.Fatal(err)
}
defer pipe.Shutdown(context.Background())
```

`NewProviderStage` takes a tool registry and a tool policy; pass `nil` for both when the pipeline calls no tools.

## 3. Run it

```go
msg := types.Message{Role: "user"}
msg.AddTextPart("What is 2+2?")

input := make(chan stage.StreamElement, 1)
input <- stage.NewMessageElement(&msg)
close(input)

output, err := pipe.Execute(ctx, input)
if err != nil {
    log.Fatalf("execute: %v", err)
}
for elem := range output {
    if elem.Error != nil {
        log.Fatalf("pipeline: %v", elem.Error)
    }
    if elem.Text != nil {
        log.Printf("response: %s", *elem.Text)
    }
}
```

## 4. Tune the pipeline

`DefaultPipelineConfig` sets a channel buffer of 32, an idle timeout of 30 seconds, no execution timeout, a graceful shutdown timeout of 10 seconds and at most 100 concurrent executions of one pipeline. Past that limit `Execute` waits for a running execution to finish, or returns the context's error if the context ends first. `WithMaxConcurrentPipelines(0)` removes the limit.

```go
config := stage.DefaultPipelineConfig().
    WithChannelBufferSize(64).
    WithIdleTimeout(60 * time.Second).
    WithExecutionTimeout(5 * time.Minute). // hard wall-clock limit
    WithGracefulShutdownTimeout(15 * time.Second)

pipe, err := stage.NewPipelineBuilderWithConfig(config).
    Chain(stages...).
    Build()
```

Set sampling options per provider stage on `stage.ProviderConfig` (`MaxTokens`, `Temperature`, `TopP`, `Seed` and others).

## 5. Add prompt assembly

Create one `TurnState` and give it to every stage that reads or writes it. `Build` rejects a prompt assembly or template stage that has none, and returns an error that wraps `stage.ErrTurnStateRequired`.

```go
turnState := stage.NewTurnState()

pipe, err := stage.NewPipelineBuilder().
    Chain(
        stage.NewPromptAssemblyStageWithTurnState(promptRegistry, "chat", variables, turnState),
        stage.NewTemplateStageWithTurnState(nil, turnState),
        stage.NewProviderStageWithTurnState(provider, nil, nil, providerConfig, nil, nil, turnState),
    ).
    Build()
```

The `nil` arguments are the event emitter and, for the provider stage, the hook registry.

## 6. Add guardrail hooks

Build the hook registry and pass it to the provider stage:

```go
bannedWords, err := guardrails.NewGuardrailHook("banned_words", map[string]any{
    "words": []any{"inappropriate"},
})
if err != nil {
    log.Fatal(err)
}
maxLength, err := guardrails.NewGuardrailHook("length", map[string]any{
    "max_characters": 2000,
})
if err != nil {
    log.Fatal(err)
}

hookRegistry := hooks.NewRegistry(
    hooks.WithProviderHook(bannedWords),
    hooks.WithProviderHook(maxLength),
)

providerStage := stage.NewProviderStageWithTurnState(
    provider, nil, nil, providerConfig, nil, hookRegistry, turnState,
)
```

## 7. Persist conversation state

Load history before prompt assembly and save after the provider. `statestore.NewRedisStore` takes a `*redis.Client` for shared storage.

```go
stateConfig := &pipeline.StateStoreConfig{
    Store:          statestore.NewMemoryStore(),
    ConversationID: "session-123",
}

pipe, err := stage.NewPipelineBuilder().
    Chain(
        stage.NewStateStoreLoadStageWithTurnState(stateConfig, turnState),
        stage.NewPromptAssemblyStageWithTurnState(promptRegistry, "chat", variables, turnState),
        stage.NewTemplateStageWithTurnState(nil, turnState),
        providerStage,
        stage.NewIncrementalSaveStage(&stage.IncrementalSaveConfig{StateStoreConfig: stateConfig}),
    ).
    Build()
```

The load stage copies `ConversationID` and `UserID` from `StateStoreConfig` into `TurnState.ConversationID` and `TurnState.UserID`.

## 8. Pass session metadata

The provider stage copies `TurnState.ProviderRequestMetadata` into `PredictionRequest.Metadata` on every call:

```go
turnState.ProviderRequestMetadata = map[string]interface{}{
    "session_id": sessionID,
    "tenant_id":  tenantID,
}
```

No built-in provider reads these keys; a custom provider can.

## 9. Choose a voice mode

For a text LLM with speech in and out (VAD mode), chain the audio turn, STT and TTS stages around the provider stage. One interruption handler shared by the three stages decides when the caller's speech interrupts the reply.

```go
interruptionHandler := audio.NewInterruptionHandler(audio.InterruptionImmediate, nil)

audioTurn, err := stage.NewAudioTurnStage(stage.AudioTurnConfig{
    SilenceDuration:     800 * time.Millisecond,
    MinSpeechDuration:   200 * time.Millisecond,
    MaxTurnDuration:     30 * time.Second,
    SampleRate:          16000,
    EmitEndOfTurn:       true, // run the model once per turn
    InterruptionHandler: interruptionHandler,
})
if err != nil {
    return err
}

sttConfig := stage.DefaultSTTStageConfig()
sttConfig.InterruptionHandler = interruptionHandler

ttsConfig := stage.DefaultTTSStageWithInterruptionConfig()
ttsConfig.Voice = "alloy"
ttsConfig.InterruptionHandler = interruptionHandler

providerConfig.Streaming = true // one tool loop per end of turn

pipe, err := stage.NewPipelineBuilder().
    Chain(
        audioTurn,
        stage.NewSTTStage(sttService, sttConfig),
        stage.NewStateStoreLoadStageWithTurnState(stateConfig, turnState),
        stage.NewPromptAssemblyStageWithTurnState(promptRegistry, "chat", variables, turnState),
        stage.NewTemplateStageWithTurnState(nil, turnState),
        stage.NewProviderStageWithTurnState(provider, nil, nil, providerConfig, nil, nil, turnState),
        stage.NewIncrementalSaveStage(&stage.IncrementalSaveConfig{StateStoreConfig: stateConfig}),
        stage.NewTTSStageWithInterruption(ttsService, ttsConfig),
    ).
    Build()
```

For a native duplex model such as Gemini Live, replace the audio, STT, provider and TTS stages with a duplex provider stage. The provider must implement `providers.StreamInputSupport`:

```go
duplex := stage.NewDuplexProviderStageWithTurnState(
    provider, &providers.StreamingInputConfig{}, nil, turnState,
)
```

## Verify

Swap in the mock provider and run step 3. The pipeline prints the mock response without calling an API:

```go
provider := mock.NewProviderWithRepository("test", "test-model", false,
    mock.NewInMemoryMockRepository("test output"))
```

## Troubleshooting

If executions time out, raise the idle timeout. It resets on each provider call, round completion and streaming chunk, and stays open while a tool call runs, so a slow tool does not trip it. Bound slow tools with the tool's own `TimeoutMs`. For a hard limit, use `WithExecutionTimeout`.

```go
config := stage.DefaultPipelineConfig().WithIdleTimeout(120 * time.Second)
```

If a slow consumer blocks producers, raise `WithChannelBufferSize`.

## Related

- [Set up providers](/runtime/how-to/providers/setup-providers/)
- [Handle errors](/runtime/how-to/pipeline/handle-errors/)
- [Stream responses](/runtime/how-to/pipeline/streaming-responses/)
- [Pipeline reference](/runtime/reference/pipeline/)
- [Pipeline tutorial](/runtime/tutorials/01-first-pipeline/)
- [Stage design](/runtime/explanation/stage-design/)
