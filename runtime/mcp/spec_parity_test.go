package mcp

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// This file grades the MCP wire types against the spec revision the client
// claims (ProtocolVersion), using a verbatim mirror of that revision's
// published schema in testdata/spec/<revision>/schema.json.
//
// Why: the client claimed 2025-06-18 while CallToolResult.structuredContent —
// new in that revision — was not a field on ToolCallResponse, so it was
// dropped on decode and a spec-conformant server's result reached the model as
// "Operation completed successfully" (#2099). Nothing compared the Go types to
// the spec they claimed, so the gap was invisible.
//
// Three rules, each closing a way that happens:
//
//   - Every spec property of a pinned definition is either a field on the Go
//     type or a specOmission saying why not. A property we drop on decode is
//     data the user never sees; one we cannot send is a feature we do not
//     have. Either way it is a decision, and it is written down here.
//   - Every field on a pinned Go type is a spec property, or a nonSpecField
//     saying why it is sent anyway.
//   - Every struct declared in this package is either pinned or recorded in
//     notWire. A new wire type cannot skip the check by not being listed —
//     absence would otherwise look like success.
//
// Bumping ProtocolVersion requires `make mcp-schema` (the test loads the
// mirror for exactly that revision), and the new revision's properties then
// fail here until each is carried or recorded.
//
// Adding a specOmission is a design decision, not a way to silence the test.

// specOmission records a spec property a pinned Go type does not carry.
type specOmission struct {
	property string
	reason   string
}

// nonSpecField records a field on a pinned Go type that the spec definition
// does not have.
type nonSpecField struct {
	field  string
	reason string
}

// specPin ties a Go wire type to the schema definition(s) it encodes. More
// than one ref means the Go type is a flattened union (e.g. Content stands for
// every ContentBlock variant) and is graded against the union of their
// properties.
type specPin struct {
	value     any
	refs      []string // slash paths from the schema root
	label     string   // spec type name for the docs; defaults to the first ref's
	omissions []specOmission
	nonSpec   []nonSpecField
}

const (
	reasonMetaDropped    = "_meta is not surfaced to callers"
	reasonNotImplemented = "the client does not implement this feature, so it does not advertise it"
	reasonTasks          = "tasks are experimental in this revision; the client does not implement or advertise them"
	reasonSampling       = "sampling is not implemented or advertised (deprecated in 2026-07-28)"
	reasonRequestMeta    = "the client sends no request _meta (progress tokens are not requested)"
	reasonModernOnly     = "2026-07-28 MRTR field, sent only to modern servers and only in answer to input_required"
)

