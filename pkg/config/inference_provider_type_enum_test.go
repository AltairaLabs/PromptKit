package config

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/inference/all"
)

// The schema enum for an inference provider's type lists exactly the backends
// runtime/inference/all registers, so a config naming any of them validates
// and the two cannot drift apart (#2708).
func TestInferenceProviderConfig_TypeEnumMatchesRegisteredBackends(t *testing.T) {
	field, ok := reflect.TypeOf(InferenceProviderConfig{}).FieldByName("Type")
	if !ok {
		t.Fatal("InferenceProviderConfig has no Type field")
	}
	var enum []string
	for _, part := range strings.Split(field.Tag.Get("jsonschema"), ",") {
		if v, found := strings.CutPrefix(part, "enum="); found {
			enum = append(enum, v)
		}
	}
	sort.Strings(enum)

	registered := append([]string(nil), inference.RegisteredTypes()...)
	sort.Strings(registered)

	want := []string{"huggingface", "nvidia-topic-control", "openai", "systemone"}
	if strings.Join(registered, ",") != strings.Join(want, ",") {
		t.Fatalf("registered inference backends = %v, want %v (update this test and the enum together)", registered, want)
	}
	if strings.Join(enum, ",") != strings.Join(registered, ",") {
		t.Errorf("jsonschema enum = %v, want the registered backends %v", enum, registered)
	}
}
