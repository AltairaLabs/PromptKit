---
title: Call HTTP tools
description: Bind a tool to an HTTP API with OnToolHTTP and the HTTPToolConfig builder.
sidebar:
  order: 4
verified:
  commit: 004b9cbbd4d40f770a2e3c76053b600a55189355
  sources:
    - runtime/tools/http_executor.go
    - sdk/conversation_tools.go
    - sdk/tools/http.go
---
Make a tool call an HTTP API with `OnToolHTTP` and `tools.NewHTTPToolConfig`, adding authentication and reshaping arguments and responses.

## Prerequisites

- A conversation from `sdk.Open`, and a tool declared in the pack. See [Register tools](/sdk/how-to/tools/register-tools/).
- The import `sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"`.

## 1. Register the tool

```go
cfg := sdktools.NewHTTPToolConfig("https://api.example.com/search",
    sdktools.WithMethod("GET"),
)

conv.OnToolHTTP("search", cfg)
```

The method defaults to `POST`. A `POST`, `PUT` or `PATCH` call sends the model's arguments as a JSON body. Any other method sends them as query parameters. The JSON response goes back to the model. A non-2xx status returns an error to the model.

`OnToolHTTP` registers a context-aware handler. To register the handler yourself, use `conv.OnTool("search", cfg.Handler())` or `conv.OnToolCtx("search", cfg.HandlerCtx())`.

## 2. Add authentication

Set a static header:

```go
cfg := sdktools.NewHTTPToolConfig("https://api.example.com/data",
    sdktools.WithHeader("X-Api-Key", "abc123"),
)
```

Read a header value from an environment variable, in the form `Header-Name=ENV_VAR`:

```go
cfg := sdktools.NewHTTPToolConfig("https://api.example.com/data",
    sdktools.WithHeaderFromEnv("Authorization=API_TOKEN"),
)
```

The runtime sets `Authorization` to the value of `$API_TOKEN` on each call, and omits the header when the variable is empty.

Use `WithPreRequest` to change the request before it is sent, such as to add an OAuth token that expires:

```go
cfg := sdktools.NewHTTPToolConfig("https://api.example.com/data",
    sdktools.WithPreRequest(func(req *http.Request) error {
        token, err := fetchToken(req.Context())
        if err != nil {
            return err
        }
        req.Header.Set("Authorization", "Bearer "+token)
        return nil
    }),
)
conv.OnToolCtx("fetch_data", cfg.HandlerCtx())
```

The function runs on every request, after the URL and the static and environment headers are set. If it returns an error, the tool call fails with that error and the request is not sent. Register the config with `HandlerCtx` or `OnToolHTTP`.

## 3. Set a timeout and redact fields

```go
cfg := sdktools.NewHTTPToolConfig("https://api.example.com/customers",
    sdktools.WithMethod("GET"),
    sdktools.WithTimeout(10000),            // milliseconds
    sdktools.WithRedact("ssn", "credit_card"),
)
```

`WithRedact` replaces the named top-level fields of a JSON object response with `[REDACTED]` before the model sees it.

## 4. Reshape arguments

`WithTransform` rewrites the model's arguments before the request:

```go
cfg := sdktools.NewHTTPToolConfig("https://api.example.com/users",
    sdktools.WithTransform(func(args map[string]any) (map[string]any, error) {
        first, _ := args["first_name"].(string)
        last, _ := args["last_name"].(string)
        return map[string]any{
            "full_name": first + " " + last,
            "email":     args["email"],
        }, nil
    }),
)
```

## 5. Reshape the response

`WithPostProcess` receives the response bytes after redaction and returns the bytes the model sees:

```go
cfg := sdktools.NewHTTPToolConfig("https://api.example.com/search",
    sdktools.WithMethod("GET"),
    sdktools.WithPostProcess(func(resp []byte) ([]byte, error) {
        var data map[string]any
        if err := json.Unmarshal(resp, &data); err != nil {
            return resp, nil
        }
        return json.Marshal(data["results"])
    }),
)
```

## Complete example

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/sdk/v2"
    sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

func main() {
    conv, err := sdk.Open("./assistant.pack.json", "assistant")
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    cfg := sdktools.NewHTTPToolConfig("https://api.example.com/customers",
        sdktools.WithMethod("GET"),
        sdktools.WithTimeout(10000),
        sdktools.WithHeaderFromEnv("Authorization=API_TOKEN"),
        sdktools.WithRedact("ssn", "credit_card"),
        sdktools.WithPostProcess(func(resp []byte) ([]byte, error) {
            var data map[string]any
            if err := json.Unmarshal(resp, &data); err != nil {
                return resp, nil
            }
            return json.Marshal(map[string]any{"name": data["name"], "plan": data["plan"]})
        }),
    )
    conv.OnToolHTTP("get_customer", cfg)

    resp, err := conv.Send(context.Background(), "Look up customer John Smith")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp.Text())
}
```

## Verify

Send a message that makes the model call the tool. The runtime logs `HTTP tool call` with the tool name, method and URL, then `HTTP tool call completed` with the status.

## Related

- [Register tools](/sdk/how-to/tools/register-tools/): handlers written in Go
- [HTTP tool mapping](/runtime/how-to/tools/http-tool-mapping/): declare URL templates and request and response mapping in the pack
- [Client-side tools](/sdk/how-to/tools/client-tools/): tools that run on the client device
- [SDK reference](/sdk/reference/conversation-manager/): every option and signature
