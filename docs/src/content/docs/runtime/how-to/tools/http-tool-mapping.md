---
title: Configure HTTP Tool Mapping
description: Route tool arguments to the query string, headers and URL path, inject fixed parameters, and filter responses.
sidebar:
  order: 4
verified:
  commit: 004b9cbbd4d40f770a2e3c76053b600a55189355
  sources:
    - runtime/tools/http_executor.go
    - runtime/tools/http_mapping.go
    - runtime/tools/http_multimodal.go
    - runtime/tools/types.go
---
Map the arguments the model sends to HTTP request components, and shrink API responses to the fields the model needs.

## Prerequisites

- A tool definition with `mode: live` and a `spec.http` block (see [HTTP Tools SDK](/sdk/how-to/tools/http-tools/)).
- A `request` block under `spec.http` for every mapping on this page. Without it, a GET sends all arguments as query parameters, a POST, PUT or PATCH sends them as the JSON body, and nothing is templated.

## Steps

### 1. Route arguments to query parameters

List the arguments that belong in the query string under `query_params`:

```yaml
spec:
  http:
    url: "https://api.example.com/search"
    method: GET
    request:
      query_params:
        - query
        - limit
```

For `{"query": "London", "limit": 5}` the request is `GET /search?limit=5&query=London`.

With a `request` block, arguments not listed in `query_params` go in the JSON body for POST, PUT and PATCH. On other methods they are dropped.

### 2. Put arguments in the URL path

Use Go `text/template` syntax in the URL. Add a `request` block, even an empty one, so the executor renders the template:

```yaml
spec:
  http:
    url: "https://api.example.com/users/{{.user_id}}/repos/{{.repo}}"
    method: GET
    request: {}
```

Arguments used in the path are excluded from the body.

The URL from the scheme through the host and port must be fixed text, so the model cannot choose where the request goes. A template such as `https://{{.region}}.api.example.com` fails when the tool is called. To reach several hosts, declare one tool per host.

### 3. Put arguments in headers

```yaml
    request:
      header_params:
        Authorization: "Bearer {{.token}}"
        X-Workspace: "{{.workspace_id}}"
```

Arguments used in a header template are excluded from the body.

### 4. Reshape or exclude body fields

`body_mapping` is a JMESPath expression applied to the body arguments of a POST, PUT or PATCH:

```yaml
spec:
  http:
    url: "https://api.example.com/users"
    method: POST
    request:
      body_mapping: "{name: first_name, surname: last_name}"
      exclude:
        - internal_id
```

`{"first_name": "John", "last_name": "Doe", "age": 30}` becomes `{"name": "John", "surname": "Doe"}`. `exclude` drops the listed arguments from the request.

### 5. Inject fixed parameters

Static values are not part of the model's input schema:

```yaml
spec:
  http:
    url: "https://geocoding-api.open-meteo.com/v1/search"
    method: GET
    request:
      query_params:
        - name
      static_query:
        count: "1"
        language: "en"
      static_headers:
        X-Api-Version: "2024-01"
```

The request is `GET /v1/search?count=1&language=en&name=London`. For POST, PUT and PATCH, `static_body` merges fixed fields into the JSON body. A static value replaces an argument with the same key.

### 6. Filter the response

`response.body_mapping` is a JMESPath expression applied to the JSON response before the model sees it:

```yaml
    response:
      body_mapping: "results[*].{name: name, latitude: latitude, longitude: longitude, country: country}"
```

This reduced the geocoding response from ~3,400 bytes to ~87 bytes, a 97% reduction in tokens sent to the model. A response that is not JSON is wrapped in a JSON object first.

| Pattern | Result |
|---------|--------|
| `results[0]` | First element of an array |
| `results[*].name` | One field from each element |
| `results[*].{a: field_a, b: field_b}` | Selected fields from each element |
| `{temp: current.temperature, wind: current.wind_speed}` | Reshaped nested object |
| `data.items[?status=='active']` | Filtered elements |

### 7. Return binary responses (optional)

Enable `multimodal` for APIs that return images, audio or video:

```yaml
spec:
  mode: live
  http:
    url: "https://api.example.com/images/{{.width}}x{{.height}}.png"
    method: GET
    request:
      query_params:
        - text
    multimodal:
      enabled: true
      accept_types:
        - image/png
        - image/jpeg
```

When the response Content-Type matches, the executor returns it as a `ContentPart` with base64-encoded data. The JSON result is a summary with `type`, `content_type` and `size_bytes`. `accept_types` also sets the request's `Accept` header. If `accept_types` is empty, any `image/`, `audio/` or `video/` type matches.

## Verify

A weather tool that combines query routing, static parameters and response filtering:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: get_weather
spec:
  description: "Get current weather for a location"
  mode: live
  timeout_ms: 10000

  input_schema:
    type: object
    properties:
      latitude:
        type: number
      longitude:
        type: number
    required: [latitude, longitude]

  output_schema:
    type: object
    properties:
      temperature_2m:
        type: number
      wind_speed_10m:
        type: number
      weather_code:
        type: integer

  http:
    url: "https://api.open-meteo.com/v1/forecast"
    method: GET
    request:
      query_params:
        - latitude
        - longitude
      static_query:
        current: "temperature_2m,wind_speed_10m,weather_code"
        timezone: "auto"
    response:
      body_mapping: "{temperature_2m: current.temperature_2m, wind_speed_10m: current.wind_speed_10m, weather_code: current.weather_code}"
```

Call the tool with `{"latitude": 51.5, "longitude": -0.12}`. The runtime logs `HTTP tool call` with the final URL, which carries the static parameters, and the result holds only the three mapped fields.

## Troubleshoot

- The URL still contains `{{.user_id}}` or the arguments appear in the query string: add a `request` block.
- An argument is missing from a GET request: list it under `query_params` or put it in the path.
- The call fails with "URL templates may use arguments in the path and query": remove the template from the scheme, host or port.
- `JMESPath expression ... failed`: test the expression against the raw response.

## Related

- [Integrate MCP](/runtime/how-to/tools/integrate-mcp/)
- [HTTP Tools SDK](/sdk/how-to/tools/http-tools/)
