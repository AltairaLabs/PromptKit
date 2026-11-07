---
title: Setup Providers
description: Connect the runtime to OpenAI, Claude, Gemini, Azure OpenAI, Ollama, vLLM and the mock provider.
sidebar:
  order: 2
verified:
  commit: dc6d6f37cfabc0079dd9af47c52502e83f2a85af
  sources:
    - runtime/credentials/azure.go
    - runtime/credentials/azure_integration.go
    - runtime/credentials/credential.go
    - runtime/providers/base/llmpricing.go
    - runtime/providers/claude/claude.go
    - runtime/providers/claude/claude_tools.go
    - runtime/providers/claude/pricing_table.go
    - runtime/providers/extra_body.go
    - runtime/providers/gemini/gemini.go
    - runtime/providers/gemini/pricing_table.go
    - runtime/providers/mock/mock.go
    - runtime/providers/mock/mock_repository.go
    - runtime/providers/ollama/ollama.go
    - runtime/providers/ollama/ollama_tools.go
    - runtime/providers/openai/openai.go
    - runtime/providers/openai/openai_tools.go
    - runtime/providers/openai/pricing_table.go
    - runtime/providers/provider.go
    - runtime/providers/vllm/vllm.go
    - runtime/providers/vllm/vllm_tools.go
    - runtime/types/message.go
---
Create runtime providers for OpenAI, Claude, Gemini, Azure OpenAI, Ollama and vLLM, and swap in the mock provider for tests.

## Prerequisites

Set the API key for each hosted provider. Each provider reads the first variable it finds in its list.

| Provider | Environment variables |
|----------|-----------------------|
| `openai` | `OPENAI_API_KEY`, `OPENAI_TOKEN` |
| `claude` | `ANTHROPIC_API_KEY`, `CLAUDE_API_KEY` |
| `gemini` | `GEMINI_API_KEY`, `GOOGLE_API_KEY` |

Ollama needs no key. vLLM takes an optional `api_key` in `additional_config`.

A credential in the provider's `credential` block (`ProviderSpec.Credential`) takes precedence for these providers too:

- vLLM sends it instead of `additional_config.api_key`.
- Ollama sends it, for a proxy in front of Ollama.
- Imagen sends an API key credential in `x-goog-api-key`, ahead of `GOOGLE_API_KEY` and then `GEMINI_API_KEY`. It applies any other credential as it is.

A provider's key is fixed when the provider is built.

A credential never follows a redirect to another host. On a redirect off the provider's scheme and host, the request loses every credential header (`Authorization`, API-key headers, the SigV4 `X-Amz-*` headers) and every custom header, with or without a rate limit. This holds for an HTTP client you pass in, too. The provider uses a copy of your client that runs your own `CheckRedirect` after its own, and your client is never modified.

## Create a hosted provider

Every constructor takes an ID, a model, a base URL (empty selects the provider's default), a `providers.ProviderDefaults`, and an `includeRawOutput` flag.

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/v2/providers"
    "github.com/AltairaLabs/PromptKit/runtime/v2/providers/claude"
    "github.com/AltairaLabs/PromptKit/runtime/v2/providers/gemini"
    "github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
)

defaults := providers.ProviderDefaults{Temperature: 0.7, MaxTokens: 2000}

oa := openai.NewProvider("openai", "gpt-5-mini", "", defaults, false)
defer oa.Close()

cl := claude.NewProvider("claude", "claude-sonnet-5-5", "", defaults, false)
defer cl.Close()

