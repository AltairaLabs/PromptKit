package version

import (
	"os"
	"strings"
	"testing"
)

// withVersionVars temporarily sets version variables and restores them after the test.
func withVersionVars(t *testing.T, v, commit, date string, fn func()) {
	t.Helper()
	origVersion, origCommit, origDate := version, gitCommit, buildDate
	defer func() {
		version, gitCommit, buildDate = origVersion, origCommit, origDate
	}()
	version, gitCommit, buildDate = v, commit, date
	fn()
}

// withLogLevel temporarily sets LOG_LEVEL env var and restores it after the test.
func withLogLevel(t *testing.T, level string, fn func()) {
	t.Helper()
	origLevel := os.Getenv("LOG_LEVEL")
	defer func() {
		if origLevel == "" {
			os.Unsetenv("LOG_LEVEL")
		} else {
			os.Setenv("LOG_LEVEL", origLevel)
		}
	}()
	if level == "" {
		os.Unsetenv("LOG_LEVEL")
	} else {
		os.Setenv("LOG_LEVEL", level)
	}
	fn()
}

func TestGetVersion(t *testing.T) {
	v := GetVersion()
	if v == "" {
		t.Error("GetVersion() returned empty string")
	}
}

func TestGetVersion_NonDev(t *testing.T) {
	withVersionVars(t, "1.0.0", "", "", func() {
		if v := GetVersion(); v != "1.0.0" {
			t.Errorf("Expected '1.0.0', got '%s'", v)
		}
	})
}

func TestGetVersionInfo(t *testing.T) {
	info := GetVersionInfo()
	if !strings.Contains(info, "PromptKit runtime version") {
		t.Errorf("GetVersionInfo() should contain 'PromptKit runtime version', got: %s", info)
	}
}

func TestGetVersionInfo_WithLdflags(t *testing.T) {
	withVersionVars(t, "2.0.0", "def456", "2024-06-15", func() {
		info := GetVersionInfo()
		for _, want := range []string{"2.0.0", "def456", "2024-06-15"} {
			if !strings.Contains(info, want) {
				t.Errorf("Version info should contain '%s', got: %s", want, info)
			}
		}
	})
}

func TestGetBuildInfo(t *testing.T) {
	attrs := GetBuildInfo()
	if len(attrs) < 2 {
		t.Error("GetBuildInfo() should return at least version key-value pair")
	}
	if attrs[0] != "version" {
		t.Errorf("First attribute should be 'version', got: %v", attrs[0])
	}
}

func TestGetBuildInfo_WithLdflags(t *testing.T) {
	withVersionVars(t, "1.2.3", "abc123", "2024-01-01", func() {
		attrs := GetBuildInfo()
		attrMap := make(map[string]any)
		for i := 0; i < len(attrs); i += 2 {
			attrMap[attrs[i].(string)] = attrs[i+1]
		}

		expected := map[string]any{"version": "1.2.3", "commit": "abc123", "built": "2024-01-01"}
		for k, want := range expected {
			if got := attrMap[k]; got != want {
				t.Errorf("%s should be '%v', got: %v", k, want, got)
			}
		}
	})
}

func TestLogStartup(t *testing.T) {
	tests := []struct {
		name  string
		level string
	}{
		{"debug level", "debug"},
		{"trace level", "trace"},
		{"info level", "info"},
		{"no env var", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withLogLevel(t, tt.level, func() {
				LogStartup() // Should not panic
			})
		})
	}
}

func TestGetCommitFromBuildInfo(t *testing.T) {
	// Tests helper function - returns whatever test binary's build info contains
	_ = getCommitFromBuildInfo()
}

func TestIsDirtyFromBuildInfo(t *testing.T) {
	// Tests helper function - returns whatever test binary's build info contains
	_ = isDirtyFromBuildInfo()
}
