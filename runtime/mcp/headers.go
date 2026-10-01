package mcp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// Request metadata headers (2026-07-28 basic/transports/streamable-http,
// "Request Metadata"). They mirror body fields so intermediaries can route
// on them without parsing the body.
const (
	headerMcpMethod      = "Mcp-Method"
	headerMcpName        = "Mcp-Name"
	headerMcpParamPrefix = "Mcp-Param-"
	// xMcpHeader is the inputSchema extension that designates a tool
	// parameter to mirror into an Mcp-Param-{name} header.
	xMcpHeader = "x-mcp-header"

	base64SentinelPrefix = "=?base64?"
	base64SentinelSuffix = "?="
	// maxSafeInteger is the largest integer JavaScript represents exactly;
	// x-mcp-header integers must stay within ±(2^53−1).
	maxSafeInteger = 1<<53 - 1
)

// encodeHeaderValue returns a value safe to send as an HTTP header. A value
// outside visible ASCII and space/tab, with leading or trailing whitespace,
// or that already looks like the sentinel, is sent as =?base64?…?= over its
// UTF-8 bytes.
func encodeHeaderValue(v string) string {
	if headerValueIsPlain(v) {
		return v
	}
	return base64SentinelPrefix + base64.StdEncoding.EncodeToString([]byte(v)) + base64SentinelSuffix
}

func headerValueIsPlain(v string) bool {
	if strings.HasPrefix(v, base64SentinelPrefix) && strings.HasSuffix(v, base64SentinelSuffix) {
		return false
	}
	if v != strings.TrimSpace(v) {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if c != '\t' && (c < 0x20 || c > 0x7E) {
			return false
		}
	}
	return true
}

// nameSourceParam is the params field Mcp-Name mirrors, per method.
var nameSourceParam = map[string]string{
	methodToolsCall:     paramName,
	methodPromptsGet:    paramName,
	methodResourcesRead: "uri",
}

// setStandardHeaders sets Mcp-Method and, where the method has one,
// Mcp-Name from the request body.
func setStandardHeaders(h http.Header, method string, params json.RawMessage) {
	h.Set(headerMcpMethod, method)
	field, ok := nameSourceParam[method]
	if !ok {
		return
	}
	var p map[string]json.RawMessage
	if json.Unmarshal(params, &p) != nil {
		return
	}
	var name string
	if json.Unmarshal(p[field], &name) == nil && name != "" {
		h.Set(headerMcpName, encodeHeaderValue(name))
	}
}

// paramHeader is one x-mcp-header designation: the header name suffix and
// the chain of properties keys leading to the annotated parameter.
type paramHeader struct {
	name string
	path []string
}

