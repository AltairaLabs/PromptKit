// HTTP request/response mapping for the HTTP tool executor.
//
// This file provides declarative input/output mapping between LLM tool call
// arguments and HTTP requests/responses. It supports:
//   - URL path parameter templating via text/template
//   - Selective argument routing to query params, headers, or body
//   - JMESPath-based body reshaping (both request and response)
//
// All mapping is optional and backward compatible: tools without mapping
// config fall through to the existing behavior.

package tools

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"text/template"
	"text/template/parse"

	"github.com/jmespath/go-jmespath"
)

// templateVarRe matches Go template variable references like {{.foo}} or {{ .bar }}.
var templateVarRe = regexp.MustCompile(`\{\{\s*\.(\w+)\s*\}\}`)

// RequestMapper maps tool arguments to HTTP request components.
// Implementations can customize URL templating, argument partitioning, and body building.
type RequestMapper interface {
	// RenderURL applies path parameter substitution to the URL template.
	RenderURL(urlTemplate string, args map[string]any) (string, error)

	// PartitionArgs splits tool arguments into query, header, and body buckets
	// based on the mapping configuration.
	PartitionArgs(args map[string]any, cfg *RequestMapping, urlTemplate string) (query, header, body map[string]any)

	// BuildBody produces the JSON request body from the body arguments.
	// If a JMESPath body_mapping is configured, it reshapes the args.
	BuildBody(bodyArgs map[string]any, jmespathExpr string) (json.RawMessage, error)

	// RenderHeaders applies template interpolation to header values.
	RenderHeaders(templates map[string]string, args map[string]any) (map[string]string, error)
}

// ResponseMapper maps HTTP response bodies to tool results.
// Implementations can customize response extraction and reshaping.
type ResponseMapper interface {
	// MapResponse applies a JMESPath expression to reshape the response JSON.
	MapResponse(response json.RawMessage, jmespathExpr string) (json.RawMessage, error)
}

// DefaultRequestMapper is the built-in implementation of RequestMapper.
// It uses text/template for URL/header interpolation and JMESPath for body reshaping.
type DefaultRequestMapper struct{}

// DefaultResponseMapper is the built-in implementation of ResponseMapper.
// It uses JMESPath for response extraction and reshaping.
type DefaultResponseMapper struct{}

// --- RequestMapper implementation ---

// ErrURLDotSegment is returned by [DefaultRequestMapper.RenderURL] when the
// rendered path has a "." or ".." segment. Escaping cannot neutralize a value
// that is a whole dot segment, and a server resolving it would serve a
// different path from the one the template names.
var ErrURLDotSegment = errors.New(`http tool URL path has a "." or ".." segment`)

// RenderURL applies Go text/template substitution to a URL string.
// Template variables like {{.user_id}} are replaced with values from args.
//
// Every value is escaped for where it lands, so an argument cannot change the
// request's path or query: values before the template's first literal "?" or
// "#" are escaped with [url.PathEscape], and values after it with
// [url.QueryEscape]. A rendered path with a "." or ".." segment fails with
// [ErrURLDotSegment].
func (m *DefaultRequestMapper) RenderURL(urlTemplate string, args map[string]any) (string, error) {
	if !strings.Contains(urlTemplate, "{{") {
		return urlTemplate, nil
	}
	tmpl, err := template.New("url").Option("missingkey=default").Funcs(urlEscapeFuncs).Parse(urlTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}
	escapeURLActions(tmpl.Root, urlQueryOffset(tmpl.Root))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, args); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}
	rendered := buf.String()
	if hasDotSegment(rendered) {
		return "", fmt.Errorf("%w: %s", ErrURLDotSegment, rendered)
	}
	return rendered, nil
}

// PartitionArgs splits tool arguments into query parameters and body fields.
// The header bucket is always empty: headers come from RequestMapping.HeaderParams
// templates. Arguments consumed by URL templates and header templates, listed
// in Exclude, or routed to the query are left out of the body. With a nil cfg
// every argument goes to the body.
func (m *DefaultRequestMapper) PartitionArgs(
	args map[string]any, cfg *RequestMapping, urlTemplate string,
) (query, header, body map[string]any) {
	if cfg == nil {
		return nil, nil, args
	}

	excluded := buildExcludeSet(cfg, urlTemplate)

	query = make(map[string]any)
	header = make(map[string]any)
	body = make(map[string]any)

	// Route explicitly listed query params
	for _, key := range cfg.QueryParams {
		if v, ok := args[key]; ok {
			query[key] = v
		}
	}

	// Copy remaining args to body, skipping excluded keys
	for k, v := range args {
		if excluded[k] {
			continue
		}
		if _, isQuery := query[k]; isQuery {
			continue
		}
		body[k] = v
	}

	return query, header, body
}

// BuildBody produces the JSON body from args, optionally reshaped via JMESPath.
func (m *DefaultRequestMapper) BuildBody(bodyArgs map[string]any, jmespathExpr string) (json.RawMessage, error) {
	if len(bodyArgs) == 0 {
		return nil, nil
	}
	if jmespathExpr == "" {
		return json.Marshal(bodyArgs)
	}
	return applyJMESPath(bodyArgs, jmespathExpr)
}

// RenderHeaders applies text/template substitution to header value templates.
func (m *DefaultRequestMapper) RenderHeaders(
	templates map[string]string, args map[string]any,
) (map[string]string, error) {
	if len(templates) == 0 {
		return nil, nil
	}

	result := make(map[string]string, len(templates))
	for key, tmpl := range templates {
		if !strings.Contains(tmpl, "{{") {
			result[key] = tmpl
			continue
		}
		rendered, err := renderTemplate("header-"+key, tmpl, args)
		if err != nil {
			return nil, fmt.Errorf("header %q template: %w", key, err)
		}
		result[key] = rendered
	}
	return result, nil
}