func mcpSpecPins() []specPin {
	return []specPin{
		{
			value: JSONRPCMessage{},
			label: "JSON-RPC message",
			refs: []string{
				"JSONRPCRequest", "JSONRPCNotification",
				"JSONRPCResultResponse", "JSONRPCErrorResponse",
			},
		},
		{value: JSONRPCError{}, refs: []string{"Error"}},
		{
			value:     InitializeRequest{},
			refs:      []string{"InitializeRequest/properties/params"},
			omissions: []specOmission{{"_meta", reasonRequestMeta}},
		},
		{
			value: InitializeResponse{},
			refs:  []string{"InitializeResult"},
			omissions: []specOmission{
				{"_meta", reasonMetaDropped},
			},
		},
		{value: Implementation{}, refs: []string{"Implementation"}},
		{value: Icon{}, refs: []string{"Icon"}},
		{
			value: ClientCapabilities{},
			refs:  []string{"ClientCapabilities"},
			omissions: []specOmission{
				{"experimental", reasonNotImplemented},
				{"roots", reasonNotImplemented},
				{"tasks", reasonTasks},
			},
			nonSpec: []nonSpecField{
				{"logging", "logging is a server capability, not a client one; the client never sets this " +
					"field, so it is never sent. Exported, so it stays until the next major (see LoggingCapability)"},
			},
		},
		{
			value: ServerCapabilities{},
			refs:  []string{"ServerCapabilities"},
			omissions: []specOmission{
				{"completions", "the client does not use completions"},
				{"experimental", "experimental server capabilities are ignored"},
				{"logging", "server log messages are not consumed"},
				{"tasks", reasonTasks},
			},
		},
		{value: ToolsCapability{}, refs: []string{"ServerCapabilities/properties/tools"}},
		{
			value: ResourcesCapability{},
			refs:  []string{"ServerCapabilities/properties/resources"},
			omissions: []specOmission{
				{"subscribe", "the client does not use resources"},
			},
		},
		{value: PromptsCapability{}, refs: []string{"ServerCapabilities/properties/prompts"}},
		{value: ElicitationCapability{}, refs: []string{"ClientCapabilities/properties/elicitation"}},
		{
			value: SamplingCapability{},
			refs:  []string{"ClientCapabilities/properties/sampling"},
			omissions: []specOmission{
				{"context", reasonSampling},
				{"tools", reasonSampling},
			},
		},
		{
			value:     ToolsListRequest{},
			refs:      []string{"ListToolsRequest/properties/params"},
			omissions: []specOmission{{"_meta", reasonRequestMeta}},
		},
		{
			value: ToolsListResponse{},
			refs:  []string{"ListToolsResult"},
			omissions: []specOmission{
				{"_meta", reasonMetaDropped},
			},
		},
		{
			value: Tool{},
			refs:  []string{"Tool"},
			omissions: []specOmission{
				{"execution", "execution hints (task support) are not carried; the client does not implement tasks"},
			},
		},
		{value: ToolAnnotations{}, refs: []string{"ToolAnnotations"}},
		{
			value: ToolCallRequest{},
			refs:  []string{"CallToolRequest/properties/params"},
			omissions: []specOmission{
				{"_meta", reasonRequestMeta},
				{"task", reasonTasks},
			},
			nonSpec: []nonSpecField{
				{"inputResponses", reasonModernOnly},
				{"requestState", reasonModernOnly},
			},
		},
		{value: ToolCallResponse{}, refs: []string{"CallToolResult"}},
		{
			value: Content{},
			label: "ContentBlock",
			refs: []string{
				"TextContent", "ImageContent", "AudioContent",
				"ResourceLink", "EmbeddedResource",
			},
		},
		{
			value: ResourceContents{},
			label: "ResourceContents",
			refs:  []string{"TextResourceContents", "BlobResourceContents"},
		},
		{value: Annotations{}, refs: []string{"Annotations"}},
	}
}

// notWire records structs in this package that are not MCP wire types, and
// why. A struct that is neither pinned nor listed here fails
// TestMCPSpecCoverage.
var notWire = map[string]string{
	"ClientOptions":          "client configuration, never serialized to a server",
	"StdioClient":            "transport implementation",
	"SSEClient":              "transport implementation",
	"StreamableClient":       "transport implementation",
	"httpAutoClient":         "transport selection: Streamable HTTP with the HTTP+SSE fallback",
	"httpDoer":               "HTTP plumbing below the MCP message layer",
	"challengeParser":        "WWW-Authenticate parsing state",
	"AuthChallenge":          "an HTTP authorization challenge handed to the host's Authorizer; not an MCP message",
	"WWWAuthenticate":        "a parsed HTTP WWW-Authenticate challenge; not an MCP message",
	"AuthError":              "the Go error for a request that stays unauthorized",
	"RegistryOptions":        "registry configuration",
	"RegistryImpl":           "registry implementation",
	"ServerConfigData":       "PromptKit's server config file shape, not MCP",
	"ServerConfig":           "PromptKit's server configuration, not MCP",
	"ToolFilter":             "PromptKit's tool allow/deny configuration, not MCP",
	"LoggingCapability":      "not an MCP client capability; see the nonSpec entry on ClientCapabilities",
	"RPCError":               "the Go error a JSON-RPC error response becomes; the wire shape is JSONRPCError",
	"callOpts":               "per-call retry policy",
	"callState":              "per-call recovery bookkeeping",
	"paramHeader":            "an x-mcp-header designation parsed from a tool's inputSchema",
	"inputRequiredError":     "carries an input_required result to the caller that retries",
	"unsupportedVersionData": "the data of an UnsupportedProtocolVersionError; pinned with the 2026-07-28 mirror",
	"DiscoverResult":         "a 2026-07-28 type; pinned with the 2026-07-28 mirror",
	"InputRequiredResult":    "a 2026-07-28 type; pinned with the 2026-07-28 mirror",
	"InputRequest":           "a 2026-07-28 type; pinned with the 2026-07-28 mirror",
	"streamCursor":           "resume position of an SSE stream, below the MCP message layer",
	"ElicitRequest":          "the handler-facing form of elicitation/create params; pinned with the 2025-11-25 mirror",
	"ElicitResult":           "the handler-facing form of the elicitation result; pinned with the 2025-11-25 mirror",
	"httpStatusError":        "an HTTP status without a JSON-RPC body, below the MCP message layer",
	"request":                "an outgoing message before it is framed; the wire shape is JSONRPCMessage",
	"session":                "the protocol state machine",
	"stdioConn":              "transport implementation",
	"pendingRequests":        "transport bookkeeping",
	"sseEvent":               "an SSE frame, below the MCP message layer",
	"sseTransport":           "transport implementation",
	"streamableTransport":    "transport implementation",
}

