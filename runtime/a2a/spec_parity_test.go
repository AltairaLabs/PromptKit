package a2a

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// This file grades the A2A wire types against the specification documents of
// the versions the package speaks, mirrored verbatim in
// testdata/spec/<version>/ by `make a2a-spec`:
//
//   - 1.0: a2a.proto. A2A 1.0's JSON is the ProtoJSON mapping of it, so a
//     message's JSON names are its fields' lowerCamelCase names, and a field
//     marked (google.api.field_behavior) = REQUIRED is required.
//   - 0.3: a2a.json, the JSON schema 0.3 published.
//
// Why: the audit in #2101 found data parts that dropped non-object values,
// card fields that were lost on decode and required fields that were never
// encoded. Nothing compared the Go types to the spec they claimed, so the
// drift was invisible (#2135).
//
// The rules, each closing a way that happens:
//
//   - Every field of a pinned spec type is either written by the Go type's
//     encoder or a specOmission saying why not.
//   - Every field the Go type's encoder writes is a spec field, or a
//     nonSpecField saying why it is sent anyway. The written fields are read
//     from the encoder (a value with every field set, marshaled), not from
//     struct tags, so a custom MarshalJSON is graded on what it writes.
//   - Every field a single-type pin's spec type requires is written even by
//     an otherwise empty value.
//   - Every struct the package declares is pinned or recorded in notWire,
//     and every a2a.proto message (and every a2a.json object type) is pinned
//     or recorded as not modeled. A new type cannot skip the check by not
//     being listed.
//
// Adding a specOmission is a design decision, not a way to silence the test.

// specOmission records a spec field a pinned Go type does not carry.
type specOmission struct {
	field  string
	reason string
}

// nonSpecField records a field a pinned Go type writes that the spec type
// does not define.
type nonSpecField struct {
	field  string
	reason string
}

// specPin ties a Go wire type to the spec type(s) it encodes in one version.
// More than one ref means the Go type is a flattened union (Part stands for
// every 0.3 part kind) and is graded against the union of their fields. A Go
// type a version shares with another is pinned once per version.
type specPin struct {
	value     any
	version   ProtocolVersion
	refs      []string // a2a.proto message names (1.0) or a2a.json definitions (0.3)
	omissions []specOmission
	nonSpec   []nonSpecField
}

const (
	reasonNoPushConfigCRUD = "push notification configs are not stored: a PromptKit server answers the " +
		"push config methods with PushNotificationNotSupportedError, and the client does not call them"
	reasonNoRequestMetadata = "callers have no way to attach request metadata to this call; the server " +
		"does not read it"
	reasonTenantV1Only = "1.0's routing field, which 0.3 does not have; the client adds it only to " +
		"1.0 requests (from the selected interface), so a 0.3 request never carries it"
	reasonV03NoSignatures = "a signature covers the card its signer published; the 0.3 rendering of a card " +
		"(0.3 field names, supportedInterfaces added) is a different document it cannot verify"
	reasonV1OnlyFlowFields = "1.0's device code flow and PKCE flag; a 0.3 rendering passes the 1.0 flows " +
		"through, and 0.3 clients ignore fields they do not know"
)

