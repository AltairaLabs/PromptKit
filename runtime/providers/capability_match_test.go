package providers

import (
	"slices"
	"testing"
)

func TestMissingCapabilities(t *testing.T) {
	tests := []struct {
		name     string
		declared []string
		required []string
		want     []string
	}{
		{"all declared", []string{CapabilityText, CapabilityTools}, []string{CapabilityTools}, nil},
		{"tools missing", []string{CapabilityText, CapabilityVision}, []string{CapabilityVision, CapabilityTools}, []string{CapabilityTools}},
		{"nothing declared", nil, []string{CapabilityTools, CapabilityText}, []string{CapabilityTools, CapabilityText}},
		{"nothing required", []string{CapabilityText}, nil, nil},
		{"duplicate requirement reported once", []string{CapabilityText}, []string{CapabilityTools, CapabilityTools}, []string{CapabilityTools}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MissingCapabilities(tt.declared, tt.required)
			if !slices.Equal(got, tt.want) {
				t.Errorf("MissingCapabilities(%q, %q) = %q, want %q", tt.declared, tt.required, got, tt.want)
			}
			if gotHas := HasCapabilities(tt.declared, tt.required); gotHas != (len(tt.want) == 0) {
				t.Errorf("HasCapabilities(%q, %q) = %v, want %v", tt.declared, tt.required, gotHas, len(tt.want) == 0)
			}
		})
	}
}
