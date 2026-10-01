// Command mcp-conformance-client drives PromptKit's MCP client for the official
// MCP conformance suite (github.com/modelcontextprotocol/conformance).
//
// The suite starts a scenario's test server, then runs this binary with the
// server URL as its last argument and the scenario name in
// MCP_CONFORMANCE_SCENARIO. Each scenario's checks grade what the server saw
// on the wire, so this program only has to behave like a normal client:
// initialize, list tools, call them.
//
// Run it with `make mcp-conformance`. Known failures are recorded in
// conformance-baseline.yml; the suite fails on any failure not in the
// baseline AND on any baseline entry that now passes, so the file stays an
// honest list of what PromptKit does not yet do.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/mcp"
)

const (
	scenarioTimeout = 20 * time.Second
	exitUsage       = 2
	minArgs         = 2 // program name and the server URL
	sampleString    = "conformance"
)

func main() {
	os.Exit(realMain(os.Args, os.Getenv("MCP_CONFORMANCE_SCENARIO"), os.Stderr))
}

// realMain runs one scenario and returns the process exit code.
func realMain(args []string, scenario string, stderr io.Writer) int {
	if len(args) < minArgs {
		_, _ = fmt.Fprintln(stderr, "usage: MCP_CONFORMANCE_SCENARIO=<scenario> mcp-conformance-client <server-url>")
		return exitUsage
	}
	if err := run(args[len(args)-1]); err != nil {
		_, _ = fmt.Fprintf(stderr, "scenario %s: %v\n", scenario, err)
		return 1
	}
	return 0
}

// run is the generic client flow every scenario at the claimed revision
// expects: initialize, list tools, call each tool with arguments built from
// its input schema.
func run(url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), scenarioTimeout)
	defer cancel()

	opts := mcp.DefaultClientOptions()
	opts.ElicitationHandler = acceptDefaults
	client := mcp.NewStreamableClientWithOptions(mcp.ServerConfig{
		Name:          sampleString,
		URL:           url,
		TransportName: mcp.TransportStreamableHTTP,
	}, opts)
	defer func() { _ = client.Close() }()

	if _, err := client.Initialize(ctx); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	tools, err := client.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("tools/list: %w", err)
	}
	for _, tool := range tools {
		if _, err := client.CallTool(ctx, tool.Name, sampleArgs(tool.InputSchema)); err != nil {
			return fmt.Errorf("tools/call %s: %w", tool.Name, err)
		}
	}
	return nil
}

// acceptDefaults plays a user who submits a form without changing it: the
// client pre-populates the schema's defaults.
func acceptDefaults(context.Context, string, mcp.ElicitRequest) (mcp.ElicitResult, error) {
	return mcp.ElicitResult{Action: mcp.ElicitActionAccept}, nil
}

// sampleArgs builds arguments satisfying the top-level types of a tool's
// input schema, enough for a scenario's server to accept the call.
func sampleArgs(schema json.RawMessage) json.RawMessage {
	var s struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schema, &s); err != nil {
		return json.RawMessage(`{}`)
	}
	args := map[string]any{}
	for name, prop := range s.Properties {
		switch prop.Type {
		case "number", "integer":
			args[name] = 1
		case "boolean":
			args[name] = true
		case "array":
			args[name] = []any{}
		case "object":
			args[name] = map[string]any{}
		default:
			args[name] = sampleString
		}
	}
	out, _ := json.Marshal(args)
	return out
}
