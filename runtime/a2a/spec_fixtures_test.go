package a2a

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xeipuuv/gojsonschema"
)

// Wire fixtures (#2135): realistic 1.0 and 0.3 documents decode and encode
// back to the same JSON, and what the encoders write is checked against the
// mirrored spec: 1.0 output field by field against a2a.proto, 0.3 output
// against a2a.json with a JSON-schema validator. spec_parity_test.go checks
// the types; these check the documents.

// --- 1.0: ProtoJSON against a2a.proto ---

// protoViolations walks a JSON value as the ProtoJSON form of message msg and
// returns every way it is not one: a member that is not a field, a REQUIRED
// field that is missing, two members of one oneof, a value of the wrong JSON
// type, an enum name the enum does not have, bytes that are not base64 or a
// timestamp that is not RFC 3339 in UTC.
func (s *protoSpec) protoViolations(msg string, v any) []string {
	var out []string
	s.walkMessage(msg, v, msg, &out)
	return out
}

func (s *protoSpec) walkMessage(msg string, v any, path string, out *[]string) {
	obj, ok := v.(map[string]any)
	if !ok {
		*out = append(*out, fmt.Sprintf("%s: a %s is a JSON object, got %T", path, msg, v))
		return
	}
	fields := map[string]protoField{}
	for _, f := range s.messages[msg] {
		fields[f.jsonName] = f
	}
	oneofs := map[string]string{}
	for _, key := range sortedKeys(obj) {
		f, ok := fields[key]
		if !ok {
			*out = append(*out, fmt.Sprintf("%s: %q is not a field of %s", path, key, msg))
			continue
		}
		if f.oneof != "" {
			if other, dup := oneofs[f.oneof]; dup {
				*out = append(*out, fmt.Sprintf("%s: %q and %q are both set, of oneof %s", path, other, key, f.oneof))
			}
			oneofs[f.oneof] = key
		}
		s.walkField(f, obj[key], path+"."+key, out)
	}
	for _, f := range s.messages[msg] {
		if _, ok := obj[f.jsonName]; f.required && !ok {
			*out = append(*out, fmt.Sprintf("%s: REQUIRED field %q is missing", path, f.jsonName))
		}
	}
}

func (s *protoSpec) walkField(f protoField, v any, path string, out *[]string) {
	if v == nil { // ProtoJSON accepts null for every field, as its default
		return
	}
	switch {
	case f.isMap:
		m, ok := v.(map[string]any)
		if !ok {
			*out = append(*out, fmt.Sprintf("%s: a map is a JSON object, got %T", path, v))
			return
		}
		for _, k := range sortedKeys(m) {
			s.walkValue(f.typ, m[k], path+"."+k, out)
		}
	case f.repeated:
		list, ok := v.([]any)
		if !ok {
			*out = append(*out, fmt.Sprintf("%s: a repeated field is a JSON array, got %T", path, v))
			return
		}
		for i, e := range list {
			s.walkValue(f.typ, e, fmt.Sprintf("%s[%d]", path, i), out)
		}
	default:
		s.walkValue(f.typ, v, path, out)
	}
}

