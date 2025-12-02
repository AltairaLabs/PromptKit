package selfplay

import (
	"testing"
)

func TestExtractRegionFromPersonaID(t *testing.T) {
	cases := map[string]string{
		"challenger_uk": "uk",
		"persona_au":    "au",
		"tester_us":     "us",
		"plain":         "us", // default
	}

	for in, want := range cases {
		got := extractRegionFromPersonaID(in)
		if got != want {
			t.Fatalf("extractRegionFromPersonaID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateUserResponse_Length(t *testing.T) {
	cg := &ContentGenerator{}

	if err := cg.validateUserResponse("One. Two."); err != nil {
		t.Fatalf("expected no error for two sentences, got: %v", err)
	}

	if err := cg.validateUserResponse("One. Two. Three."); err == nil {
		t.Fatalf("expected error for three sentences, got nil")
	}
}

func TestValidateUserResponse_QuestionCount(t *testing.T) {
	cg := &ContentGenerator{}

	if err := cg.validateUserResponse("Is this ok? Maybe."); err != nil {
		t.Fatalf("expected no error for <=2 questions, got: %v", err)
	}

	if err := cg.validateUserResponse("One? Two? Three?"); err == nil {
		t.Fatalf("expected error for >2 questions, got nil")
	}
}

func TestValidateUserResponse_RoleIntegrity(t *testing.T) {
	cg := &ContentGenerator{}

	if err := cg.validateUserResponse("As an AI, here's your plan."); err == nil {
		t.Fatalf("expected role integrity error, got nil")
	}

	if err := cg.validateUserResponse("Hi there. I have a quick question."); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestExtractRegionFromPersonaID_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		personaID string
		want      string
	}{
		{
			name:      "empty string",
			personaID: "",
			want:      "us",
		},
		{
			name:      "UK in middle not matched",
			personaID: "uk_challenger",
			want:      "us",
		},
		{
			name:      "multiple underscores with UK",
			personaID: "test_persona_uk",
			want:      "uk",
		},
		{
			name:      "AU at start not matched",
			personaID: "au_persona",
			want:      "us",
		},
		{
			name:      "case sensitive lowercase au",
			personaID: "persona_au",
			want:      "au",
		},
		{
			name:      "explicit us suffix",
			personaID: "admin_us",
			want:      "us",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractRegionFromPersonaID(tt.personaID)
			if got != tt.want {
				t.Errorf("extractRegionFromPersonaID(%q) = %q, want %q", tt.personaID, got, tt.want)
			}
		})
	}
}

func TestContentGenerator_NewContentGenerator(t *testing.T) {
	generator := NewContentGenerator(nil, nil)

	if generator == nil {
		t.Fatal("NewContentGenerator() returned nil")
	}

	if generator.provider != nil {
		t.Error("expected nil provider")
	}

	if generator.persona != nil {
		t.Error("expected nil persona")
	}
}

func TestValidateUserResponse_AllProblematicPhrases(t *testing.T) {
	cg := &ContentGenerator{}

	problematicPhrases := []string{
		"here's your plan",
		"here's what you should do",
		"step 1:",
		"step 2:",
		"first, you should",
		"as an ai",
		"i recommend",
		"my suggestion is",
	}

	for _, phrase := range problematicPhrases {
		t.Run(phrase, func(t *testing.T) {
			err := cg.validateUserResponse(phrase)
			if err == nil {
				t.Errorf("expected error for phrase %q, got nil", phrase)
			}
		})
	}
}

func TestValidateUserResponse_CaseInsensitive(t *testing.T) {
	cg := &ContentGenerator{}

	tests := []string{
		"STEP 1: do this",
		"Step 1: do that",
		"step 1: whatever",
		"StEp 1: mixed case",
	}

	for _, test := range tests {
		t.Run(test, func(t *testing.T) {
			err := cg.validateUserResponse(test)
			if err == nil {
				t.Errorf("expected error for %q (case insensitive), got nil", test)
			}
		})
	}
}