gm := gemini.NewProvider("gemini", "gemini-3.7-flash", "", defaults, false)
defer gm.Close()
```

To choose another model, pass its ID. The pricing tables recognize these models:

| Provider | Models |
|----------|--------|
| OpenAI | `gpt-5-nano`, `gpt-5-mini`, `gpt-5`, `gpt-4.1`, `o4-mini` |
| Claude | `claude-haiku-5-5`, `claude-sonnet-5-5`, `claude-opus-5-5` |
| Gemini | `gemini-3.5-flash-lite`, `gemini-3.7-flash`, `gemini-3.1-pro` |

To set rates yourself, fill `ProviderDefaults.Pricing` with cost per 1,000 tokens. The rates take precedence over the built-in tables when both are positive:

```go
priced := providers.ProviderDefaults{
    Temperature: 0.8,
    TopP:        0.95,
    MaxTokens:   2000,
    Pricing: providers.Pricing{
        InputCostPer1K:  inputRate,
        OutputCostPer1K: outputRate,
    },
}
```

## Limit output tokens

`ProviderDefaults.MaxTokens` caps output tokens. A request's own `MaxTokens` overrides it. Leave `MaxTokens` at zero, or set it to `providers.MaxTokensUnlimited`, to send no limit and let the model's own maximum apply. Claude requires a limit: it sends 4096 when none is set, and `CreateProviderFromSpec` rejects `MaxTokensUnlimited` for it.

## Fall back across providers

Try each provider in order and return the first success:

```go
func predictWithFallback(
    ctx context.Context, chain []providers.Provider, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
    var lastErr error
    for _, p := range chain {
        resp, err := p.Predict(ctx, req)
        if err == nil {
            return resp, nil
        }
        lastErr = err
        log.Printf("provider %s failed: %v", p.ID(), err)
    }
    return providers.PredictionResponse{}, fmt.Errorf("all providers failed: %w", lastErr)
}
```

## Use Azure OpenAI

Use `NewProviderWithCredential` with the `azure` platform and an `AzureCredential`:

```go
import "github.com/AltairaLabs/PromptKit/runtime/v2/credentials"

cred, err := credentials.NewAzureCredential(ctx, "https://your-resource.openai.azure.com")
if err != nil {
    log.Fatal(err)
}

provider := openai.NewProviderWithCredential(
    "azure-openai",
    "gpt-5",
    "",  // derived from the endpoint and model
    defaults,
    false,
    cred,
    "azure",
    &providers.PlatformConfig{
        Type:     "azure",
        Endpoint: "https://your-resource.openai.azure.com",
    },
)
```

The provider derives the deployment URL (`{endpoint}/openai/deployments/{model}`) and appends `?api-version=...` to requests. `AzureCredential` handles Azure AD authentication (managed identity, Azure CLI, environment variables).

For API key authentication, pass an API key credential instead:

```go
provider := openai.NewProviderWithCredential(
    "azure-openai",
    "gpt-5",
    credentials.AzureOpenAIEndpoint("https://your-resource.openai.azure.com", "gpt-5"),
    defaults,
    false,
    credentials.NewAPIKeyCredential(
        os.Getenv("AZURE_OPENAI_API_KEY"),
        credentials.WithHeaderName("api-key"),
        credentials.WithPrefix(""),
    ),
    "azure",
    nil,
)
```

## Use an OpenAI-compatible endpoint

Pass the endpoint as the base URL:

```go
provider := openai.NewProvider("local-llm", "model-name", "http://localhost:8080/v1", defaults, false)
```

## Use Ollama

```go
import "github.com/AltairaLabs/PromptKit/runtime/v2/providers/ollama"

provider := ollama.NewProvider(
    "ollama",
    "llama3.2:1b",
    "http://localhost:11434",
    providers.ProviderDefaults{Temperature: 0.7, MaxTokens: 2048},
    false,
    map[string]interface{}{
        "keep_alive": "5m",  // keep the model loaded
    },
)
defer provider.Close()
```

Start the server and pull the model first:

```bash
ollama serve
ollama pull llama3.2:1b
```

## Use vLLM

```go
import "github.com/AltairaLabs/PromptKit/runtime/v2/providers/vllm"

provider := vllm.NewProvider(
    "vllm",
    "meta-llama/Llama-3.2-3B-Instruct",
    "http://localhost:8000",
    providers.ProviderDefaults{Temperature: 0.7, MaxTokens: 2048},
    false,
    map[string]interface{}{
        "use_beam_search": false,
        "guided_json":     jsonSchema,  // force structured output
    },
)
defer provider.Close()
```

Start a server with the model:

```bash
docker run --rm --gpus all \
  -p 8000:8000 \
  vllm/vllm-openai:latest \
  --model meta-llama/Llama-3.2-3B-Instruct \
  --dtype half \
  --max-model-len 4096
