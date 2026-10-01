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
	"errors"
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
	os.Exit(realMain(os.Args, os.Getenv("MCP_CONFORMANCE_SCENARIO"), os.Getenv("MCP_CONFORMANCE_CONTEXT"), os.Stderr))
}

// realMain runs one scenario and returns the process exit code.
func realMain(args []string, scenario, scenarioContext string, stderr io.Writer) int {
	if len(args) < minArgs {
		_, _ = fmt.Fprintln(stderr, "usage: MCP_CONFORMANCE_SCENARIO=<scenario> mcp-conformance-client <server-url>")
		return exitUsage
	}
	if err := run(args[len(args)-1], parseContext(scenarioContext), stderr); err != nil {
		_, _ = fmt.Fprintf(stderr, "scenario %s: %v\n", scenario, err)
		return 1
	}
	return 0
}

// scenarioContext is the data a scenario passes in MCP_CONFORMANCE_CONTEXT.
// Some scenarios name the tool calls to make, with exact arguments, so the
// values exercise particular encodings.
type scenarioContext struct {
	ToolCalls []toolCall `json:"toolCalls"`
}

type toolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func parseContext(raw string) scenarioContext {
	var c scenarioContext
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &c)
	}
	return c
}

// run is the generic client flow the scenarios expect: connect, list tools,
// and call them — with the scenario's arguments when it supplies them,
// otherwise with arguments built from each tool's input schema. A tool call
// the server rejects is reported, not fatal: the scenario's checks decide
// what was correct.
func run(url string, sc scenarioContext, stderr io.Writer) error {
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
	calls := sc.ToolCalls
	if len(calls) == 0 {
		calls = defaultCalls(tools)
	}
	for _, call := range calls {
		if _, err := client.CallTool(ctx, call.Name, call.Arguments); err != nil {
			var rpcErr *mcp.RPCError
			if !errors.As(err, &rpcErr) {
				return fmt.Errorf("tools/call %s: %w", call.Name, err)
			}
			_, _ = fmt.Fprintf(stderr, "tools/call %s: %v\n", call.Name, err)
		}
	}
	return nil
}

// Tools of the json-schema-2020-12-preservation scenario: the client echoes
// the focal tool's inputSchema back exactly as listed, proving it did not
// rewrite the schema.
const (
	schemaFocalTool = "json_schema_2020_12_tool"
	schemaEchoTool  = "json_schema_echo"
)

// defaultCalls calls every tool with arguments built from its schema, except
// that a schema-echo tool is called with the focal tool's schema verbatim.
func defaultCalls(tools []mcp.Tool) []toolCall {
	var focal json.RawMessage
	for _, t := range tools {
		if t.Name == schemaFocalTool {
			focal = t.InputSchema
		}
	}
	var calls []toolCall
	for _, t := range tools {
		args := sampleArgs(t.InputSchema)
		if t.Name == schemaEchoTool && focal != nil {
			args, _ = json.Marshal(map[string]json.RawMessage{"schema": focal})
		}
		calls = append(calls, toolCall{Name: t.Name, Arguments: args})
	}
	return calls
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