func a2aSpecPins() []specPin {
	const v1, v03 = ProtocolVersion10, ProtocolVersion03
	return []specPin{
		// --- 1.0: the Go protocol model, which marshals as 1.0 ---
		{value: Part{}, version: v1, refs: []string{"Part"}},
		{value: Message{}, version: v1, refs: []string{"Message"}},
		{value: Artifact{}, version: v1, refs: []string{"Artifact"}},
		{value: TaskStatus{}, version: v1, refs: []string{"TaskStatus"}},
		{value: Task{}, version: v1, refs: []string{"Task"}},
		{value: AgentCard{}, version: v1, refs: []string{"AgentCard"}},
		{value: AgentCardSignature{}, version: v1, refs: []string{"AgentCardSignature"}},
		{value: AgentSkill{}, version: v1, refs: []string{"AgentSkill"}},
		{value: AgentProvider{}, version: v1, refs: []string{"AgentProvider"}},
		{value: AgentCapabilities{}, version: v1, refs: []string{"AgentCapabilities"}},
		{value: AgentInterface{}, version: v1, refs: []string{"AgentInterface"}},
		{value: AgentExtension{}, version: v1, refs: []string{"AgentExtension"}},
		{value: SendMessageRequest{}, version: v1, refs: []string{"SendMessageRequest"}},
		{value: SendMessageConfiguration{}, version: v1, refs: []string{"SendMessageConfiguration"}},
		{value: TaskPushNotificationConfig{}, version: v1, refs: []string{"TaskPushNotificationConfig"}},
		{value: AuthenticationInfo{}, version: v1, refs: []string{"AuthenticationInfo"}},
		{value: GetTaskRequest{}, version: v1, refs: []string{"GetTaskRequest"}},
		{
			value: CancelTaskRequest{}, version: v1, refs: []string{"CancelTaskRequest"},
			omissions: []specOmission{{"metadata", reasonNoRequestMetadata}},
		},
		{value: ListTasksRequest{}, version: v1, refs: []string{"ListTasksRequest"}},
		{value: ListTasksResponse{}, version: v1, refs: []string{"ListTasksResponse"}},
		{value: SubscribeTaskRequest{}, version: v1, refs: []string{"SubscribeToTaskRequest"}},
		{value: SendMessageResponse{}, version: v1, refs: []string{"SendMessageResponse"}},
		{value: StreamResponse{}, version: v1, refs: []string{"StreamResponse"}},
		{value: TaskStatusUpdateEvent{}, version: v1, refs: []string{"TaskStatusUpdateEvent"}},
		{value: TaskArtifactUpdateEvent{}, version: v1, refs: []string{"TaskArtifactUpdateEvent"}},
		{value: SecurityScheme{}, version: v1, refs: []string{"SecurityScheme"}},
		{value: APIKeySecurityScheme{}, version: v1, refs: []string{"APIKeySecurityScheme"}},
		{value: HTTPAuthSecurityScheme{}, version: v1, refs: []string{"HTTPAuthSecurityScheme"}},
		{value: OAuth2SecurityScheme{}, version: v1, refs: []string{"OAuth2SecurityScheme"}},
		{value: OpenIDConnectSecurityScheme{}, version: v1, refs: []string{"OpenIdConnectSecurityScheme"}},
		{value: MutualTLSSecurityScheme{}, version: v1, refs: []string{"MutualTlsSecurityScheme"}},
		{value: OAuthFlows{}, version: v1, refs: []string{"OAuthFlows"}},
		{
			value: OAuthFlow{}, version: v1,
			refs: []string{
				"AuthorizationCodeOAuthFlow", "ClientCredentialsOAuthFlow", "ImplicitOAuthFlow",
				"PasswordOAuthFlow", "DeviceCodeOAuthFlow",
			},
		},
		{value: SecurityRequirement{}, version: v1, refs: []string{"SecurityRequirement"}},
		{value: stringList{}, version: v1, refs: []string{"StringList"}},

		// --- 0.3: the JSON-RPC envelope (a2a.proto has none; 1.0 §9 uses JSON-RPC 2.0 as 0.3 does) ---
		{value: JSONRPCRequest{}, version: v03, refs: []string{"JSONRPCRequest"}},
		{value: JSONRPCResponse{}, version: v03, refs: []string{"JSONRPCSuccessResponse", "JSONRPCErrorResponse"}},
		{value: JSONRPCError{}, version: v03, refs: []string{"JSONRPCError"}},

		// --- 0.3: the request params a client sends a 0.3 server as they are ---
		{
			value: GetTaskRequest{}, version: v03, refs: []string{"TaskQueryParams"},
			omissions: []specOmission{{"metadata", reasonNoRequestMetadata}},
			nonSpec:   []nonSpecField{{"tenant", reasonTenantV1Only}},
		},
		{
			value: CancelTaskRequest{}, version: v03, refs: []string{"TaskIdParams"},
			omissions: []specOmission{{"metadata", reasonNoRequestMetadata}},
			nonSpec:   []nonSpecField{{"tenant", reasonTenantV1Only}},
		},
		{
			value: SubscribeTaskRequest{}, version: v03, refs: []string{"TaskIdParams"},
			omissions: []specOmission{{"metadata", reasonNoRequestMetadata}},
			nonSpec:   []nonSpecField{{"tenant", reasonTenantV1Only}},
		},

		// --- 0.3: the version-specific shapes in wire.go and security.go ---
		{value: v03Part{}, version: v03, refs: []string{"TextPart", "FilePart", "DataPart"}},
		{value: v03File{}, version: v03, refs: []string{"FileWithBytes", "FileWithUri"}},
		{value: v03Message{}, version: v03, refs: []string{"Message"}},
		{value: v03TaskStatus{}, version: v03, refs: []string{"TaskStatus"}},
		{value: v03Artifact{}, version: v03, refs: []string{"Artifact"}},
		{value: v03Task{}, version: v03, refs: []string{"Task"}},
		{
			value: v03StatusUpdate{}, version: v03, refs: []string{"TaskStatusUpdateEvent"},
		},
		{value: v03ArtifactUpdate{}, version: v03, refs: []string{"TaskArtifactUpdateEvent"}},
		{value: v03SendParams{}, version: v03, refs: []string{"MessageSendParams"}},
		{value: v03SendConfig{}, version: v03, refs: []string{"MessageSendConfiguration"}},
		{value: v03PushConfig{}, version: v03, refs: []string{"PushNotificationConfig"}},
		{value: v03Auth{}, version: v03, refs: []string{"PushNotificationAuthenticationInfo"}},
		{value: v03Interface{}, version: v03, refs: []string{"AgentInterface"}},
		{
			value: v03Capabilities{}, version: v03, refs: []string{"AgentCapabilities"},
			omissions: []specOmission{{"stateTransitionHistory", "PromptKit keeps no state transition " +
				"history to offer; A2A 1.0 removed the capability"}},
		},
		{value: v03Skill{}, version: v03, refs: []string{"AgentSkill"}},
		{
			value: v03Card{}, version: v03, refs: []string{"AgentCard"},
			omissions: []specOmission{{"signatures", reasonV03NoSignatures}},
			nonSpec: []nonSpecField{{"supportedInterfaces", "1.0's interface list, kept on the 0.3 card so " +
				"a 1.0 client reading the same document still finds its interface"}},
		},
		{
			value: v03SecurityScheme{}, version: v03,
			refs: []string{
				"APIKeySecurityScheme", "HTTPAuthSecurityScheme", "OAuth2SecurityScheme",
				"OpenIdConnectSecurityScheme", "MutualTLSSecurityScheme",
			},
		},
		{value: AgentProvider{}, version: v03, refs: []string{"AgentProvider"}},
		{value: AgentExtension{}, version: v03, refs: []string{"AgentExtension"}},
		{value: AgentCardSignature{}, version: v03, refs: []string{"AgentCardSignature"}},
		{
			value: OAuthFlows{}, version: v03, refs: []string{"OAuthFlows"},
			nonSpec: []nonSpecField{{"deviceCode", reasonV1OnlyFlowFields}},
		},
		{
			value: OAuthFlow{}, version: v03,
			refs: []string{
				"AuthorizationCodeOAuthFlow", "ClientCredentialsOAuthFlow", "ImplicitOAuthFlow", "PasswordOAuthFlow",
			},
			nonSpec: []nonSpecField{
				{"deviceAuthorizationUrl", reasonV1OnlyFlowFields},
				{"pkceRequired", reasonV1OnlyFlowFields},
			},
		},
	}
}

