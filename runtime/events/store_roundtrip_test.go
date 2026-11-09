package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// eventDataTypeNames parses this package's non-test sources and returns every
// struct type that is EventData: one that embeds baseEventData, directly or
// through another such struct (the capability-call types embed
// CapabilityCallData).
func eventDataTypeNames(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	embeds := map[string][]string{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		require.NoError(t, err)
		ast.Inspect(f, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok || ts.Assign.IsValid() {
				return true
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				return true
			}
			embeds[ts.Name.Name] = nil
			for _, field := range st.Fields.List {
				if len(field.Names) == 0 {
					if id, ok := field.Type.(*ast.Ident); ok {
						embeds[ts.Name.Name] = append(embeds[ts.Name.Name], id.Name)
					}
				}
			}
			return true
		})
	}
	isData := map[string]bool{"baseEventData": true}
	for changed := true; changed; {
		changed = false
		for name, embedded := range embeds {
			for _, e := range embedded {
				if isData[e] && !isData[name] {
					isData[name] = true
					changed = true
				}
			}
		}
	}
	delete(isData, "baseEventData")
	names := make([]string, 0, len(isData))
	for name := range isData {
		names = append(names, name)
	}
	sort.Strings(names)
	require.NotEmpty(t, names)
	return names
}

// TestEventDataRegistry_CoversEveryEventDataType pins that every EventData
// struct can be read back from a FileEventStore. The SDK subscribes the store
// to every bus event (sdk.WithEventStore), and an unregistered DataType comes
// back from Query and Stream with nil Data.
func TestEventDataRegistry_CoversEveryEventDataType(t *testing.T) {
	var missing []string
	for _, name := range eventDataTypeNames(t) {
		if _, ok := eventDataRegistry["*events."+name]; !ok {
			missing = append(missing, name)
		}
	}
	assert.Empty(t, missing, "EventData types a FileEventStore cannot read back")
}

var sampleTime = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// fillSample sets every exported, serialized field of v to a non-zero value,
// so a round trip that drops or mangles any field shows up as a difference.
func fillSample(v reflect.Value, depth int) {
	if depth > 6 {
		return
	}
	switch v.Kind() {
	case reflect.Ptr:
		v.Set(reflect.New(v.Type().Elem()))
		fillSample(v.Elem(), depth+1)
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(time.Time{}) {
			v.Set(reflect.ValueOf(sampleTime))
			return
		}
		for i := 0; i < v.NumField(); i++ {
			f := v.Type().Field(i)
			if !f.IsExported() || f.Tag.Get("json") == "-" {
				continue
			}
			fillSample(v.Field(i), depth+1)
		}
	case reflect.String:
		v.SetString("s")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(7)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(7)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1.5)
	case reflect.Slice:
		if v.Type() == reflect.TypeOf(json.RawMessage{}) {
			v.Set(reflect.ValueOf(json.RawMessage(`{"k":"v"}`)))
			return
		}
		s := reflect.MakeSlice(v.Type(), 1, 1)
		fillSample(s.Index(0), depth+1)
		v.Set(s)
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		key := reflect.New(v.Type().Key()).Elem()
		fillSample(key, depth+1)
		val := reflect.New(v.Type().Elem()).Elem()
		fillSample(val, depth+1)
		m.SetMapIndex(key, val)
		v.Set(m)
	case reflect.Interface:
		if v.Type() == reflect.TypeOf((*error)(nil)).Elem() {
			v.Set(reflect.ValueOf(errors.New("boom")))
			return
		}
		if v.NumMethod() == 0 {
			v.Set(reflect.ValueOf("v"))
		}
	}
}

// TestFileEventStore_RoundTripsEveryEventDataType appends one fully populated
// event per registered data type and reads it back through Query. What a
// replay sees must be what was recorded.
func TestFileEventStore_RoundTripsEveryEventDataType(t *testing.T) {
	store, err := NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	canonical := map[string]EventData{}
	for _, factory := range eventDataRegistry {
		data := factory()
		canonical[fmt.Sprintf("%T", data)] = data
	}
	// Every EventData type is covered: the registry's canonical names are
	// exactly the package's EventData structs.
	var want []string
	for _, name := range eventDataTypeNames(t) {
		want = append(want, "*events."+name)
	}
	assert.ElementsMatch(t, want, mapKeys(canonical))

	for typeName, data := range canonical {
		t.Run(typeName, func(t *testing.T) {
			fillSample(reflect.ValueOf(data).Elem(), 0)
			session := "rt-" + strings.TrimPrefix(typeName, "*events.")
			require.NoError(t, store.Append(ctx, &Event{
				Type: EventType("test.roundtrip"), Timestamp: sampleTime, SessionID: session, Data: data,
			}))
			got, err := store.Query(ctx, &EventFilter{SessionID: session})
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, data, got[0].Data)
		})
	}
}

// TestDeserializeEventData_LegacyAliasNames pins that recordings written under
// the pre-consolidation type names still decode, into the consolidated type.
func TestDeserializeEventData_LegacyAliasNames(t *testing.T) {
	tests := []struct {
		dataType string
		json     string
		want     EventData
	}{
		{"*events.MiddlewareStartedData", `{"Name":"auth","Index":2}`, &MiddlewareEventData{Name: "auth", Index: 2}},
		{"*events.StageCompletedData", `{"Name":"provider","StageType":"generate"}`,
			&StageEventData{Name: "provider", StageType: "generate"}},
		{"*events.ToolCallFailedData", `{"ToolName":"search","CallID":"c1","Error":"timeout"}`,
			&ToolCallEventData{ToolName: "search", CallID: "c1", Error: errors.New("timeout")}},
		{"*events.ValidationPassedData", `{"ValidatorName":"guard"}`, &ValidationEventData{ValidatorName: "guard"}},
		{"*events.StateLoadedData", `{"ConversationID":"conv","MessageCount":3}`,
			&StateEventData{ConversationID: "conv", MessageCount: 3}},
		{"*events.AudioInputData", `{"actor":"user","direction":"input"}`, &AudioEventData{Actor: "user", Direction: "input"}},
		{"*events.ImageOutputData", `{"generated_from":"dalle","direction":"output"}`,
			&ImageEventData{GeneratedFrom: "dalle", Direction: "output"}},
	}
	for _, tt := range tests {
		t.Run(tt.dataType, func(t *testing.T) {
			assert.Equal(t, tt.want, deserializeEventData(tt.dataType, json.RawMessage(tt.json)))
		})
	}
}

func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