// toolParamHeaders returns a tool's x-mcp-header designations, or an error
// if any violates the constraints (2026-07-28 server/tools, "x-mcp-header").
// A client on Streamable HTTP MUST exclude such a tool from tools/list.
//
// The schema is walked only through "properties" keys: an annotation reached
// through items, a composition or conditional keyword, or $ref is invalid,
// and $ref is never dereferenced.
func toolParamHeaders(inputSchema json.RawMessage) ([]paramHeader, error) {
	var root map[string]any
	if len(inputSchema) == 0 || json.Unmarshal(inputSchema, &root) != nil {
		return nil, nil
	}
	var out []paramHeader
	if err := collectParamHeaders(root, nil, true, &out); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, h := range out {
		key := strings.ToLower(h.name)
		if seen[key] {
			return nil, fmt.Errorf("x-mcp-header %q is not unique (case-insensitively)", h.name)
		}
		seen[key] = true
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

// collectParamHeaders walks a schema node. reachable reports whether the
// node is statically reachable from the root through properties keys only.
func collectParamHeaders(node map[string]any, path []string, reachable bool, out *[]paramHeader) error {
	if raw, ok := node[xMcpHeader]; ok {
		if err := recordParamHeader(node, raw, path, reachable, out); err != nil {
			return err
		}
	}
	for key, child := range node {
		switch key {
		case xMcpHeader:
		case "properties":
			if err := walkProperties(child, path, reachable, out); err != nil {
				return err
			}
		default:
			// Any other keyword (items, anyOf, $defs, if/then, ...) leads to
			// nodes that are not statically reachable.
			if err := walkUnreachable(child, path, out); err != nil {
				return err
			}
		}
	}
	return nil
}

// recordParamHeader validates one x-mcp-header annotation and records it.
func recordParamHeader(node map[string]any, raw any, path []string, reachable bool, out *[]paramHeader) error {
	if !reachable || len(path) == 0 {
		return fmt.Errorf("x-mcp-header at %q is not reachable from the schema root through properties alone",
			strings.Join(path, "."))
	}
	name, err := validParamHeaderName(raw)
	if err != nil {
		return err
	}
	if err := validParamHeaderType(node); err != nil {
		return fmt.Errorf("x-mcp-header %q: %w", name, err)
	}
	*out = append(*out, paramHeader{name: name, path: append([]string(nil), path...)})
	return nil
}

func walkProperties(child any, path []string, reachable bool, out *[]paramHeader) error {
	props, ok := child.(map[string]any)
	if !ok {
		return nil
	}
	for prop, sub := range props {
		m, ok := sub.(map[string]any)
		if !ok {
			continue
		}
		if err := collectParamHeaders(m, append(path[:len(path):len(path)], prop), reachable, out); err != nil {
			return err
		}
	}
	return nil
}

func walkUnreachable(v any, path []string, out *[]paramHeader) error {
	switch n := v.(type) {
	case map[string]any:
		return collectParamHeaders(n, path, false, out)
	case []any:
		for _, item := range n {
			if err := walkUnreachable(item, path, out); err != nil {
				return err
			}
		}
	}
	return nil
}

func validParamHeaderName(raw any) (string, error) {
	name, ok := raw.(string)
	if !ok || name == "" {
		return "", fmt.Errorf("x-mcp-header must be a non-empty string")
	}
	for i := 0; i < len(name); i++ {
		if !isTokenChar(name[i]) {
			return "", fmt.Errorf("x-mcp-header %q is not an HTTP field-name token", name)
		}
	}
	return name, nil
}

// isTokenChar reports whether c is an RFC 9110 tchar.
func isTokenChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0
}

func validParamHeaderType(node map[string]any) error {
	switch t := node["type"].(type) {
	case string:
		switch t {
		case "string", "integer", "boolean":
			return nil
		}
		return fmt.Errorf("applies to a %q parameter; only string, integer and boolean are allowed", t)
	default:
		return fmt.Errorf("applies to a parameter without a single primitive type")
	}
}

// setParamHeaders sets an Mcp-Param-{name} header for each designated
// parameter present in the call's arguments. A parameter that is absent or
// null gets no header.
func setParamHeaders(h http.Header, headers []paramHeader, arguments json.RawMessage) error {
	if len(headers) == 0 {
		return nil
	}
	var args any
	if len(arguments) == 0 || json.Unmarshal(arguments, &args) != nil {
		return nil
	}
	for _, ph := range headers {
		v, ok := lookupPath(args, ph.path)
		if !ok || v == nil {
			continue
		}
		s, err := paramHeaderValue(v)
		if err != nil {
			return fmt.Errorf("parameter for header %s%s: %w", headerMcpParamPrefix, ph.name, err)
		}
		h.Set(headerMcpParamPrefix+ph.name, encodeHeaderValue(s))
	}
	return nil
}

func lookupPath(v any, path []string) (any, bool) {
	for _, key := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		v, ok = m[key]
		if !ok {
			return nil, false
		}
	}
	return v, true
}

// paramHeaderValue converts a primitive argument to its header string.
func paramHeaderValue(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case bool:
		return strconv.FormatBool(x), nil
	case float64:
		if x != math.Trunc(x) || math.Abs(x) > maxSafeInteger {
			return "", fmt.Errorf("value %v is not a safe integer", x)
		}
		return strconv.FormatInt(int64(x), 10), nil
	}
	return "", fmt.Errorf("value of type %T cannot be sent as a header", v)
}
