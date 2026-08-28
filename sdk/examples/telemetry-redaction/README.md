# Telemetry Redaction

What conversation content reaches observability consumers, and the two controls
over it.

```bash
go run .
```

No API keys, no network — the model is scripted in `main.go`.

## Output

```
default                 trace: no content       audit store: LEAKED the access token
capture on              trace: LEAKED           audit store: LEAKED
capture on + redactor   trace: redacted         audit store: redacted
```

## The two controls do different jobs

| option | scope | default |
|---|---|---|
| `WithTelemetryContentCapture(bool)` | spans only | **off** |
| `WithEventRedactor(policy)` | every bus subscriber | none |

**The gate is a tracing control.** It keeps payloads off spans and does nothing
for any other consumer. Row 1 shows this: the trace is clean while an
`EventStore` the caller wired still receives the raw token. That is correct — a
store is a sink you chose, and redacting it by default would break audit and
replay — but it means the gate alone is not a data-protection story.

**The redactor is the general control.** It runs at the bus, so one policy
covers the tracer, a configured event store, and the metrics collector.

`WithRecording` is deliberately unaffected by either: `RecordingStage` appends
straight to its `EventStore` with no bus hop, so recordings keep full fidelity.
If you need those redacted, redact in the store you supply.

## Why this cannot live in a tool handler

The obvious idea is that tool calling is extensible, so redaction belongs in
your handler. Of the four content-bearing attributes, three are produced by the
**model** — tool-call arguments, the tool-call list, message content — and are
not yours to change. The fourth, the tool result, *is* yours, but it is the same
value the **model** consumes: redact it there and you have withheld it from the
model rather than from the trace.

So the enforcement point is the runtime's. The policy is yours, which is what
`WithEventRedactor` takes.

## Why it matters

Tool arguments are composed by the model and carry whatever your tools accept —
order ids, email addresses, free text. Under on-behalf-of token exchange they
carry live delegated OAuth credentials. Exporting a trace exports all of it,
across whatever trust boundary your APM sits behind.
