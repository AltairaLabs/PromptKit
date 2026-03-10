package handlers

import "testing"

func TestScorePtr(t *testing.T) {
	p := scorePtr(0.5)
	if *p != 0.5 {
		t.Errorf("expected 0.5, got %f", *p)
	}
}

func TestBoolScore(t *testing.T) {
	if *boolScore(true) != 1.0 {
		t.Error("expected 1.0 for true")
	}
	if *boolScore(false) != 0.0 {
		t.Error("expected 0.0 for false")
	}
}

func TestRatioScore(t *testing.T) {
	tests := []struct {
		name         string
		found, total int
		expected     float64
	}{
		{"zero total", 0, 0, 1.0},
		{"all found", 3, 3, 1.0},
		{"half found", 1, 2, 0.5},
		{"none found", 0, 5, 0.0},
		{"clamped above 1", 5, 3, 1.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := *ratioScore(tt.found, tt.total)
			if got != tt.expected {
				t.Errorf("ratioScore(%d, %d) = %f, want %f", tt.found, tt.total, got, tt.expected)
			}
		})
	}
}

func TestInverseRatioScore(t *testing.T) {
	tests := []struct {
		name         string
		found, total int
		expected     float64
	}{
		{"zero total", 0, 0, 1.0},
		{"no violations", 0, 5, 1.0},
		{"all violations", 5, 5, 0.0},
		{"half violations", 1, 2, 0.5},
		{"clamped below 0", 5, 3, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := *inverseRatioScore(tt.found, tt.total)
			if got != tt.expected {
				t.Errorf("inverseRatioScore(%d, %d) = %f, want %f", tt.found, tt.total, got, tt.expected)
			}
		})
	}
}
