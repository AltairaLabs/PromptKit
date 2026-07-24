# Logging Configuration Profiles

Ready-to-use `LoggingConfig` files for the three environments you typically run
PromptKit in. Each is a `promptkit.altairalabs.ai/v1alpha1` `LoggingConfig`
manifest you can load to control log level, format, and per-component verbosity.

| Profile | File | Use it for |
|---------|------|------------|
| Development | [`development.logging.yaml`](./development.logging.yaml) | Local dev — `debug` level, human-readable text, request/response detail |
| Debugging | [`debugging.logging.yaml`](./debugging.logging.yaml) | Chasing a specific issue — maximum verbosity on the components involved |
| Production | [`production.logging.yaml`](./production.logging.yaml) | Deployed services — `info` level, structured JSON, minimal noise |

## What they set

- **`defaultLevel`** — the baseline log level (`debug` / `info` / `warn` / `error`).
- **`format`** — `text` for a readable terminal, `json` for structured ingestion.
- **`commonFields`** — fields stamped on every log entry (service name, environment, …).
- **Per-component levels** — raise or lower verbosity for individual subsystems
  (providers, pipeline, tools, …) without changing the global level.

## Using a profile

Point the runtime at a profile via configuration, then start your app as usual —
the manifest tunes the logger without any code changes. Copy a profile into your
project and adjust the fields to taste.
