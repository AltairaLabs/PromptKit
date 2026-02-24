package hooks

import "testing"

func TestDeny(t *testing.T) {
	d := Deny("nope")
	if d.Allow {
		t.Error("Deny should produce Allow=false")
	}
	if d.Reason != "nope" {
		t.Errorf("expected reason 'nope', got %q", d.Reason)
	}
	if d.Metadata != nil {
		t.Error("Deny should not set metadata")
	}
}

func TestDenyWithMetadata(t *testing.T) {
	meta := map[string]any{"validator_type": "banned_words"}
	d := DenyWithMetadata("bad word", meta)
	if d.Allow {
		t.Error("DenyWithMetadata should produce Allow=false")
	}
	if d.Reason != "bad word" {
		t.Errorf("expected reason 'bad word', got %q", d.Reason)
	}
	if d.Metadata["validator_type"] != "banned_words" {
		t.Error("metadata not preserved")
	}
}

func TestAllow(t *testing.T) {
	if !Allow.Allow {
		t.Error("Allow sentinel should have Allow=true")
	}
	if Allow.Reason != "" {
		t.Error("Allow sentinel should have empty reason")
	}
}
