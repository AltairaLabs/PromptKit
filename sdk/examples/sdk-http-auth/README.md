# HTTP Tool Authentication Example

Add custom authentication to HTTP tools called by the SDK.

## What it shows

Three patterns for injecting auth into HTTP tool requests:

- **Static API key** via `WithHeader`
- **Environment-based headers** via `WithHeaderFromEnv`
- **Dynamic OAuth with token caching** via `WithPreRequest`

No API key required — the example uses a mock provider with canned responses.

## Running

```bash
go run ./sdk/examples/sdk-http-auth
```
