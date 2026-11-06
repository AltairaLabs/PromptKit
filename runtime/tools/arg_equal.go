package tools

import (
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
)

// argValuesEqual reports whether two decoded argument values are equal. It is
// reflect.DeepEqual except that numbers compare by value whatever their Go
// type: YAML decodes 42 as int, encoding/json as float64, and a decoder with
// UseNumber as json.Number, and all three mean the same argument. Maps and
// slices are compared element by element with the same rule; map keys compare
// by their printed form, so a YAML map[any]any equals a JSON map[string]any.
func argValuesEqual(a, b any) bool {
	if ra, ok := numericValue(a); ok {
		rb, ok := numericValue(b)
		return ok && ra.Cmp(rb) == 0
	}
	va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
	switch {
	case isKind(va, reflect.Map) && isKind(vb, reflect.Map):
		return mapsEqual(va, vb)
	case isList(va) && isList(vb):
		return listsEqual(va, vb)
	default:
		return reflect.DeepEqual(a, b)
	}
}

func isKind(v reflect.Value, k reflect.Kind) bool {
	return v.IsValid() && v.Kind() == k
}

func isList(v reflect.Value) bool {
	return isKind(v, reflect.Slice) || isKind(v, reflect.Array)
}

func mapsEqual(a, b reflect.Value) bool {
	if a.Len() != b.Len() {
		return false
	}
	bByKey := make(map[string]reflect.Value, b.Len())
	for _, k := range b.MapKeys() {
		bByKey[fmt.Sprint(k.Interface())] = b.MapIndex(k)
	}
	for _, k := range a.MapKeys() {
		bv, ok := bByKey[fmt.Sprint(k.Interface())]
		if !ok || !argValuesEqual(a.MapIndex(k).Interface(), bv.Interface()) {
			return false
		}
	}
	return true
}

func listsEqual(a, b reflect.Value) bool {
	if a.Len() != b.Len() {
		return false
	}
	for i := 0; i < a.Len(); i++ {
		if !argValuesEqual(a.Index(i).Interface(), b.Index(i).Interface()) {
			return false
		}
	}
	return true
}

// numericValue returns v as an exact rational when v is a Go number or a
// json.Number. NaN and infinities are not numbers here, so they fall back to
// reflect.DeepEqual.
func numericValue(v any) (*big.Rat, bool) {
	switch n := v.(type) {
	case json.Number:
		return new(big.Rat).SetString(n.String())
	case float64:
		return ratFromFloat(n)
	case float32:
		return ratFromFloat(float64(n))
	}
	rv := reflect.ValueOf(v)
	switch {
	case !rv.IsValid():
		return nil, false
	case rv.CanInt():
		return new(big.Rat).SetInt64(rv.Int()), true
	case rv.CanUint():
		return new(big.Rat).SetUint64(rv.Uint()), true
	default:
		return nil, false
	}
}

func ratFromFloat(f float64) (*big.Rat, bool) {
	r := new(big.Rat).SetFloat64(f)
	return r, r != nil
}