// notWire records structs in this package that are not A2A wire types, and
// why. A struct that is neither pinned nor listed here fails
// TestA2ASpecCoverage.
var notWire = map[string]string{
	"ToolBridge":      "builds tool descriptors from a card; never serialized",
	"RPCError":        "the Go error a JSON-RPC error response becomes; the wire shape is JSONRPCError",
	"HTTPStatusError": "the Go error for a non-2xx HTTP response; not an A2A message",
	"StreamEvent":     "one decoded stream result handed to the caller; the wire shapes are StreamResponse and the 0.3 kinds",
	"unknownState":    "the Go error for a task in a state no version defines",
	"Client":          "the JSON-RPC client",
	"readCloser":      "stream plumbing",
	"scanLine":        "SSE line scanner state",
	"idleTimer":       "stream idle timeout",
	"clientEntry":     "executor's per-agent client cache entry",
	"RetryPolicy":     "executor configuration",
	"Executor":        "tool executor that calls agents",
	"a2aInput":        "the arguments of an A2A skill tool, which the model writes; not an A2A message",
	"ServerRequest":   "a checked JSON-RPC request handed to a server; the wire shape is JSONRPCRequest",
	"errorInfo": "google.rpc.ErrorInfo, which googleapis defines and a2a.proto only refers to (1.0 §9.5); " +
		"its shape is pinned by the server error tests",
}

