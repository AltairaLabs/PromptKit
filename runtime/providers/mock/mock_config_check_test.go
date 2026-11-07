package mock

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeMockConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mock.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNewFileMockRepository_RejectsUnknownKeys(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "top-level typo",
			body: "default_response: hi\n",
			want: []string{`unknown key "default_response"`, "valid: defaultResponse, scenarios, selfplay"},
		},
		{
			name: "scenario typo",
			body: "scenarios:\n  s1:\n    turn:\n      1: hi\n",
			want: []string{`unknown key "turn"`, "valid: defaultResponse, steps, turns"},
		},
		{
			name: "structured turn typo",
			body: "scenarios:\n  s1:\n    turns:\n      1:\n        contnet: hi\n",
			want: []string{`scenarios.s1.turns.1: unknown key "contnet"`, "valid: audio_file"},
		},
		{
			name: "tool call typo",
			body: "scenarios:\n  s1:\n    turns:\n      1:\n        tool_calls:\n          - name: t\n            args: {}\n",
			want: []string{`scenarios.s1.turns.1.tool_calls[0]: unknown key "args"`, "valid: arguments, name"},
		},
		{
			name: "part typo in a selfplay step",
			body: "selfplay:\n  p1:\n    steps:\n      a:\n        parts:\n          - type: text\n            txt: hi\n",
			want: []string{`selfplay.p1.steps.a.parts[0]: unknown key "txt"`, "valid: audio_url"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewFileMockRepository(writeMockConfig(t, tt.body))
			if err == nil {
				t.Fatal("expected an unknown-key error, got nil")
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not contain %q", err, w)
				}
			}
		})
	}
}

func TestNewFileMockRepository_AcceptsKnownKeys(t *testing.T) {
	body := `defaultResponse: fallback
scenarios:
  s1:
    defaultResponse: s1 default
    turns:
      1: plain
      2:
        type: multimodal
        text: hi
        parts:
          - type: image
            image_url: {url: "mock://x.png"}
            metadata: {a: 1}
        audio_file: a.pcm
        audio_sample_rate: 16000
        audio_mime_type: audio/pcm
      3:
        response: legacy
        tool_calls:
          - name: lookup
            arguments: {q: x}
    steps:
      a: step text
selfplay:
  p1:
    turns:
      1: user line
`
	repo, err := NewFileMockRepository(writeMockConfig(t, body))
	if err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	got, err := repo.GetResponse(context.Background(), ResponseParams{})
	if err != nil || got != "fallback" {
		t.Fatalf("GetResponse = %q, %v; want fallback", got, err)
	}
}

func TestNewFileMockRepository_EmptyFile(t *testing.T) {
	if _, err := NewFileMockRepository(writeMockConfig(t, "")); err != nil {
		t.Fatalf("empty config rejected: %v", err)
	}
}
