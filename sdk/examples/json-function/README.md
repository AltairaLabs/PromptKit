# JSON Function Example

Demonstrates the **function-style** PromptKit pattern: invoke a prompt like a
serverless function — structured JSON in, one-shot, JSON out.

## What it shows

- **`WithJSONInput`** — bind a structured input to the prompt's template
  variables for a single `Send`. Top-level fields become `{{topic}}` /
  `{{audience}}`; the whole payload is available as `{{input}}`.
- **`WithResponseFormat`** — request schema-enforced JSON output (works across
  Claude, OpenAI, and Gemini).
- **Empty-message one-shot** — passing `""` as the message makes the input JSON
  also fill the user turn, so a single `Send` is a complete invocation.
- **Typed round-trip** — unmarshal the JSON response into a Go struct.

## The recipe

```go
conv, _ := sdk.Open("./research.pack.json", "plan",
    sdk.WithResponseFormat(&providers.ResponseFormat{
        Type:       providers.ResponseFormatJSONSchema,
        JSONSchema: outputSchema,
        SchemaName: "research_plan",
        Strict:     true,
    }),
)
defer conv.Close()

resp, _ := conv.Send(ctx, "", sdk.WithJSONInput(PlanRequest{
    Topic:    "utility-scale battery storage growth in 2023",
    Audience: "executive",
}))

var plan PlanResponse
json.Unmarshal([]byte(resp.Text()), &plan)
```

## Binding rules

For a JSON object input:

- `{{input}}` is bound to the whole object (compact JSON).
- Each top-level field is bound to `{{field}}` — strings pass through verbatim;
  numbers, booleans, objects, and arrays are bound as their compact JSON.
- Bound variables override open-time `WithVariables` defaults.
- A real top-level field named `input` shadows the synthetic whole-object var.

## Run it

```bash
export OPENAI_API_KEY=your-key   # or ANTHROPIC_API_KEY / GEMINI_API_KEY
go run .
```

The smoke test (`go test ./...`) exercises the same path with a mock provider —
no API keys required.

> Note: prefer `WithResponseFormat` with a schema over the schemaless
> `WithJSONMode` for cross-provider output — `WithJSONMode` has no effect on
> Anthropic, which only supports schema-backed structured output.