func (s *protoSpec) walkValue(typ string, v any, path string, out *[]string) {
	bad := func(want string) {
		*out = append(*out, fmt.Sprintf("%s: a %s is %s, got %T %v", path, typ, want, v, v))
	}
	if _, ok := s.messages[typ]; ok {
		s.walkMessage(typ, v, path, out)
		return
	}
	if names, ok := s.enums[typ]; ok {
		if str, isStr := v.(string); !isStr || !contains(names, str) {
			bad("one of " + strings.Join(names, ", "))
		}
		return
	}
	switch typ {
	case "string":
		if _, ok := v.(string); !ok {
			bad("a JSON string")
		}
	case "bool":
		if _, ok := v.(bool); !ok {
			bad("a JSON boolean")
		}
	case "int32", "int64":
		if _, ok := v.(float64); !ok {
			bad("a JSON number")
		}
	case "bytes":
		str, ok := v.(string)
		if _, err := base64.StdEncoding.DecodeString(str); !ok || err != nil {
			bad("base64")
		}
	case "google.protobuf.Timestamp":
		str, ok := v.(string)
		if _, err := time.Parse(time.RFC3339Nano, str); !ok || err != nil || !strings.HasSuffix(str, "Z") {
			bad("RFC 3339 in UTC (Z)")
		}
	case "google.protobuf.Struct":
		if _, ok := v.(map[string]any); !ok {
			bad("a JSON object")
		}
	case "google.protobuf.Value":
	default:
		*out = append(*out, fmt.Sprintf("%s: type %s is not handled by this walker", path, typ))
	}
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

// wireFixture is one fixture document: what it decodes into and the encoder
// that writes it back.
type wireFixture struct {
	file string
	// v1Message is the a2a.proto message the document is (1.0), or the
	// a2a.json definition (0.3); "" when the spec defines none (the JSON-RPC
	// envelope in 1.0).
	specType string
	// roundTrip decodes the document and returns what the encoder writes.
	roundTrip func(t *testing.T, raw []byte) any
}

// decodeInto returns a roundTrip that decodes into T and encodes with enc.
func decodeInto[T any](enc func(*T) any) func(*testing.T, []byte) any {
	return func(t *testing.T, raw []byte) any {
		t.Helper()
		var v T
		require.NoError(t, json.Unmarshal(raw, &v))
		return enc(&v)
	}
}

func asIs[T any](v *T) any { return v }

func streamEvent(v ProtocolVersion) func(*testing.T, []byte) any {
	return func(t *testing.T, raw []byte) any {
		t.Helper()
		event, ok := parseStreamEvent(string(raw))
		require.True(t, ok, "not a stream event")
		var payload any
		switch {
		case event.Task != nil:
			payload = event.Task
		case event.Message != nil:
			payload = event.Message
		case event.StatusUpdate != nil:
			payload = event.StatusUpdate
		case event.ArtifactUpdate != nil:
			payload = event.ArtifactUpdate
		}
		wire, err := v.WireStreamEvent(payload)
		require.NoError(t, err)
		return wire
	}
}

var v1Fixtures = []wireFixture{
	{"1.0/task.json", "Task", decodeInto(asIs[Task])},
	{"1.0/send_message_request.json", "SendMessageRequest", decodeInto(asIs[SendMessageRequest])},
	{"1.0/send_message_response.json", "SendMessageResponse", decodeInto(asIs[SendMessageResponse])},
	{"1.0/stream_status_update.json", "StreamResponse", streamEvent(ProtocolVersion10)},
	{"1.0/stream_artifact_update.json", "StreamResponse", streamEvent(ProtocolVersion10)},
	{"1.0/list_tasks_request.json", "ListTasksRequest", decodeInto(asIs[ListTasksRequest])},
	{"1.0/list_tasks_response.json", "ListTasksResponse", decodeInto(asIs[ListTasksResponse])},
	{"1.0/agent_card_security.json", "AgentCard", decodeInto(ProtocolVersion10.WireAgentCard)},
	{"../agent_card_v1.json", "AgentCard", decodeInto(ProtocolVersion10.WireAgentCard)},
	{"1.0/error_response.json", "", decodeInto(asIs[JSONRPCResponse])},
}

var v03Fixtures = []wireFixture{
	{"0.3/task.json", "Task", decodeInto(ProtocolVersion03.WireTask)},
	{"0.3/message.json", "Message", streamEvent(ProtocolVersion03)},
	{"0.3/status_update.json", "TaskStatusUpdateEvent", streamEvent(ProtocolVersion03)},
	{"0.3/artifact_update.json", "TaskArtifactUpdateEvent", streamEvent(ProtocolVersion03)},
	{"0.3/send_params.json", "MessageSendParams", decodeInto(ProtocolVersion03.WireSendParams)},
	{"0.3/agent_card.json", "AgentCard", decodeInto(ProtocolVersion03.WireAgentCard)},
	{"0.3/error_response.json", "JSONRPCErrorResponse", decodeInto(asIs[JSONRPCResponse])},
}

func readWireFixture(t *testing.T, file string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "wire", file))
	require.NoError(t, err)
	return raw
}

func marshalToAny(t *testing.T, v any) any {
	t.Helper()
	out, err := json.Marshal(v)
	require.NoError(t, err)
	var back any
	require.NoError(t, json.Unmarshal(out, &back))
	return back
}

func TestA2AWireFixtures_V1RoundTripAndMatchTheProto(t *testing.T) {
	spec := loadV1Spec(t)
	for _, fx := range v1Fixtures {
		t.Run(fx.file, func(t *testing.T) {
			raw := readWireFixture(t, fx.file)
			var doc any
			require.NoError(t, json.Unmarshal(raw, &doc))
			if fx.specType != "" {
				require.Empty(t, spec.protoViolations(fx.specType, doc), "the fixture itself is not a valid %s",
					fx.specType)
			}

			got := marshalToAny(t, fx.roundTrip(t, raw))
			assert.Equal(t, doc, got, "decoding and encoding again changed the document")
			if fx.specType != "" {
				assert.Empty(t, spec.protoViolations(fx.specType, got))
			}
		})
	}
}

// v03Validator validates JSON values against one a2a.json definition.
func v03Validator(t *testing.T, doc map[string]any, def string) *gojsonschema.Schema {
	t.Helper()
	schema, err := gojsonschema.NewSchema(gojsonschema.NewGoLoader(map[string]any{
		"$schema":     doc["$schema"],
		"definitions": doc["definitions"],
		"$ref":        "#/definitions/" + def,
	}))
	require.NoError(t, err)
	return schema
}

func v03Violations(t *testing.T, schema *gojsonschema.Schema, v any) []string {
	t.Helper()
	res, err := schema.Validate(gojsonschema.NewGoLoader(v))
	require.NoError(t, err)
	var out []string
	for _, e := range res.Errors() {
		out = append(out, e.String())
	}
	return out
}