func loadMCPSpec(t *testing.T) map[string]any {
	t.Helper()
	path := filepath.Join("testdata", "spec", ProtocolVersion, "schema.json")
	raw, err := os.ReadFile(path)
	require.NoError(t, err,
		"no mirrored MCP schema for the claimed ProtocolVersion %q; run `make mcp-schema`", ProtocolVersion)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))
	return doc
}

func TestMCPSpecMirrorIsTheClaimedRevision(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("testdata", "spec"))
	require.NoError(t, err)
	var revisions []string
	for _, e := range entries {
		if e.IsDir() {
			revisions = append(revisions, e.Name())
		}
	}
	require.Equal(t, []string{ProtocolVersion}, revisions,
		"testdata/spec must mirror exactly the revision the client claims; run `make mcp-schema`")
}

// resolveSpecRef walks a slash path from the schema's definitions to a
// definition, following $refs on the way. Schemas through 2025-06-18 keep
// definitions under "definitions" (draft-07); later ones under "$defs"
// (2020-12), with request params behind a $ref (SEP-1319).
func resolveSpecRef(t *testing.T, doc map[string]any, ref string) map[string]any {
	t.Helper()
	defs := specDefinitions(t, doc)
	var node any = defs
	for _, seg := range strings.Split(ref, "/") {
		node = followSpecRef(t, defs, node, ref)
		m, ok := node.(map[string]any)
		require.Truef(t, ok, "schema path %q: %q is not an object", ref, seg)
		node, ok = m[seg]
		require.Truef(t, ok, "schema path %q: no %q — has the spec renamed it?", ref, seg)
	}
	def, ok := followSpecRef(t, defs, node, ref).(map[string]any)
	require.Truef(t, ok, "schema path %q is not an object", ref)
	return def
}

func specDefinitions(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	for _, key := range []string{"$defs", "definitions"} {
		if defs, ok := doc[key].(map[string]any); ok {
			return defs
		}
	}
	t.Fatal("mirrored schema has neither $defs nor definitions")
	return nil
}

// followSpecRef resolves a {"$ref": "#/$defs/Name"} node to its definition.
func followSpecRef(t *testing.T, defs map[string]any, node any, ref string) any {
	t.Helper()
	for range 10 {
		m, ok := node.(map[string]any)
		if !ok {
			return node
		}
		target, ok := m["$ref"].(string)
		if !ok {
			return node
		}
		name := target[strings.LastIndex(target, "/")+1:]
		node, ok = defs[name]
		require.Truef(t, ok, "schema path %q: $ref %q does not resolve", ref, target)
	}
	t.Fatalf("schema path %q: $ref chain too deep", ref)
	return nil
}

