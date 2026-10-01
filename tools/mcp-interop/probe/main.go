// Command probe connects PromptKit's MCP client to one server, lists its
// tools and calls the ones named in TOOLS, printing one line per step.
//
//	probe <label> stdio <command> [args...]
//	probe <label> streamable|sse|auto <url>
//
// TOOLS is a ';'-separated list of name or name=<json arguments>. LEGACY=1
// forces the initialize handshake (ClientOptions.DisableModernProtocol). The
// exit status is non-zero if the client cannot connect or list tools; a tool
// call that fails is reported, not fatal, since the servers include tools
// that fail on purpose.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

const (
	probeTimeout = 90 * time.Second
	callTimeout  = 20 * time.Second
	maxShown     = 110
	minArgs      = 4
	exitUsage    = 2
	// Values a user filling in a form would enter.
	sampleText    = "green"
	sampleInteger = 42
	sampleNumber  = 3.14
)

func main() {
	if len(os.Args) < minArgs {
		fmt.Fprintln(os.Stderr, "usage: probe <label> stdio <command> [args...] | probe <label> streamable|sse|auto <url>")
		os.Exit(exitUsage)
	}
	if err := run(os.Args[1], os.Args[2], os.Args[3:]); err != nil {
		fmt.Println(os.Args[1], "FAIL", err)
		os.Exit(1)
	}
}

func serverConfig(kind string, target []string) mcp.ServerConfig {
	cfg := mcp.ServerConfig{Name: "s", TimeoutMs: int(callTimeout / time.Millisecond)}
	if kind == "stdio" {
		cfg.Command, cfg.Args = target[0], target[1:]
		return cfg
	}
	cfg.URL = target[0]
	switch kind {
	case "streamable":
		cfg.TransportName = mcp.TransportStreamableHTTP
	case "sse":
		cfg.TransportName = mcp.TransportSSE
	}
	return cfg
}

// formField is the part of an elicitation property schema the probe reads.
type formField struct {
	Type  string `json:"type"`
	Enum  []any  `json:"enum"`
	OneOf []struct {
		Const any `json:"const"`
	} `json:"oneOf"`
	AnyOf []struct {
		Const any `json:"const"`
	} `json:"anyOf"`
	Items *formField `json:"items"`
}

// firstChoice is the first value an enumerated field allows, if it is one.
func (f *formField) firstChoice() (any, bool) {
	switch {
	case len(f.Enum) > 0:
		return f.Enum[0], true
	case len(f.OneOf) > 0:
		return f.OneOf[0].Const, true
	case len(f.AnyOf) > 0:
		return f.AnyOf[0].Const, true
	}
	return nil, false
}

// value is what a user filling in the form would enter for f.
func (f *formField) value() any {
	if v, ok := f.firstChoice(); ok {
		return v
	}
	switch f.Type {
	case "integer":
		return sampleInteger
	case "number":
		return sampleNumber
	case "boolean":
		return true
	case "array":
		if f.Items != nil {
			return []any{f.Items.value()}
		}
		return []any{sampleText}
	}
	return sampleText
}

// answerForm answers every elicitation as a user filling in the form would:
// a value of the requested type for every property, the first choice of an
// enumerated one, and "green" for free text.
func answerForm(_ context.Context, _ string, req mcp.ElicitRequest) (mcp.ElicitResult, error) {
	var schema struct {
		Properties map[string]*formField `json:"properties"`
	}
	_ = json.Unmarshal(req.RequestedSchema, &schema)
	answer := map[string]any{}
	for name, field := range schema.Properties {
		answer[name] = field.value()
	}
	content, err := json.Marshal(answer)
	if err != nil {
		return mcp.ElicitResult{}, err
	}
	return mcp.ElicitResult{Action: mcp.ElicitActionAccept, Content: content}, nil
}

func run(label, kind string, target []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	reg := mcp.NewRegistryWithOptions(mcp.RegistryOptions{ConfigureClient: func(_ mcp.ServerConfig, o *mcp.ClientOptions) {
		o.ElicitationHandler = answerForm
		o.DisableModernProtocol = os.Getenv("LEGACY") == "1"
		o.EnableGracefulDegradation = false
	}})
	defer func() { _ = reg.Close() }()
	if err := reg.RegisterServer(serverConfig(kind, target)); err != nil {
		return fmt.Errorf("register: %w", err)
	}
	start := time.Now()
	client, err := reg.GetClient(ctx, "s")
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	info, err := client.Initialize(ctx)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	fmt.Printf("%s connected in %v: version=%s server=%s\n",
		label, time.Since(start).Round(time.Millisecond), info.ProtocolVersion, info.ServerInfo.Name)
	listed, err := client.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("tools/list: %w", err)
	}
	byName := map[string]mcp.Tool{}
	for i := range listed {
		byName[listed[i].Name] = listed[i]
	}
	callTools(ctx, label, reg, byName)
	return nil
}

// callTools calls each tool in TOOLS through the MCP executor, as an agent's
// tool call would reach it.
func callTools(ctx context.Context, label string, reg *mcp.RegistryImpl, byName map[string]mcp.Tool) {
	exec := tools.NewMCPExecutor(reg)
	for _, spec := range strings.Split(os.Getenv("TOOLS"), ";") {
		if spec == "" {
			continue
		}
		name, args, _ := strings.Cut(spec, "=")
		if args == "" {
			args = "{}"
		}
		t, ok := byName[name]
		if !ok {
			fmt.Printf("%s   %-24s MISSING\n", label, name)
			continue
		}
		d := &tools.ToolDescriptor{Name: name, Mode: "mcp", InputSchema: t.InputSchema, OutputSchema: t.OutputSchema}
		callCtx, cancel := context.WithTimeout(ctx, callTimeout)
		out, parts, err := exec.ExecuteMultimodal(callCtx, d, json.RawMessage(args))
		cancel()
		if err != nil {
			fmt.Printf("%s   %-24s ERR %v\n", label, name, err)
			continue
		}
		shown := string(out)
		if len(shown) > maxShown {
			shown = shown[:maxShown] + "…"
		}
		fmt.Printf("%s   %-24s OK  %s parts=%d\n", label, name, shown, len(parts))
	}
}
