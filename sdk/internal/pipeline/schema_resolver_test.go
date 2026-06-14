package pipeline

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewSchemaResolver(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "s.json"), []byte(`{"type":"object"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewSchemaResolver(dir)

	got, err := r("s.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"type":"object"}` {
		t.Errorf("got %s", got)
	}

	if b, err := r(""); err != nil || b != nil {
		t.Errorf("empty path = (%s,%v), want (nil,nil)", b, err)
	}

	if _, err := r("missing.json"); err == nil {
		t.Error("expected error for missing file")
	}
}
