package mcp

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
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
	reasonMetaDropped       = "_meta is not surfaced to callers"
	reasonNotImplemented    = "the client does not implement this feature, so it does not advertise it"
	reasonTasks             = "tasks are experimental in this revision; the client does not implement or advertise them"
	reasonSampling          = "sampling is not implemented or advertised (deprecated in 2026-07-28)"
	reasonStandaloneRequest = "in 2025-11-25 these are standalone JSON-RPC requests from the server, answered " +
		"by the session; as 2026-07-28 input requests inside an input_required result they carry no envelope"
	reasonRequestMeta = "set by the session, not the caller: a 2026-07-28 request carries the protocol metadata " +
		"(version, client info, capabilities); a handshake-era request carries none"
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
			value: InitializeRequest{},
			refs:  []string{"InitializeRequest/properties/params"},
			omissions: []specOmission{
				{"_meta", "the handshake request carries no metadata (progress tokens are not requested)"},
			},
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
		{value: ToolsListResponse{}, refs: []string{"ListToolsResult"}},
		{value: Tool{}, refs: []string{"Tool"}},
		{value: ToolExecution{}, refs: []string{"ToolExecution"}},
		{value: ToolAnnotations{}, refs: []string{"ToolAnnotations"}},
		{
			value: ToolCallRequest{},
			refs:  []string{"CallToolRequest/properties/params"},
			omissions: []specOmission{
				{"_meta", reasonRequestMeta},
				{"task", reasonTasks},
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
		{value: DiscoverResult{}, refs: []string{"DiscoverResult"}},
		{value: InputRequiredResult{}, refs: []string{"InputRequiredResult"}},
		{
			value: InputRequest{},
			label: "InputRequest",
			refs:  []string{"ElicitRequest", "CreateMessageRequest", "ListRootsRequest"},
			omissions: []specOmission{
				{"id", reasonStandaloneRequest},
				{"jsonrpc", reasonStandaloneRequest},
			},
		},
		{
			value: unsupportedVersionData{},
			label: "UnsupportedProtocolVersionError data",
			refs:  []string{"UnsupportedProtocolVersionError/properties/error/allOf/1/properties/data"},
		},
		{
			value: ElicitRequest{},
			label: "ElicitRequest params",
			refs:  []string{"ElicitRequestFormParams", "ElicitRequestURLParams"},
			omissions: []specOmission{
				{"_meta", reasonMetaDropped},
				{"task", reasonTasks},
				{"elicitationId", "URL-mode elicitation is not advertised, so its correlation id is not used"},
			},
		},
		{
			value:     ElicitResult{},
			refs:      []string{"ElicitResult"},
			omissions: []specOmission{{"_meta", "the client attaches no metadata to its answers"}},
		},
	}
}

// notWire records structs in this package that are not MCP wire types, and
// why. A struct that is neither pinned nor listed here fails
// TestMCPSpecCoverage.
var notWire = map[string]string{
	"ClientOptions":       "client configuration, never serialized to a server",
	"StdioClient":         "transport implementation",
	"SSEClient":           "transport implementation",
	"StreamableClient":    "transport implementation",
	"httpClient":          "the HTTP clients' shared lifecycle",
	"httpAutoClient":      "transport selection: Streamable HTTP with the HTTP+SSE fallback",
	"httpDoer":            "HTTP plumbing below the MCP message layer",
	"challengeParser":     "WWW-Authenticate parsing state",
	"AuthChallenge":       "an HTTP authorization challenge handed to the host's Authorizer; not an MCP message",
	"WWWAuthenticate":     "a parsed HTTP WWW-Authenticate challenge; not an MCP message",
	"AuthError":           "the Go error for a request that stays unauthorized",
	"RegistryOptions":     "registry configuration",
	"RegistryImpl":        "registry implementation",
	"ServerConfigData":    "PromptKit's server config file shape, not MCP",
	"ServerConfig":        "PromptKit's server configuration, not MCP",
	"ToolFilter":          "PromptKit's tool allow/deny configuration, not MCP",
	"LoggingCapability":   "not an MCP client capability; see the nonSpec entry on ClientCapabilities",
	"RPCError":            "the Go error a JSON-RPC error response becomes; the wire shape is JSONRPCError",
	"callOpts":            "per-call retry policy",
	"callState":           "per-call recovery bookkeeping",
	"paramHeader":         "an x-mcp-header designation parsed from a tool's inputSchema",
	"inputRequiredError":  "carries an input_required result to the caller that retries",
	"streamCursor":        "resume position of an SSE stream, below the MCP message layer",
	"httpStatusError":     "an HTTP status without a JSON-RPC body, below the MCP message layer",
	"request":             "an outgoing message before it is framed; the wire shape is JSONRPCMessage",
	"session":             "the protocol state machine",
	"stdioConn":           "transport implementation",
	"pendingRequests":     "transport bookkeeping",
	"sseEvent":            "an SSE frame, below the MCP message layer",
	"sseTransport":        "transport implementation",
	"streamableTransport": "transport implementation",
}

