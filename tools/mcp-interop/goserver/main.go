// Command goserver is an MCP server built with the official Go SDK, for the
// interop matrix: a text tool, a structured tool, an image, a failing tool
// and a tool that asks the user for input (mid-call on a handshake-era
// session, by multi round-trip on a stateless one).
//
//	goserver stdio | http <port> | stateless <port> | sse <port>
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type echoIn struct {
	Text string `json:"text"`
}

type addIn struct {
	A int `json:"a"`
	B int `json:"b"`
}

type addOut struct {
	Total int `json:"total"`
}

const (
	modeStateless     = "stateless"
	colorQuestion     = "color?"
	readHeaderTimeout = 5 * time.Second
	minArgs           = 2
	// statelessVersion is the first protocol revision whose servers answer
	// with input_required instead of asking mid-call.
	statelessVersion = "2026-07-28"
)

// onePixelPNG is a 1x1 PNG.
var onePixelPNG, _ = base64.StdEncoding.DecodeString(
	"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==")

func text(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

func echo(_ context.Context, _ *mcp.CallToolRequest, in echoIn) (*mcp.CallToolResult, any, error) {
	return text(in.Text), nil, nil
}

func add(_ context.Context, _ *mcp.CallToolRequest, in addIn) (*mcp.CallToolResult, addOut, error) {
	return nil, addOut{Total: in.A + in.B}, nil
}

func image(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
	png := &mcp.ImageContent{Data: onePixelPNG, MIMEType: "image/png"}
	return &mcp.CallToolResult{Content: []mcp.Content{png}}, nil, nil
}

func boom(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
	return nil, nil, errors.New("kaboom")
}

// ask asks the user for a color: by input_required on a stateless session,
// by an elicitation request mid-call on a handshake-era one.
func ask(ctx context.Context, r *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	const typ = "type"
	schema := map[string]any{
		typ:          "object",
		"properties": map[string]any{"color": map[string]any{typ: "string", "default": "blue"}},
	}
	if resp, ok := r.Params.InputResponses["c"]; ok {
		er := resp.(*mcp.ElicitResult)
		return text(fmt.Sprintf("%s:%v state=%s", er.Action, er.Content["color"], r.Params.RequestState)), nil, nil
	}
	if v := r.Session.InitializeParams(); v != nil && v.ProtocolVersion >= statelessVersion {
		return &mcp.CallToolResult{
			InputRequests: mcp.InputRequestMap{"c": &mcp.ElicitParams{Message: colorQuestion, RequestedSchema: schema}},
			RequestState:  "s1",
		}, nil, nil
	}
	er, err := r.Session.Elicit(ctx, &mcp.ElicitParams{Message: colorQuestion, RequestedSchema: schema})
	if err != nil {
		return nil, nil, err
	}
	return text(fmt.Sprintf("%s:%v", er.Action, er.Content["color"])), nil, nil
}

func server() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "go-real", Version: "1.0"}, nil)
	mcp.AddTool(s, &mcp.Tool{Name: "echo", Description: "Echo"}, echo)
	mcp.AddTool(s, &mcp.Tool{Name: "add", Description: "Add"}, add)
	mcp.AddTool(s, &mcp.Tool{Name: "image", Description: "Image"}, image)
	mcp.AddTool(s, &mcp.Tool{Name: "boom", Description: "Fails"}, boom)
	mcp.AddTool(s, &mcp.Tool{Name: "ask", Description: "Elicit"}, ask)
	return s
}

func listen(port string, h http.Handler) {
	srv := &http.Server{Addr: "127.0.0.1:" + port, Handler: h, ReadHeaderTimeout: readHeaderTimeout}
	log.Fatal(srv.ListenAndServe())
}

func main() {
	if len(os.Args) < minArgs {
		log.Fatal("usage: goserver stdio | http <port> | stateless <port> | sse <port>")
	}
	byRequest := func(*http.Request) *mcp.Server { return server() }
	switch mode := os.Args[1]; mode {
	case "stdio":
		if err := server().Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			log.Fatal(err)
		}
	case "http", modeStateless:
		opts := &mcp.StreamableHTTPOptions{Stateless: mode == modeStateless}
		listen(os.Args[2], mcp.NewStreamableHTTPHandler(byRequest, opts))
	case "sse":
		listen(os.Args[2], mcp.NewSSEHandler(byRequest, nil))
	default:
		log.Fatal("unknown mode; use stdio, http, stateless or sse")
	}
}