// specProperties returns the property names and required set of the given
// definitions, unioned.
func specProperties(t *testing.T, doc map[string]any, refs []string) (props, required map[string]bool) {
	t.Helper()
	props, required = map[string]bool{}, map[string]bool{}
	for _, ref := range refs {
		def := resolveSpecRef(t, doc, ref)
		if p, ok := def["properties"].(map[string]any); ok {
			for name := range p {
				props[name] = true
			}
		}
		// A required property of one union variant is not required of the
		// flattened type, so only single-definition pins enforce required.
		if len(refs) == 1 {
			if req, ok := def["required"].([]any); ok {
				for _, r := range req {
					required[r.(string)] = true
				}
			}
		}
	}
	return props, required
}

// jsonFields returns the JSON field names of a struct and whether each is
// omitempty.
func jsonFields(typ reflect.Type) map[string]bool {
	fields := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if name == "" {
			name = f.Name
		}
		fields[name] = strings.Contains(opts, "omitempty")
	}
	return fields
}

func TestMCPSpecParity(t *testing.T) {
	doc := loadMCPSpec(t)

	for _, pin := range mcpSpecPins() {
		typ := reflect.TypeOf(pin.value)
		t.Run(typ.Name(), func(t *testing.T) {
			props, required := specProperties(t, doc, pin.refs)
			fields := jsonFields(typ)

			omitted := map[string]bool{}
			for _, o := range pin.omissions {
				require.NotEmptyf(t, o.reason, "omission %q needs a reason", o.property)
				require.Truef(t, props[o.property],
					"omission %q is not a property of %v; remove the stale entry", o.property, pin.refs)
				require.Falsef(t, fieldPresent(fields, o.property),
					"%s now carries %q; remove the stale omission", typ.Name(), o.property)
				omitted[o.property] = true
			}
			extra := map[string]bool{}
			for _, n := range pin.nonSpec {
				require.NotEmptyf(t, n.reason, "non-spec field %q needs a reason", n.field)
				require.Falsef(t, props[n.field],
					"%q is a spec property of %v now; remove the stale nonSpec entry", n.field, pin.refs)
				extra[n.field] = true
			}

			for _, prop := range sortedKeys(props) {
				if !fieldPresent(fields, prop) && !omitted[prop] {
					t.Errorf("%s drops spec property %q of %v: carry it, or record a specOmission saying why not",
						typ.Name(), prop, pin.refs)
				}
			}
			for _, field := range sortedKeys(fields) {
				if !props[field] && !extra[field] {
					t.Errorf("%s field %q is not in %v (MCP %s): remove it, or record a nonSpecField saying why",
						typ.Name(), field, pin.refs, ProtocolVersion)
				}
			}
			for _, prop := range sortedKeys(required) {
				if omitEmpty, ok := fields[prop]; ok && omitEmpty {
					t.Errorf("%s field %q is required by %v but tagged omitempty", typ.Name(), prop, pin.refs)
				}
			}
		})
	}
}

func fieldPresent(fields map[string]bool, name string) bool {
	_, ok := fields[name]
	return ok
}

// TestMCPSpecCoverage requires every struct declared in this package to be
// pinned or recorded in notWire, so a new wire type cannot go unchecked.
func TestMCPSpecCoverage(t *testing.T) {
	pinned := map[string]bool{}
	for _, pin := range mcpSpecPins() {
		pinned[reflect.TypeOf(pin.value).Name()] = true
	}

	declared := declaredStructs(t)
	for _, name := range declared {
		if !pinned[name] && notWire[name] == "" {
			t.Errorf("struct %s is neither pinned to the MCP schema in mcpSpecPins nor recorded in notWire", name)
		}
	}
	declaredSet := map[string]bool{}
	for _, name := range declared {
		declaredSet[name] = true
	}
	for _, name := range sortedKeys(notWire) {
		if !declaredSet[name] {
			t.Errorf("notWire lists %s, which this package no longer declares; remove it", name)
		}
	}
}

