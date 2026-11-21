package agui

import (
	"bytes"
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
)

// JSON Patch (RFC 6902) operations the adapter emits.
const (
	patchAdd     = "add"
	patchRemove  = "remove"
	patchReplace = "replace"
)

// stateSync keeps the client's state in line with the StateProvider's: the
// first state the run reads is sent whole, as STATE_SNAPSHOT, and each change
// after it as a STATE_DELTA patching the state the client holds.
type stateSync struct {
	// sent is the state the client holds, as JSON values; valid when hasSent.
	sent    any
	hasSent bool
}

// syncState reads the state and sends what changed since the client's copy.
func (st *runState) syncState() {
	sp := st.a.cfg.stateProvider
	if sp == nil {
		return
	}
	snapshot, err := sp.Snapshot(st.a.sender)
	if err != nil {
		return
	}
	current, err := normalizeState(snapshot)
	if err != nil || current == nil {
		return // no state to send: AG-UI's snapshot must carry one
	}
	s := &st.state
	if s.hasSent {
		ops, ok := statePatch(s.sent, current)
		if ok && len(ops) == 0 {
			return
		}
		if ok {
			st.emit(aguievents.NewStateDeltaEvent(ops))
			s.sent = current
			return
		}
	}
	st.emit(aguievents.NewStateSnapshotEvent(snapshot))
	s.sent, s.hasSent = current, true
}

// normalizeState is v as plain JSON values (maps, slices, strings, bools,
// json.Number, nil), so two states compare by what a client receives.
func normalizeState(v any) (any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// statePatch is the JSON Patch turning from into to, both normalized states.
// It reports false for a change a patch the AG-UI Go SDK can encode cannot
// carry: one that replaces the whole state (the SDK rejects the root path) or
// sets a value to null (the SDK drops a null value from its operation).
func statePatch(from, to any) ([]aguievents.JSONPatchOperation, bool) {
	if !sameContainer(from, to) {
		if reflect.DeepEqual(from, to) {
			return nil, true
		}
		return nil, false
	}
	var ops []aguievents.JSONPatchOperation
	if !diffValue("", from, to, &ops) {
		return nil, false
	}
	return ops, true
}

// sameContainer reports whether a and b are both objects or both arrays.
func sameContainer(a, b any) bool {
	switch a.(type) {
	case map[string]any:
		_, ok := b.(map[string]any)
		return ok
	case []any:
		_, ok := b.([]any)
		return ok
	}
	return false
}

// diffValue appends the operations turning from into to at path. It reports
// false when one of them would carry a null value.
func diffValue(path string, from, to any, ops *[]aguievents.JSONPatchOperation) bool {
	if reflect.DeepEqual(from, to) {
		return true
	}
	if sameContainer(from, to) {
		if f, ok := from.(map[string]any); ok {
			return diffObject(path, f, to.(map[string]any), ops)
		}
		return diffArray(path, from.([]any), to.([]any), ops)
	}
	return appendSet(ops, patchReplace, path, to)
}

func diffObject(path string, from, to map[string]any, ops *[]aguievents.JSONPatchOperation) bool {
	union := maps.Clone(from)
	maps.Copy(union, to)
	keys := slices.Sorted(maps.Keys(union))
	for _, k := range keys {
		child := path + "/" + escapePointer(k)
		fv, inFrom := from[k]
		tv, inTo := to[k]
		var ok bool
		switch {
		case !inTo:
			*ops = append(*ops, aguievents.JSONPatchOperation{Op: patchRemove, Path: child})
			ok = true
		case !inFrom:
			ok = appendSet(ops, patchAdd, child, tv)
		default:
			ok = diffValue(child, fv, tv, ops)
		}
		if !ok {
			return false
		}
	}
	return true
}

// diffArray patches the elements both arrays hold in place, then adds the
// new tail or removes the old one, last element first, so every index names
// an element that exists when its operation applies.
func diffArray(path string, from, to []any, ops *[]aguievents.JSONPatchOperation) bool {
	common := min(len(from), len(to))
	for i := 0; i < common; i++ {
		if !diffValue(path+"/"+strconv.Itoa(i), from[i], to[i], ops) {
			return false
		}
	}
	for i := common; i < len(to); i++ {
		if !appendSet(ops, patchAdd, path+"/"+strconv.Itoa(i), to[i]) {
			return false
		}
	}
	for i := len(from) - 1; i >= common; i-- {
		*ops = append(*ops, aguievents.JSONPatchOperation{Op: patchRemove, Path: path + "/" + strconv.Itoa(i)})
	}
	return true
}

// appendSet appends an add or replace of value at path, or reports false for
// a null value, which the AG-UI Go SDK cannot encode, and for the root path,
// which it rejects.
func appendSet(ops *[]aguievents.JSONPatchOperation, op, path string, value any) bool {
	if value == nil || path == "" {
		return false
	}
	*ops = append(*ops, aguievents.JSONPatchOperation{Op: op, Path: path, Value: value})
	return true
}

// escapePointer escapes a key as a JSON Pointer (RFC 6901) reference token.
func escapePointer(key string) string {
	return strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}