func TestA2AWireFixtures_V03RoundTripAndMatchTheSchema(t *testing.T) {
	doc := loadV03Spec(t)
	for _, fx := range v03Fixtures {
		t.Run(fx.file, func(t *testing.T) {
			schema := v03Validator(t, doc, fx.specType)
			raw := readWireFixture(t, fx.file)
			var want any
			require.NoError(t, json.Unmarshal(raw, &want))
			require.Empty(t, v03Violations(t, schema, want), "the fixture itself is not a valid %s", fx.specType)

			got := marshalToAny(t, fx.roundTrip(t, raw))
			assert.Empty(t, v03Violations(t, schema, got))
			if card, ok := got.(map[string]any); ok && fx.specType == "AgentCard" {
				// The 0.3 rendering keeps 1.0's interface list for 1.0 readers
				// (a nonSpec field of v03Card); the rest is the document.
				assert.Equal(t, []any{
					map[string]any{"url": "https://recipes.example.com/a2a", "protocolBinding": "JSONRPC",
						"protocolVersion": "0.3"},
					map[string]any{"url": "https://recipes.example.com/grpc", "protocolBinding": "GRPC",
						"protocolVersion": "0.3"},
				}, card["supportedInterfaces"])
				delete(card, "supportedInterfaces")
			}
			assert.Equal(t, want, got, "decoding and encoding again changed the document")
		})
	}
}

// What the 0.3 encoders write for sparse and edge-case values is valid 0.3:
// the required fields are there, and nothing 1.0 allows that 0.3 does not
// (a data part that is not an object, an empty file) slips through.
func TestA2AWire_V03EncodersWriteValidDocuments(t *testing.T) {
	doc := loadV03Spec(t)
	working := TaskStatus{State: TaskStateWorking}
	cases := []struct {
		name string
		def  string
		v    any
	}{
		{"empty card", "AgentCard", ProtocolVersion03.WireAgentCard(&AgentCard{})},
		{"sample card", "AgentCard", ProtocolVersion03.WireAgentCard(sampleCard())},
		{"bare task", "Task", ProtocolVersion03.WireTask(&Task{Status: working})},
		{"bare message", "Message", toV03Message(&Message{Role: RoleUser})},
		{"bare status update", "TaskStatusUpdateEvent", toV03StatusUpdate(&TaskStatusUpdateEvent{Status: working})},
		{"bare artifact update", "TaskArtifactUpdateEvent", toV03ArtifactUpdate(&TaskArtifactUpdateEvent{})},
		{"bare send params", "MessageSendParams", ProtocolVersion03.WireSendParams(&SendMessageRequest{
			Message: Message{Role: RoleUser}, Configuration: &SendMessageConfiguration{
				TaskPushNotificationConfig: &TaskPushNotificationConfig{Authentication: &AuthenticationInfo{}},
			},
		})},
		{"empty text part", "Part", toV03Part(&Part{})},
		{"array data part", "Part", toV03Part(&Part{DataValue: []any{"a", 1}})},
		{"string data part", "Part", toV03Part(&Part{DataValue: "plain"})},
		{"empty object data part", "Part", toV03Part(&Part{Data: map[string]any{}})},
		{"empty file part", "Part", toV03Part(&Part{Raw: []byte{}})},
		{"url file part", "Part", toV03Part(&Part{URL: strPtr("https://x.example/f")})},
		{"error", "JSONRPCErrorResponse", errorResponse(nil, ErrorFor(ProtocolVersion03, ErrCodeTaskNotFound,
			"Task not found", nil))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Empty(t, v03Violations(t, v03Validator(t, doc, tc.def), marshalToAny(t, tc.v)))
		})
	}
}

// What the 1.0 encoders write for sparse values carries every REQUIRED field.
func TestA2AWire_V1EncodersWriteValidDocuments(t *testing.T) {
	spec := loadV1Spec(t)
	working := TaskStatus{State: TaskStateWorking}
	cases := []struct {
		msg string
		v   any
	}{
		{"AgentCard", ProtocolVersion10.WireAgentCard(&AgentCard{Skills: []AgentSkill{{}}})},
		{"AgentCard", ProtocolVersion10.WireAgentCard(sampleCard())},
		{"Task", &Task{Status: working}},
		{"SendMessageResponse", ProtocolVersion10.WireSendResult(&Task{Status: working})},
		{"Message", &Message{Role: RoleAgent, Parts: []Part{{}}}},
		{"SendMessageRequest", &SendMessageRequest{Configuration: &SendMessageConfiguration{
			Blocking: true, TaskPushNotificationConfig: &TaskPushNotificationConfig{Authentication: &AuthenticationInfo{}},
		}}},
		{"ListTasksResponse", &ListTasksResponse{Tasks: []Task{{Status: working}}}},
	}
	for _, tc := range cases {
		t.Run(tc.msg, func(t *testing.T) {
			assert.Empty(t, spec.protoViolations(tc.msg, marshalToAny(t, tc.v)))
		})
	}
	for _, s := range []*StreamResponse{
		{StatusUpdate: &TaskStatusUpdateEvent{Status: working}},
		{ArtifactUpdate: &TaskArtifactUpdateEvent{}},
		{Message: &Message{Role: RoleAgent}},
	} {
		assert.Empty(t, spec.protoViolations("StreamResponse", marshalToAny(t, s)))
	}
}
