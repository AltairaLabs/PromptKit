package pack

import (
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
)

// specToolPolicyKeys are the keys the PromptPack spec defines for a prompt's
// tool_policy, read from the generated packspec.ToolPolicy so they follow the
// embedded schema.
var specToolPolicyKeys = jsonKeys(reflect.TypeFor[packspec.ToolPolicy]())

// jsonKeys returns the JSON names of t's fields.
func jsonKeys(t reflect.Type) map[string]bool {
	keys := make(map[string]bool, t.NumField())
	for i := range t.NumField() {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			keys[name] = true
		}
	}
	return keys
}

// warnedToolPolicyKeys holds the "path\x00prompt\x00key" entries already
// reported. A workflow reloads its pack for every state it opens, and a
// multi-agent open loads it once per member; the warning is for the author,
// once per key, not once per load.
var warnedToolPolicyKeys sync.Map

// warnDroppedToolPolicyKeys logs, for each prompt, every tool_policy key the
// spec does not define. Schema validation rejects such a key; with validation
// skipped, decoding drops it and the limit it names never applies. An
// example is max_calls_per_minute, which is a host setting
// (sdk.WithToolPolicy), not pack vocabulary. A pack that does not parse is left to Parse to report.
func warnDroppedToolPolicyKeys(path string, data []byte) {
	var raw struct {
		Prompts map[string]struct {
			ToolPolicy map[string]json.RawMessage `json:"tool_policy"`
		} `json:"prompts"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return
	}
	for _, name := range slices.Sorted(maps.Keys(raw.Prompts)) {
		for _, key := range slices.Sorted(maps.Keys(raw.Prompts[name].ToolPolicy)) {
			if specToolPolicyKeys[key] {
				continue
			}
			if _, seen := warnedToolPolicyKeys.LoadOrStore(path+"\x00"+name+"\x00"+key, true); !seen {
				logger.Warn("pack: tool_policy key is not in the PromptPack spec and is ignored",
					"prompt", name, "key", key)
			}
		}
	}
}
