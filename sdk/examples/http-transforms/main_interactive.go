// Package main demonstrates HTTP tool transforms and response processing via the SDK.
//
// This example shows three patterns for HTTP tool customization:
//   - WithTransform: normalize/map LLM arguments before the request
//   - WithPreRequest: inject headers, tracing IDs, or auth at request time
//   - WithPostProcess + WithRedact: transform responses and strip sensitive fields
//
// Run with:
//
//	export OPENAI_API_KEY=your-key
//	go run .
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/AltairaLabs/PromptKit/sdk"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/tools"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

func main() {
	conv, err := sdk.Open("./http-transforms.pack.json", "assistant")
	if err != nil {
		log.Fatalf("Failed to open pack: %v", err)
	}
	defer conv.Close()

	titleCase := cases.Title(language.English)

	// --- Pattern 1: Argument transform ---
	// The LLM sends {"name": "john smith"} but the API expects
	// {"first_name": "John", "last_name": "Smith"}.
	lookupCfg := sdktools.NewHTTPToolConfig("https://api.example.com/users/lookup",
		sdktools.WithMethod("POST"),
		sdktools.WithHeader("Accept", "application/json"),
		sdktools.WithTransform(func(args map[string]any) (map[string]any, error) {
			name, _ := args["name"].(string)
			parts := strings.SplitN(name, " ", 2)

			transformed := map[string]any{
				"first_name": titleCase.String(parts[0]),
				"last_name":  "",
			}
			if len(parts) > 1 {
				transformed["last_name"] = titleCase.String(parts[1])
			}
			return transformed, nil
		}),
	)
	conv.OnTool("lookup_user", lookupCfg.Handler())

	// --- Pattern 2: Pre-request hook ---
	// Inject a correlation ID and dynamic auth token into every request.
	var requestCount int
	orderCfg := sdktools.NewHTTPToolConfig("https://api.example.com/orders",
		sdktools.WithMethod("GET"),
		sdktools.WithPreRequest(func(req *http.Request) error {
			requestCount++
			req.Header.Set("X-Correlation-ID", fmt.Sprintf("req-%04d", requestCount))
			req.Header.Set("Authorization", "Bearer dynamic-token-here")
			return nil
		}),
	)
	conv.OnToolCtx("get_orders", orderCfg.HandlerCtx())

	// --- Pattern 3: Response post-processing + field redaction ---
	// Strip sensitive fields before sending to the LLM, add computed fields.
	customerCfg := sdktools.NewHTTPToolConfig("https://api.example.com/customers",
		sdktools.WithMethod("GET"),
		sdktools.WithRedact("ssn", "credit_card", "internal_notes"),
		sdktools.WithPostProcess(func(resp []byte) ([]byte, error) {
			var data map[string]any
			if err := json.Unmarshal(resp, &data); err != nil {
				return resp, nil
			}

			first, _ := data["first_name"].(string)
			last, _ := data["last_name"].(string)
			if first != "" || last != "" {
				data["display_name"] = strings.TrimSpace(first + " " + last)
			}
			return json.Marshal(data)
		}),
	)
	conv.OnTool("get_customer", customerCfg.Handler())

	// Send a message that triggers tool use.
	ctx := context.Background()
	resp, err := conv.Send(ctx, "Look up user John Smith")
	if err != nil {
		log.Fatalf("Send failed: %v", err)
	}
	fmt.Println("Response:", resp.Text())
}
