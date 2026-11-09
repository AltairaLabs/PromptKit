package types

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// fillAll sets every exported field of v to a non-zero value.
func fillAll(v reflect.Value, depth int) {
	if depth > 6 {
		return
	}
	switch v.Kind() {
	case reflect.Ptr:
		v.Set(reflect.New(v.Type().Elem()))
		fillAll(v.Elem(), depth+1)
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(time.Time{}) {
			v.Set(reflect.ValueOf(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)))
			return
		}
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				fillAll(v.Field(i), depth+1)
			}
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
		fillAll(s.Index(0), depth+1)
		v.Set(s)
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		key := reflect.New(v.Type().Key()).Elem()
		fillAll(key, depth+1)
		val := reflect.New(v.Type().Elem()).Elem()
		fillAll(val, depth+1)
		m.SetMapIndex(key, val)
		v.Set(m)
	case reflect.Interface:
		if v.NumMethod() == 0 {
			v.Set(reflect.ValueOf("v"))
		}
	}
}

// TestCloneMessage_KeepsEveryFieldAndIsolatesMedia pins that CloneMessage
// copies every field, including every MediaContent field a future change
// adds, and that the clone's parts and media do not alias the original's.
func TestCloneMessage_KeepsEveryFieldAndIsolatesMedia(t *testing.T) {
	var orig Message
	fillAll(reflect.ValueOf(&orig).Elem(), 0)

	clone := CloneMessage(orig)
	if !reflect.DeepEqual(orig, clone) {
		t.Fatalf("clone differs from original:\norig  %+v\nclone %+v", orig, clone)
	}

	*clone.Parts[0].Text = "changed"
	*clone.Parts[0].Media.URL = "changed"
	*clone.Parts[0].Media.Width = 99
	clone.Meta["s"] = "changed"
	if *orig.Parts[0].Text != "s" || *orig.Parts[0].Media.URL != "s" || *orig.Parts[0].Media.Width != 7 {
		t.Error("mutating the clone's part changed the original")
	}
	if orig.Meta["s"] != "v" {
		t.Error("mutating the clone's Meta changed the original")
	}
}