// --- ResponseMapper implementation ---

// MapResponse applies a JMESPath expression to extract or reshape a JSON response.
func (m *DefaultResponseMapper) MapResponse(
	response json.RawMessage, jmespathExpr string,
) (json.RawMessage, error) {
	if jmespathExpr == "" {
		return response, nil
	}

	var data any
	if err := json.Unmarshal(response, &data); err != nil {
		return response, nil // non-JSON passes through
	}

	return applyJMESPath(data, jmespathExpr)
}

// --- Helpers ---

// renderTemplate executes a Go text/template with the given data.
func renderTemplate(name, tmplStr string, data any) (string, error) {
	tmpl, err := template.New(name).Option("missingkey=default").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}
	return buf.String(), nil
}

const (
	urlPathEscapeFunc  = "promptkitURLPathEscape"
	urlQueryEscapeFunc = "promptkitURLQueryEscape"
)

// urlEscapeFuncs are appended to every URL template action by
// escapeURLActions. A missing argument arrives as nil and renders as
// "<no value>", as an unescaped template action would print it.
var urlEscapeFuncs = template.FuncMap{
	urlPathEscapeFunc:  func(v any) string { return url.PathEscape(urlValueString(v)) },
	urlQueryEscapeFunc: func(v any) string { return url.QueryEscape(urlValueString(v)) },
}

func urlValueString(v any) string {
	if v == nil {
		return "<no value>"
	}
	return fmt.Sprint(v)
}

// urlQueryOffset returns the byte offset of the first literal "?" or "#" in
// the template text, where the path ends, or -1 when the template has none.
func urlQueryOffset(root *parse.ListNode) int {
	for _, n := range root.Nodes {
		text, ok := n.(*parse.TextNode)
		if !ok {
			continue
		}
		if i := bytes.IndexAny(text.Text, "?#"); i >= 0 {
			return int(text.Pos) + i
		}
	}
	return -1
}

// escapeURLActions pipes the output of every action in the template through
// the path or query escaper, chosen by whether the action sits before or after
// queryOffset. Actions nested in if, range and with blocks are escaped too.
func escapeURLActions(list *parse.ListNode, queryOffset int) {
	if list == nil {
		return
	}
	for _, n := range list.Nodes {
		switch node := n.(type) {
		case *parse.ActionNode:
			escapeURLAction(node, queryOffset)
		case *parse.IfNode:
			escapeURLActions(node.List, queryOffset)
			escapeURLActions(node.ElseList, queryOffset)
		case *parse.RangeNode:
			escapeURLActions(node.List, queryOffset)
			escapeURLActions(node.ElseList, queryOffset)
		case *parse.WithNode:
			escapeURLActions(node.List, queryOffset)
			escapeURLActions(node.ElseList, queryOffset)
		}
	}
}

// escapeURLAction appends the escaper to one action's pipeline. Variable
// declarations print nothing, so they are left alone.
func escapeURLAction(node *parse.ActionNode, queryOffset int) {
	if len(node.Pipe.Decl) > 0 {
		return
	}
	fn := urlPathEscapeFunc
	if queryOffset >= 0 && int(node.Pos) > queryOffset {
		fn = urlQueryEscapeFunc
	}
	node.Pipe.Cmds = append(node.Pipe.Cmds, &parse.CommandNode{
		NodeType: parse.NodeCommand,
		Pos:      node.Pos,
		Args:     []parse.Node{parse.NewIdentifier(fn).SetPos(node.Pos)},
	})
}

// hasDotSegment reports whether the path of a rendered URL has a "." or ".."
// segment.
func hasDotSegment(rendered string) bool {
	path := rendered
	if i := strings.Index(path, "://"); i >= 0 {
		path = path[i+len("://"):]
		if j := strings.IndexByte(path, '/'); j >= 0 {
			path = path[j:]
		} else {
			return false
		}
	}
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == "." || seg == ".." {
			return true
		}
	}
	return false
}

// extractTemplateVars returns the set of variable names referenced in a
// Go text/template string (e.g., {{.user_id}} → "user_id").
func extractTemplateVars(tmplStr string) map[string]bool {
	vars := make(map[string]bool)
	for _, match := range templateVarRe.FindAllStringSubmatch(tmplStr, -1) {
		if len(match) > 1 {
			vars[match[1]] = true
		}
	}
	return vars
}

// buildExcludeSet computes the set of arg keys that should NOT appear in the
// request body. This includes:
//   - keys consumed by URL path templates
//   - keys consumed by header templates
//   - keys explicitly listed in cfg.Exclude
//   - keys routed to query params
func buildExcludeSet(cfg *RequestMapping, urlTemplate string) map[string]bool {
	excluded := make(map[string]bool)

	// URL path params
	for k := range extractTemplateVars(urlTemplate) {
		excluded[k] = true
	}

	// Header template params
	for _, tmpl := range cfg.HeaderParams {
		for k := range extractTemplateVars(tmpl) {
			excluded[k] = true
		}
	}

	// Explicit exclusions
	for _, k := range cfg.Exclude {
		excluded[k] = true
	}

	// Query params
	for _, k := range cfg.QueryParams {
		excluded[k] = true
	}

	return excluded
}

// applyJMESPath searches data with a JMESPath expression and returns the
// result as JSON.
func applyJMESPath(data any, expr string) (json.RawMessage, error) {
	result, err := jmespath.Search(expr, data)
	if err != nil {
		return nil, fmt.Errorf("JMESPath expression %q failed: %w", expr, err)
	}
	if result == nil {
		return json.RawMessage("null"), nil
	}
	return json.Marshal(result)
}