func declaredStructs(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	matches, err := filepath.Glob("*.go")
	require.NoError(t, err)
	var names []string
	for _, file := range matches {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, file, nil, 0)
		require.NoError(t, err)
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts := spec.(*ast.TypeSpec)
				if _, isStruct := ts.Type.(*ast.StructType); isStruct {
					names = append(names, ts.Name.Name)
				}
			}
		}
	}
	sort.Strings(names)
	return names
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// The MCP how-to states what the client implements. That statement is
// rendered from mcpSpecPins, so the docs cannot claim more (or less) than
// TestMCPSpecParity enforces. Regenerate with `make mcp-spec-docs`.
const (
	specDocPath  = "../../docs/src/content/docs/runtime/how-to/tools/integrate-mcp.md"
	specDocBegin = "<!-- BEGIN GENERATED: mcp-spec-support. Do not edit; run `make mcp-spec-docs`. -->"
	specDocEnd   = "<!-- END GENERATED: mcp-spec-support -->"
)

// specLabel names a pin's spec type for the docs: "InitializeRequest params"
// for "InitializeRequest/properties/params".
func specLabel(pin specPin) string {
	if pin.label != "" {
		return pin.label
	}
	parts := strings.Split(strings.TrimPrefix(pin.refs[0], ""), "/properties/")
	return strings.Join(parts, " ")
}

func renderMCPSpecSupport() string {
	var b strings.Builder
	b.WriteString(specDocBegin + "\n\n")
	b.WriteString("PromptKit's MCP client implements protocol revision **" + ProtocolVersion +
		"** (`mcp.ProtocolVersion`). CI checks every message type it sends or reads against that " +
		"revision's published schema, so the table below is the complete list of spec fields the " +
		"client does not carry; every other field is carried.\n\n" +
		"That check covers message fields. Behaviour is checked by scenario tests and by the official " +
		"[MCP conformance suite](https://github.com/modelcontextprotocol/conformance) (`make mcp-conformance`); " +
		"known gaps are tracked in [#2100](https://github.com/AltairaLabs/PromptKit/issues/2100).\n\n")
	b.WriteString("Spec fields PromptKit does not carry:\n\n")
	b.WriteString("| Spec type | Field | Why |\n|---|---|---|\n")
	var nonSpec []string
	for _, pin := range mcpSpecPins() {
		label := specLabel(pin)
		for _, o := range pin.omissions {
			b.WriteString("| " + label + " | `" + o.property + "` | " + o.reason + " |\n")
		}
		for _, n := range pin.nonSpec {
			nonSpec = append(nonSpec, "- "+label+" `"+n.field+"`: "+n.reason+"\n")
		}
	}
	if len(nonSpec) > 0 {
		b.WriteString("\nFields PromptKit declares that the spec does not define:\n\n")
		for _, line := range nonSpec {
			b.WriteString(line)
		}
	}
	b.WriteString("\n" + specDocEnd)
	return b.String()
}

func TestMCPSpecSupportDocIsCurrent(t *testing.T) {
	raw, err := os.ReadFile(specDocPath)
	require.NoError(t, err)
	doc := string(raw)

	start := strings.Index(doc, specDocBegin)
	end := strings.Index(doc, specDocEnd)
	require.True(t, start >= 0 && end > start,
		"%s has lost its generated mcp-spec-support block; restore the BEGIN/END markers", specDocPath)

	want := renderMCPSpecSupport()
	got := doc[start : end+len(specDocEnd)]
	if got == want {
		return
	}
	if os.Getenv("UPDATE_SPEC_DOCS") == "1" {
		updated := doc[:start] + want + doc[end+len(specDocEnd):]
		require.NoError(t, os.WriteFile(specDocPath, []byte(updated), 0o600))
		return
	}
	t.Errorf("%s states MCP support that no longer matches mcpSpecPins; run `make mcp-spec-docs`", specDocPath)
}
