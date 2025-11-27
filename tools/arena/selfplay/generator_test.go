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