// v1NotModeled records a2a.proto messages no Go type encodes, and why.
var v1NotModeled = map[string]string{
	"GetTaskPushNotificationConfigRequest":    reasonNoPushConfigCRUD,
	"DeleteTaskPushNotificationConfigRequest": reasonNoPushConfigCRUD,
	"ListTaskPushNotificationConfigsRequest":  reasonNoPushConfigCRUD,
	"ListTaskPushNotificationConfigsResponse": reasonNoPushConfigCRUD,
	"GetExtendedAgentCardRequest": "its only field is tenant, which the client adds to every 1.0 request " +
		"from the selected interface; the client sends {} otherwise",
}

// v03NotModeled records a2a.json object types no Go type encodes, and why.
// JSON-RPC request and response wrappers and the error variants are not
// listed: their params, results and error object are pinned on their own.
var v03NotModeled = map[string]string{
	"GetTaskPushNotificationConfigParams":    reasonNoPushConfigCRUD,
	"DeleteTaskPushNotificationConfigParams": reasonNoPushConfigCRUD,
	"ListTaskPushNotificationConfigParams":   reasonNoPushConfigCRUD,
	"TaskPushNotificationConfig":             reasonNoPushConfigCRUD,
	"JSONRPCMessage":                         "the base of JSONRPCRequest and the responses, each pinned",
	"PartBase":                               "the base of the part kinds; its field is on v03Part",
	"FileBase":                               "the base of the file variants; its fields are on v03File",
	"SecuritySchemeBase":                     "the base of the scheme types; its field is on v03SecurityScheme",
}

// --- loading the mirrors ---

// spokenVersions are the versions the package speaks, each with a mirror.
var spokenVersions = []ProtocolVersion{ProtocolVersion10, ProtocolVersion03}

func TestA2ASpecMirrorIsTheSpokenVersions(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("testdata", "spec"))
	require.NoError(t, err)
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	var want []string
	for _, v := range spokenVersions {
		want = append(want, string(v))
	}
	sort.Strings(want)
	require.Equal(t, want, dirs,
		"testdata/spec must mirror exactly the versions the package speaks; run `make a2a-spec`")
}

// protoField is one field of an a2a.proto message.
type protoField struct {
	jsonName string
	typ      string // scalar, message or enum name; for a map, the value type
	repeated bool
	isMap    bool
	required bool
	oneof    string
}

// protoSpec is what the parity checks need of a2a.proto.
type protoSpec struct {
	messages map[string][]protoField
	enums    map[string][]string
}