```

### vLLM `additional_config` keys

All vLLM-specific settings go in `additional_config`. The provider reads these keys and ignores any other. To send a server parameter that has no key here, put it in `extra_body`.

| Key | Type | Effect |
|-----|------|--------|
| `api_key` | string | Bearer token for a vLLM server started with `--api-key`. Read once at construction. |
| `use_beam_search` | bool | Beam search instead of sampling. Higher quality, higher latency. |
| `best_of` | int | Generate *n* candidates server-side and return the best. Costs *n*× the compute. |
| `ignore_eos` | bool | Keep generating past the EOS token, up to `max_tokens`. |
| `skip_special_tokens` | bool | Strip special tokens from the returned text. |
| `guided_json` | object | JSON Schema the output must satisfy. |
| `guided_regex` | string | Regex the output must match. |
| `guided_grammar` | string | GBNF grammar the output must follow. |
| `guided_choice` | string[] | Closed set of permitted answers. |
| `extra_body` | object | Extra request fields, merged into every request. See [Send server parameters with `extra_body`](#send-server-parameters-with-extra_body). |

The four `guided_*` keys are mutually exclusive: vLLM applies one decoding constraint per request. The server enforces guided decoding, so the model cannot ignore it the way it can ignore a prompt instruction.

```yaml
id: local-llama
type: vllm
model: meta-llama/Llama-3.2-3B-Instruct
base_url: http://localhost:8000
additional_config:
  guided_choice: [refund, exchange, escalate]
  best_of: 4
```

A value of the wrong type is ignored rather than rejected, so check a key that has no effect against the table before suspecting the server. Two keys accept the shapes a config decoder produces: `best_of` accepts a JSON number (an integer literal decodes to a float such as `4.0`) but refuses a fractional one, and `guided_choice` accepts the untyped sequence that YAML and JSON produce. A `guided_choice` list containing a non-string is refused whole, because a silently shortened list changes what the model may answer.

### Send server parameters with `extra_body`

The `openai`, `vllm` and `ollama` providers merge `additional_config.extra_body` into the body of every chat request, on the streaming and tool-calling paths too. Use it for a request parameter the server accepts and PromptKit has no key for. To turn off Qwen3's reasoning on vLLM:

```yaml
id: qwen3
type: vllm
model: Qwen/Qwen3-8B
base_url: http://localhost:8000
additional_config:
  extra_body:
    chat_template_kwargs:
      enable_thinking: false
```

A field the provider sets itself, such as `model`, `messages`, `tools` or `stream`, always wins. The provider drops the colliding `extra_body` field and logs a warning naming it. An `extra_body` that is not a map is ignored with a warning.

## Test with the mock provider

Back the mock provider with an in-memory repository, and set the response per scenario and turn:

```go
import "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"

repo := mock.NewInMemoryMockRepository("Default reply")
repo.SetResponse("greeting", 1, "Hi there!")

provider := mock.NewProviderWithRepository("mock", "test-model", false, repo)
defer provider.Close()

resp, err := provider.Predict(ctx, providers.PredictionRequest{})
```

`mock.NewProvider("mock", "test-model", false)` returns a fixed response naming the provider and model.

## Verify

Check the cost the provider reports after a call:

```go
if resp.CostInfo != nil {
    log.Printf("Cost: $%.6f", resp.CostInfo.TotalCost)
}
```

## Troubleshoot

**API key not found.** Check that one of the variables in the prerequisites table is set in the process that runs your program:

```go
if os.Getenv("OPENAI_API_KEY") == "" {
    log.Fatal("OPENAI_API_KEY is not set")
}
```

**Requests time out.** Pass a context with a longer deadline:

```go
ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
defer cancel()
```

## Related

- [Configure Pipeline](/runtime/how-to/pipeline/configure-pipeline/)
- [Monitor Costs](/runtime/how-to/observability/monitor-costs/)
- [Provider Reference](/runtime/reference/providers/)
- [First Pipeline tutorial](/runtime/tutorials/01-first-pipeline/)
