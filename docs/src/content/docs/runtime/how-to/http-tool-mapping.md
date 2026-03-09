---
title: Configure HTTP Tool Mapping
sidebar:
  order: 4
---
Map LLM tool arguments to HTTP request components and filter responses to reduce token usage.

## Goal

Configure declarative request and response mapping for HTTP tools so you can:
- Route arguments to query params, headers, or URL path segments
- Inject static API parameters the LLM doesn't need to know about
- Filter large API responses down to the fields the LLM actually needs
- Handle multimodal (binary) responses like images and audio

## Request Mapping

### Route Arguments to Query Parameters

By default, all LLM arguments are sent as the JSON body. Use `query_params` to route specific arguments to the URL query string instead:

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

When the LLM calls the tool with `{"query": "London", "limit": 5}`, the request becomes `GET /search?query=London&limit=5`.

### URL Path Parameters

Use Go `text/template` syntax to interpolate arguments into the URL path:

```yaml
spec:
  http:
    url: "https://api.example.com/users/{{.user_id}}/repos/{{.repo}}"
    method: GET
```

Path-consumed arguments are automatically excluded from the request body.

### Header Parameters

Route arguments to HTTP headers using templates:

```yaml
spec:
  http:
    url: "https://api.example.com/data"
    method: GET
    request:
      header_params:
        Authorization: "Bearer {{.token}}"
        X-Workspace: "{{.workspace_id}}"
```

Header-consumed arguments are automatically excluded from the body.

### Body Reshaping

Use JMESPath to reshape the request body before sending:

```yaml
spec:
  http:
    url: "https://api.example.com/users"
    method: POST
    request:
      body_mapping: "{name: first_name, surname: last_name}"
```

This transforms `{"first_name": "John", "last_name": "Doe", "age": 30}` into `{"name": "John", "surname": "Doe"}`.

### Exclude Arguments

Explicitly exclude arguments from the request:

```yaml
spec:
  http:
    url: "https://api.example.com/data"
    method: POST
    request:
      exclude:
        - internal_id
```

## Static Parameters

Inject fixed parameters that the LLM doesn't need to know about. These are invisible to the LLM's input schema.

### Static Query Parameters

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
        format: "json"
```

The LLM only sees `name` in the input schema. The request becomes `GET /search?name=London&count=1&language=en&format=json`.

### Static Headers

```yaml
spec:
  http:
    url: "https://api.example.com/data"
    method: GET
    request:
      static_headers:
        X-Api-Version: "2024-01"
        Accept: "application/json"
```

### Static Body Fields

Merge fixed fields into the JSON body for POST/PUT/PATCH requests:

```yaml
spec:
  http:
    url: "https://api.example.com/data"
    method: POST
    request:
      static_body:
        api_version: "v2"
        format: "json"
```

LLM-provided body fields and static body fields are merged. Static fields do not override LLM-provided fields with the same key.

## Response Mapping

### Filter Response Fields

Use JMESPath to extract only the fields the LLM needs from large API responses:

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
    response:
      body_mapping: "results[*].{name: name, latitude: latitude, longitude: longitude, country: country}"
```

This reduced the geocoding response from ~3,400 bytes to ~87 bytes — a 97% reduction in tokens sent to the LLM.

### Common JMESPath Patterns

| Pattern | Description |
|---------|-------------|
| `results[0]` | First element of an array |
| `results[*].name` | Extract one field from each array element |
| `results[*].{a: field_a, b: field_b}` | Pick specific fields from each element |
| `{temp: current.temperature, wind: current.wind_speed}` | Reshape nested objects |
| `data.items[?status=='active']` | Filter array elements |

## Multimodal Responses

Handle binary responses (images, audio, video) from HTTP APIs:

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

When `multimodal.enabled` is true, the executor detects binary Content-Type headers and returns the response as a `ContentPart` with base64-encoded data. If `accept_types` is empty, common image/audio/video types are auto-detected.

## Complete Example

A weather tool with query routing, static params, and response filtering:

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

## See Also

- [Register Tools](../how-to/integrate-mcp) - MCP tool integration
- [HTTP Tools SDK](../../sdk/how-to/http-tools) - Programmatic HTTP tools with custom auth