var (
	protoBlockRE  = regexp.MustCompile(`^(message|enum|oneof|service)\s+(\w+)\s*\{`)
	protoFieldRE  = regexp.MustCompile(`^(optional\s+|repeated\s+)?(map<\s*\w+\s*,\s*([\w.]+)\s*>|[\w.]+)\s+(\w+)\s*=\s*\d+\s*(\[[^\]]*\])?\s*;`)
	protoEnumRE   = regexp.MustCompile(`^(\w+)\s*=\s*\d+\s*(\[[^\]]*\])?\s*;`)
	protoStringRE = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
)

// protoJSONName is protoc's json_name for a field: lowerCamelCase.
func protoJSONName(name string) string {
	var b strings.Builder
	upper := false
	for _, r := range name {
		if r == '_' {
			upper = true
			continue
		}
		if upper && r >= 'a' && r <= 'z' {
			r -= 'a' - 'A'
		}
		upper = false
		b.WriteRune(r)
	}
	return b.String()
}

// parseProto reads the messages and enums of a2a.proto. It handles what the
// file uses (top-level messages and enums, oneofs, maps, field options) and
// fails on anything else rather than misread it.
func parseProto(t *testing.T, src string) *protoSpec {
	t.Helper()
	spec := &protoSpec{messages: map[string][]protoField{}, enums: map[string][]string{}}
	type frame struct{ kind, name string }
	var stack []frame
	for n, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "//") {
			continue
		}
		line = protoStringRE.ReplaceAllString(line, `""`)
		if i := strings.Index(line, "//"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if m := protoBlockRE.FindStringSubmatch(line); m != nil {
			if m[1] == "message" || m[1] == "enum" {
				require.Emptyf(t, stack, "a2a.proto:%d: nested %s %s is not supported by this parser", n+1, m[1], m[2])
			}
			if m[1] == "message" {
				spec.messages[m[2]] = nil
			}
			if m[1] == "enum" {
				spec.enums[m[2]] = nil
			}
			stack = append(stack, frame{m[1], m[2]})
			line = line[strings.Index(line, "{")+1:]
		} else if len(stack) > 0 {
			switch top := stack[len(stack)-1]; top.kind {
			case "message", "oneof":
				if m := protoFieldRE.FindStringSubmatch(line); m != nil {
					msg, oneof := top.name, ""
					if top.kind == "oneof" {
						msg, oneof = stack[len(stack)-2].name, top.name
					}
					f := protoField{
						jsonName: protoJSONName(m[4]),
						typ:      m[2],
						repeated: strings.HasPrefix(m[1], "repeated"),
						required: strings.Contains(m[5], "field_behavior) = REQUIRED"),
						oneof:    oneof,
					}
					if m[3] != "" {
						f.isMap, f.typ = true, m[3]
					}
					spec.messages[msg] = append(spec.messages[msg], f)
					continue
				}
				if top.kind == "message" && line != "" && line != "}" && !strings.HasPrefix(line, "reserved") {
					t.Fatalf("a2a.proto:%d: unrecognized line in message %s: %q", n+1, top.name, line)
				}
			case "enum":
				if m := protoEnumRE.FindStringSubmatch(line); m != nil {
					spec.enums[top.name] = append(spec.enums[top.name], m[1])
					continue
				}
			}
		}
		for _, c := range line {
			switch c {
			case '{':
				stack = append(stack, frame{"other", ""})
			case '}':
				require.NotEmptyf(t, stack, "a2a.proto:%d: unbalanced }", n+1)
				stack = stack[:len(stack)-1]
			}
		}
	}
	require.Empty(t, stack, "a2a.proto: unbalanced braces")
	return spec
}

func loadV1Spec(t *testing.T) *protoSpec {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "spec", string(ProtocolVersion10), "a2a.proto"))
	require.NoError(t, err, "no mirrored a2a.proto; run `make a2a-spec`")
	spec := parseProto(t, string(raw))
	require.NotEmpty(t, spec.messages["AgentCard"], "parsed a2a.proto has no AgentCard fields")
	return spec
}

func loadV03Spec(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "spec", string(ProtocolVersion03), "a2a.json"))
	require.NoError(t, err, "no mirrored a2a.json; run `make a2a-spec`")
	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))
	return doc
}

