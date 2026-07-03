# PromptKit Examples

This directory holds examples that exercise the **PromptKit SDK and runtime**.

> **Looking for PromptArena example packs?** The arena testing examples
> (customer-support, guardrails, voice self-play, workflows, MCP, multimodal,
> etc.) moved to the [promptarena repo](https://github.com/AltairaLabs/promptarena/tree/main/examples)
> along with the CLI. See [promptarena.altairalabs.ai](https://promptarena.altairalabs.ai)
> for the documented walkthroughs.

## Examples in this repo

| Example | What it shows |
|---------|---------------|
| [`a2a-demo`](./a2a-demo) | Agent-to-Agent (A2A) protocol server + client using the SDK |
| [`a2a-auth-test`](./a2a-auth-test) | Authenticated A2A endpoints |
| [`logging-config`](./logging-config) | Logging configuration profiles (development / debugging / production) |

## Running the Go examples

```bash
cd examples/a2a-demo
go run .
```

These modules are part of the Go workspace (`go.work`) and build against the
local `runtime`/`sdk` packages.
