package guardrails

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestRequiredFieldsHook_Name(t *testing.T) {
	h := NewRequiredFieldsHook([]string{"name"})
	if h.Name() != "required_fields" {
		t.Errorf("Name() = %q, want %q", h.Name(), "required_fields")
	}
}

func TestRequiredFieldsHook_BeforeCall(t *testing.T) {
	h := NewRequiredFieldsHook([]string{"name"})
	d := h.BeforeCall(context.Background(), &hooks.ProviderRequest{})
	if !d.Allow {
		t.Error("BeforeCall should always allow")
	}
}

func TestRequiredFieldsHook_AfterCall(t *testing.T) {
	h := NewRequiredFieldsHook([]string{"name", "email"})

	tests := []struct {
		name    string
		content string
		allow   bool
		missing int
	}{
		{"all present", "name: John, email: john@test.com", true, 0},
		{"missing email", "name: John", false, 1},
		{"missing both", "nothing here", false, 2},
		{"empty content", "", false, 2},
		{"all present different order", "email: test, name: test", true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := h.AfterCall(context.Background(), &hooks.ProviderRequest{}, &hooks.ProviderResponse{
				Message: types.Message{Content: tt.content},
			})
			if d.Allow != tt.allow {
				t.Errorf("Allow = %v, want %v", d.Allow, tt.allow)
			}
			if !tt.allow {
				if d.Metadata["validator_type"] != "required_fields" {
					t.Errorf("validator_type = %v, want required_fields", d.Metadata["validator_type"])
				}
				missing := d.Metadata["missing"].([]string)
				if len(missing) != tt.missing {
					t.Errorf("missing count = %d, want %d", len(missing), tt.missing)
				}
			}
		})
	}
}

func TestRequiredFieldsHook_EmptyFields(t *testing.T) {
	h := NewRequiredFieldsHook([]string{})
	d := h.AfterCall(context.Background(), &hooks.ProviderRequest{}, &hooks.ProviderResponse{
		Message: types.Message{Content: "anything"},
	})
	if !d.Allow {
		t.Error("empty required fields should allow")
	}
}

func TestRequiredFieldsHook_NotStreamable(t *testing.T) {
	h := NewRequiredFieldsHook([]string{"name"})
	if _, ok := interface{}(h).(hooks.ChunkInterceptor); ok {
		t.Error("RequiredFieldsHook should not implement ChunkInterceptor")
	}
}