func v03Definitions(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	defs, ok := doc["definitions"].(map[string]any)
	require.True(t, ok, "a2a.json has no definitions")
	return defs
}

// specShape is a pin's spec fields.
type specShape struct {
	fields   map[string]bool
	required map[string]bool // only for single-type pins
}

func pinShape(t *testing.T, v1 *protoSpec, v03 map[string]any, pin specPin) specShape {
	t.Helper()
	shape := specShape{fields: map[string]bool{}, required: map[string]bool{}}
	for _, ref := range pin.refs {
		switch pin.version {
		case ProtocolVersion10:
			fields, ok := v1.messages[ref]
			require.Truef(t, ok, "a2a.proto has no message %s — has the spec renamed it?", ref)
			for _, f := range fields {
				shape.fields[f.jsonName] = true
				if f.required && len(pin.refs) == 1 {
					shape.required[f.jsonName] = true
				}
			}
		case ProtocolVersion03:
			def, ok := v03Definitions(t, v03)[ref].(map[string]any)
			require.Truef(t, ok, "a2a.json has no definition %s — has the spec renamed it?", ref)
			props, _ := def["properties"].(map[string]any)
			for name := range props {
				shape.fields[name] = true
			}
			if req, ok := def["required"].([]any); ok && len(pin.refs) == 1 {
				for _, r := range req {
					shape.required[r.(string)] = true
				}
			}
		}
	}
	return shape
}

// --- what an encoder writes ---

// enumSample is a valid value for the package's string enums, whose encoders
// refuse an arbitrary string.
func enumSample(typ reflect.Type) (reflect.Value, bool) {
	switch typ {
	case reflect.TypeOf(TaskState("")):
		return reflect.ValueOf(TaskStateWorking), true
	case reflect.TypeOf(Role("")):
		return reflect.ValueOf(RoleUser), true
	}
	return reflect.Value{}, false
}

var (
	timeType       = reflect.TypeOf(time.Time{})
	rawMessageType = reflect.TypeOf(json.RawMessage{})
)

