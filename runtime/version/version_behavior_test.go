package version

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"runtime/debug"
	"strings"
	"testing"
)

func TestLogStartup_LogsOnlyAtDebugOrTrace(t *testing.T) {
	tests := []struct {
		level   string
		wantLog bool
	}{
		{"debug", true}, {"TRACE", true}, {"info", false}, {"warn", false}, {"", false},
	}
	for _, tt := range tests {
		t.Run("LOG_LEVEL="+tt.level, func(t *testing.T) {
			var buf bytes.Buffer
			orig := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
			t.Cleanup(func() { slog.SetDefault(orig) })

			withLogLevel(t, tt.level, LogStartup)

			out := strings.TrimSpace(buf.String())
			if !tt.wantLog {
				if out != "" {
					t.Fatalf("logged at LOG_LEVEL=%q: %s", tt.level, out)
				}
				return
			}
			var rec map[string]any
			if err := json.Unmarshal([]byte(out), &rec); err != nil {
				t.Fatalf("want one JSON record, got %q: %v", out, err)
			}
			if rec["msg"] != "PromptKit runtime starting" || rec["level"] != "DEBUG" {
				t.Errorf("record = %v", rec)
			}
			if rec["version"] != GetVersion() {
				t.Errorf("version = %v, want %v", rec["version"], GetVersion())
			}
		})
	}
}

func TestCommitFromSettings(t *testing.T) {
	tests := []struct {
		name     string
		settings []debug.BuildSetting
		want     string
	}{
		{"full hash shortened", []debug.BuildSetting{{Key: "vcs.revision", Value: "0123456789abcdef"}}, "0123456"},
		{"short hash kept", []debug.BuildSetting{{Key: "vcs.revision", Value: "abc"}}, "abc"},
		{"empty revision ignored", []debug.BuildSetting{{Key: "vcs.revision", Value: ""}}, ""},
		{"other keys ignored", []debug.BuildSetting{{Key: "vcs.time", Value: "2026"}}, ""},
		{"no settings", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := commitFromSettings(tt.settings); got != tt.want {
				t.Errorf("commitFromSettings = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDirtyFromSettings(t *testing.T) {
	tests := []struct {
		settings []debug.BuildSetting
		want     bool
	}{
		{[]debug.BuildSetting{{Key: "vcs.modified", Value: "true"}}, true},
		{[]debug.BuildSetting{{Key: "vcs.modified", Value: "false"}}, false},
		{[]debug.BuildSetting{{Key: "vcs.revision", Value: "true"}}, false},
		{nil, false},
	}
	for _, tt := range tests {
		if got := dirtyFromSettings(tt.settings); got != tt.want {
			t.Errorf("dirtyFromSettings(%v) = %v, want %v", tt.settings, got, tt.want)
		}
	}
}