// claimedRevisions are the revisions the client claims, newest first: the
// stateless one and the newest handshake one.
var claimedRevisions = []string{ProtocolVersion, LegacyProtocolVersion}

// loadMCPSpecs loads the mirrored schema of each claimed revision.
func loadMCPSpecs(t *testing.T) map[string]map[string]any {
	t.Helper()
	docs := map[string]map[string]any{}
	for _, rev := range claimedRevisions {
		path := filepath.Join("testdata", "spec", rev, "schema.json")
		raw, err := os.ReadFile(path)
		require.NoError(t, err, "no mirrored MCP schema for the claimed revision %q; run `make mcp-schema`", rev)
		var doc map[string]any
		require.NoError(t, json.Unmarshal(raw, &doc))
		docs[rev] = doc
	}
	return docs
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
	want := append([]string(nil), claimedRevisions...)
	sort.Strings(want)
	require.Equal(t, want, revisions,
		"testdata/spec must mirror exactly the revisions the client claims; run `make mcp-schema`")
}

// resolveSpecRef walks a slash path from the schema's definitions to a
// definition, following $refs on the way. It reports false when the
// revision has no such definition. Schemas through 2025-06-18 keep
// definitions under "definitions" (draft-07); later ones under "$defs"
// (2020-12), with request params behind a $ref (SEP-1319).
func resolveSpecRef(t *testing.T, doc map[string]any, ref string) (map[string]any, bool) {
	t.Helper()
	defs := specDefinitions(t, doc)
	var node any = defs
	for _, seg := range strings.Split(ref, "/") {
		node = followSpecRef(t, defs, node, ref)
		if list, ok := node.([]any); ok { // an allOf/anyOf member, by index
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(list) {
				return nil, false
			}
			node = list[i]
			continue
		}
		m, ok := node.(map[string]any)
		if !ok {
			return nil, false
		}
		if node, ok = m[seg]; !ok {
			return nil, false
		}
	}
	def, ok := followSpecRef(t, defs, node, ref).(map[string]any)
	return def, ok
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

// specShape is a pin's spec properties across the claimed revisions.
type specShape struct {
	// props maps each property to the revisions that define it.
	props map[string][]string
	// required maps each revision to the properties it requires.
	required map[string]map[string]bool
}

// specProperties returns the property names of the given definitions,
// unioned across them and across the claimed revisions, with the revisions
// that define each. One Go type serves every revision, so it is graded
// against all of them.
func specProperties(t *testing.T, docs map[string]map[string]any, refs []string) specShape {
	t.Helper()
	shape := specShape{props: map[string][]string{}, required: map[string]map[string]bool{}}
	found := false
	for _, rev := range claimedRevisions {
		for _, ref := range refs {
			def, ok := resolveSpecRef(t, docs[rev], ref)
			if !ok {
				continue
			}
			found = true
			if p, ok := def["properties"].(map[string]any); ok {
				for name := range p {
					if !slices.Contains(shape.props[name], rev) {
						shape.props[name] = append(shape.props[name], rev)
					}
				}
			}
			// A required property of one union variant is not required of the
			// flattened type, so only single-definition pins enforce required.
			if len(refs) == 1 {
				req := map[string]bool{}
				if list, ok := def["required"].([]any); ok {
					for _, r := range list {
						req[r.(string)] = true
					}
				}
				shape.required[rev] = req
			}
		}
	}
	require.Truef(t, found, "no claimed revision defines %v — has the spec renamed it?", refs)
	return shape
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
	docs := loadMCPSpecs(t)

	for _, pin := range mcpSpecPins() {
		typ := reflect.TypeOf(pin.value)
		t.Run(typ.Name(), func(t *testing.T) {
			shape := specProperties(t, docs, pin.refs)
			fields := jsonFields(typ)

			omitted := map[string]bool{}
			for _, o := range pin.omissions {
				require.NotEmptyf(t, o.reason, "omission %q needs a reason", o.property)
				require.Truef(t, shape.props[o.property] != nil,
					"omission %q is not a property of %v; remove the stale entry", o.property, pin.refs)
				require.Falsef(t, fieldPresent(fields, o.property),
					"%s now carries %q; remove the stale omission", typ.Name(), o.property)
				omitted[o.property] = true
			}
			extra := map[string]bool{}
			for _, n := range pin.nonSpec {
				require.NotEmptyf(t, n.reason, "non-spec field %q needs a reason", n.field)
				require.Nilf(t, shape.props[n.field],
					"%q is a spec property of %v now; remove the stale nonSpec entry", n.field, pin.refs)
				extra[n.field] = true
			}

			for _, prop := range sortedKeys(shape.props) {
				if !fieldPresent(fields, prop) && !omitted[prop] {
					t.Errorf("%s drops spec property %q of %v (%v): carry it, or record a specOmission saying why not",
						typ.Name(), prop, pin.refs, shape.props[prop])
				}
			}
			for _, field := range sortedKeys(fields) {
				if shape.props[field] == nil && !extra[field] {
					t.Errorf("%s field %q is not in %v (MCP %v): remove it, or record a nonSpecField saying why",
						typ.Name(), field, pin.refs, claimedRevisions)
				}
			}
			for _, rev := range sortedKeys(shape.required) {
				for _, prop := range sortedKeys(shape.required[rev]) {
					if omitEmpty, ok := fields[prop]; ok && omitEmpty && requiredInEveryRevision(shape, prop) {
						t.Errorf("%s field %q is required by %v in every revision but tagged omitempty",
							typ.Name(), prop, pin.refs)
					}
				}
			}
		})
	}
}

// requiredInEveryRevision reports whether every revision that defines the
// pin requires prop. A field required only by one revision has to be
// omitempty to stay absent in the other.
func requiredInEveryRevision(shape specShape, prop string) bool {
	for _, req := range shape.required {
		if !req[prop] {
			return false
		}
	}
	return true
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

func renderMCPSpecSupport(t *testing.T, docs map[string]map[string]any) string {
	t.Helper()
	var b strings.Builder
	b.WriteString(specDocBegin + "\n\n")
	b.WriteString("PromptKit's MCP client implements protocol revision **" + ProtocolVersion +
		"** (`mcp.ProtocolVersion`), the stateless revision, and **" + LegacyProtocolVersion +
		"** (`mcp.LegacyProtocolVersion`), the newest revision with an `initialize` handshake, for " +
		"servers that predate it. It detects which a server speaks, and also accepts the earlier " +
		"handshake revisions a server may choose (2025-06-18, 2025-03-26, 2024-11-05).\n\n" +
		"CI checks every message type the client sends or reads against both revisions' published " +
		"schemas, so the table below is the complete list of spec fields the client does not carry; " +
		"every other field is carried.\n\n" +
		"That check covers message fields. Behaviour is checked by scenario tests and by the official " +
		"[MCP conformance suite](https://github.com/modelcontextprotocol/conformance) (`make mcp-conformance`) " +
		"against both revisions' requirements.\n\n")
	b.WriteString("Spec fields PromptKit does not carry:\n\n")
	b.WriteString("| Spec type | Field | Why |\n|---|---|---|\n")
	var nonSpec []string
	for _, pin := range mcpSpecPins() {
		label := specLabel(pin)
		shape := specProperties(t, docs, pin.refs)
		for _, o := range pin.omissions {
			field := "`" + o.property + "`"
			if revs := shape.props[o.property]; len(revs) < len(claimedRevisions) {
				field += " (" + strings.Join(revs, ", ") + " only)"
			}
			b.WriteString("| " + label + " | " + field + " | " + o.reason + " |\n")
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

	want := renderMCPSpecSupport(t, loadMCPSpecs(t))
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