// populated returns a value of typ with every field set, recursively, so
// that marshaling it writes every field its encoder can write.
func populated(typ reflect.Type, depth int) reflect.Value {
	v := reflect.New(typ).Elem()
	if depth > 6 {
		return v
	}
	if e, ok := enumSample(typ); ok {
		return e
	}
	switch typ {
	case timeType:
		return reflect.ValueOf(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	case rawMessageType:
		return reflect.ValueOf(json.RawMessage(`{}`))
	}
	switch typ.Kind() {
	case reflect.String:
		v.SetString("x")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	case reflect.Float64:
		v.SetFloat(1)
	case reflect.Interface:
		v.Set(reflect.ValueOf("x"))
	case reflect.Pointer:
		p := reflect.New(typ.Elem())
		p.Elem().Set(populated(typ.Elem(), depth+1))
		v.Set(p)
	case reflect.Slice:
		if typ.Elem().Kind() == reflect.Uint8 {
			v.SetBytes([]byte("x"))
			break
		}
		v.Set(reflect.Append(v, populated(typ.Elem(), depth+1)))
	case reflect.Map:
		m := reflect.MakeMap(typ)
		m.SetMapIndex(reflect.ValueOf("k").Convert(typ.Key()), populated(typ.Elem(), depth+1))
		v.Set(m)
	case reflect.Struct:
		for i := range typ.NumField() {
			if typ.Field(i).IsExported() {
				v.Field(i).Set(populated(typ.Field(i).Type, depth+1))
			}
		}
	}
	return v
}

// minimal returns the zero value of typ with only the enum fields set (an
// encoder refuses an empty enum), recursively through embedded and value
// struct fields.
func minimal(typ reflect.Type) reflect.Value {
	v := reflect.New(typ).Elem()
	if e, ok := enumSample(typ); ok {
		return e
	}
	if typ.Kind() != reflect.Struct || typ == timeType {
		return v
	}
	for i := range typ.NumField() {
		f := typ.Field(i)
		if !f.IsExported() {
			continue
		}
		if _, ok := enumSample(f.Type); ok || f.Type.Kind() == reflect.Struct {
			v.Field(i).Set(minimal(f.Type))
		}
	}
	return v
}

// writtenFields returns the top-level JSON members marshaling v writes.
func writtenFields(t *testing.T, v reflect.Value) map[string]bool {
	t.Helper()
	out, err := json.Marshal(v.Interface())
	require.NoError(t, err)
	var obj map[string]json.RawMessage
	require.NoErrorf(t, json.Unmarshal(out, &obj), "%s does not encode as a JSON object: %s", v.Type(), out)
	fields := map[string]bool{}
	for k := range obj {
		fields[k] = true
	}
	return fields
}

func pinName(pin specPin) string {
	return reflect.TypeOf(pin.value).Name() + "@" + string(pin.version)
}

func TestA2ASpecParity(t *testing.T) {
	v1, v03 := loadV1Spec(t), loadV03Spec(t)

	seen := map[string]bool{}
	for _, pin := range a2aSpecPins() {
		name := pinName(pin)
		require.Falsef(t, seen[name], "%s is pinned twice", name)
		seen[name] = true
		t.Run(name, func(t *testing.T) {
			typ := reflect.TypeOf(pin.value)
			shape := pinShape(t, v1, v03, pin)
			written := writtenFields(t, populated(typ, 0))

			omitted := map[string]bool{}
			for _, o := range pin.omissions {
				require.NotEmptyf(t, o.reason, "omission %q needs a reason", o.field)
				require.Truef(t, shape.fields[o.field],
					"omission %q is not a field of %v; remove the stale entry", o.field, pin.refs)
				require.Falsef(t, written[o.field], "%s now writes %q; remove the stale omission", name, o.field)
				omitted[o.field] = true
			}
			extra := map[string]bool{}
			for _, n := range pin.nonSpec {
				require.NotEmptyf(t, n.reason, "non-spec field %q needs a reason", n.field)
				require.Falsef(t, shape.fields[n.field],
					"%q is a field of %v now; remove the stale nonSpec entry", n.field, pin.refs)
				require.Truef(t, written[n.field], "%s no longer writes %q; remove the stale nonSpec entry",
					name, n.field)
				extra[n.field] = true
			}

			for _, f := range sortedKeys(shape.fields) {
				if !written[f] && !omitted[f] {
					t.Errorf("%s does not write spec field %q of %v (A2A %s): carry it, or record a "+
						"specOmission saying why not", name, f, pin.refs, pin.version)
				}
			}
			for _, f := range sortedKeys(written) {
				if !shape.fields[f] && !extra[f] {
					t.Errorf("%s writes %q, which %v (A2A %s) does not define: remove it, or record a "+
						"nonSpecField saying why", name, f, pin.refs, pin.version)
				}
			}
			empty := writtenFields(t, minimal(typ))
			for _, f := range sortedKeys(shape.required) {
				if !empty[f] && !omitted[f] {
					t.Errorf("%s leaves out %q, which %v (A2A %s) requires, when it is empty", name, f,
						pin.refs, pin.version)
				}
			}
		})
	}
}

// TestA2ASpecCoverage requires every struct the package declares, and every
// spec type, to be pinned or recorded as not needing to be, so a new type on
// either side cannot go unchecked.
func TestA2ASpecCoverage(t *testing.T) {
	v1, v03 := loadV1Spec(t), loadV03Spec(t)
	pinnedGo := map[string]bool{}
	pinnedSpec := map[ProtocolVersion]map[string]bool{ProtocolVersion10: {}, ProtocolVersion03: {}}
	for _, pin := range a2aSpecPins() {
		pinnedGo[reflect.TypeOf(pin.value).Name()] = true
		for _, ref := range pin.refs {
			pinnedSpec[pin.version][ref] = true
		}
	}

	declared := declaredStructs(t)
	for _, name := range declared {
		if !pinnedGo[name] && notWire[name] == "" {
			t.Errorf("struct %s is neither pinned in a2aSpecPins nor recorded in notWire", name)
		}
		if pinnedGo[name] && notWire[name] != "" {
			t.Errorf("struct %s is pinned and also recorded in notWire; remove the notWire entry", name)
		}
	}
	for _, name := range sortedKeys(notWire) {
		if !slices.Contains(declared, name) {
			t.Errorf("notWire lists %s, which this package no longer declares; remove it", name)
		}
	}

	for _, msg := range sortedKeys(v1.messages) {
		if !pinnedSpec[ProtocolVersion10][msg] && v1NotModeled[msg] == "" {
			t.Errorf("a2a.proto message %s is neither pinned nor recorded in v1NotModeled", msg)
		}
	}
	for _, msg := range sortedKeys(v1NotModeled) {
		if _, ok := v1.messages[msg]; !ok || pinnedSpec[ProtocolVersion10][msg] {
			t.Errorf("v1NotModeled lists %s, which is pinned or not an a2a.proto message; remove it", msg)
		}
	}

	defs := v03Definitions(t, v03)
	for _, name := range sortedKeys(defs) {
		def, _ := defs[name].(map[string]any)
		if _, isObject := def["properties"]; !isObject || isV03Envelope(name, def) {
			continue
		}
		if !pinnedSpec[ProtocolVersion03][name] && v03NotModeled[name] == "" {
			t.Errorf("a2a.json definition %s is neither pinned nor recorded in v03NotModeled", name)
		}
	}
	for _, name := range sortedKeys(v03NotModeled) {
		if _, ok := defs[name]; !ok || pinnedSpec[ProtocolVersion03][name] {
			t.Errorf("v03NotModeled lists %s, which is pinned or not an a2a.json definition; remove it", name)
		}
	}
}

// isV03Envelope reports whether an a2a.json definition is a JSON-RPC request
// or response wrapper (method, params and result are pinned on their own) or
// one of the error variants (JSONRPCError with a fixed code).
func isV03Envelope(name string, def map[string]any) bool {
	props, _ := def["properties"].(map[string]any)
	_, hasRPC := props["jsonrpc"]
	_, hasCode := props["code"]
	return (hasRPC && name != "JSONRPCRequest" && name != "JSONRPCSuccessResponse" &&
		name != "JSONRPCErrorResponse") || (hasCode && name != "JSONRPCError")
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

// --- enums ---

func TestA2ASpecEnums(t *testing.T) {
	v1, v03 := loadV1Spec(t), loadV03Spec(t)
	defs := v03Definitions(t, v03)

	var v1States, v03States []string
	for s := range taskStateWire {
		v1States = append(v1States, s.V1Name())
		v03States = append(v03States, s.V03Name())
	}
	require.ElementsMatch(t, v1.enums["TaskState"], v1States, "TaskState V1Name vs a2a.proto TaskState")
	require.ElementsMatch(t, v03Enum(t, defs["TaskState"]), v03States, "TaskState V03Name vs a2a.json TaskState")

	require.ElementsMatch(t, v1.enums["Role"], []string{roleUnspecifiedV1, RoleUser.V1Name(), RoleAgent.V1Name()})
	msg := defs["Message"].(map[string]any)["properties"].(map[string]any)
	require.ElementsMatch(t, v03Enum(t, msg["role"]), []string{string(RoleUser), string(RoleAgent)})

	require.Contains(t, v03Enum(t, defs["TransportProtocol"]), ProtocolBindingJSONRPC)
	require.Len(t, v1.enums, 2, "a2a.proto has an enum this test does not check")
}

func v03Enum(t *testing.T, node any) []string {
	t.Helper()
	m, ok := node.(map[string]any)
	require.True(t, ok)
	list, ok := m["enum"].([]any)
	require.True(t, ok, "not an enum: %v", node)
	out := make([]string, len(list))
	for i, v := range list {
		out[i] = v.(string)
	}
	return out
}
