package config

import (
	"fmt"
	"strings"
	"testing"
)

func TestDefaultLoggingConfig(t *testing.T) {
	cfg := DefaultLoggingConfig()

	if cfg.DefaultLevel != LogLevelInfo {
		t.Errorf("DefaultLevel: expected %s, got %s", LogLevelInfo, cfg.DefaultLevel)
	}
	if cfg.Format != LogFormatText {
		t.Errorf("Format: expected %s, got %s", LogFormatText, cfg.Format)
	}
}

func TestLoggingConfigSpec_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     LoggingConfigSpec
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			cfg: LoggingConfigSpec{
				DefaultLevel: LogLevelDebug,
				Format:       LogFormatJSON,
				Modules: []ModuleLoggingConfig{
					{Name: "runtime", Level: LogLevelDebug},
				},
			},
			wantErr: false,
		},
		{
			name:    "empty config is valid",
			cfg:     LoggingConfigSpec{},
			wantErr: false,
		},
		{
			name: "invalid default level",
			cfg: LoggingConfigSpec{
				DefaultLevel: "invalid",
			},
			wantErr: true,
			errMsg:  "defaultLevel",
		},
		{
			name: "invalid format",
			cfg: LoggingConfigSpec{
				Format: "xml",
			},
			wantErr: true,
			errMsg:  "format",
		},
		{
			name: "module without name",
			cfg: LoggingConfigSpec{
				Modules: []ModuleLoggingConfig{
					{Level: LogLevelDebug},
				},
			},
			wantErr: true,
			errMsg:  "module name is required",
		},
		{
			name: "module with invalid level",
			cfg: LoggingConfigSpec{
				Modules: []ModuleLoggingConfig{
					{Name: "runtime", Level: "invalid"},
				},
			},
			wantErr: true,
			errMsg:  "modules[runtime].level",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errMsg != "" {
					if ve, ok := err.(*ValidationError); ok {
						if ve.Field != tt.errMsg && ve.Message != tt.errMsg {
							// Check if error message contains expected string
							if !strings.Contains(err.Error(), tt.errMsg) {
								t.Errorf("error should contain %q, got: %v", tt.errMsg, err)
							}
						}
					}
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestValidationError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      ValidationError
		expected string
	}{
		{
			name: "with value",
			err: ValidationError{
				Field:   "providers[0].role",
				Message: "unknown role",
				Value:   "xml",
			},
			expected: "config validation error: providers[0].role: unknown role (got: xml)",
		},
		{
			name: "without value",
			err: ValidationError{
				Field:   "mcp_servers[0].name",
				Message: "name is required",
			},
			expected: "config validation error: mcp_servers[0].name: name is required",
		},
		{
			name: "logging section",
			err: ValidationError{
				Section: "logging",
				Field:   "format",
				Message: "must be json or text",
				Value:   "xml",
			},
			expected: "logging config validation error: format: must be json or text (got: xml)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.expected {
				t.Errorf("Error() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestLoggingValidate_KeepsLoggingPrefix(t *testing.T) {
	cases := map[string]LoggingConfigSpec{
		"default level": {DefaultLevel: "loud"},
		"format":        {Format: "xml"},
		"module name":   {Modules: []ModuleLoggingConfig{{Level: LogLevelInfo}}},
		"module level":  {Modules: []ModuleLoggingConfig{{Name: "m", Level: "loud"}}},
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			err := spec.Validate()
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.HasPrefix(err.Error(), "logging config validation error: ") {
				t.Errorf("Error() = %q, want the logging prefix", err.Error())
			}
		})
	}
}

func TestRuntimeConfigValidate_NonLoggingErrorHasNoLoggingPrefix(t *testing.T) {
	spec := RuntimeConfigSpec{MCPServers: []MCPServerConfig{{Command: "x"}}}
	err := spec.Validate()
	if err == nil {
		t.Fatal("expected an error for an mcp_servers entry with no name")
	}
	if strings.Contains(err.Error(), "logging") {
		t.Errorf("Error() = %q names logging for an mcp_servers field", err.Error())
	}
	if !strings.HasPrefix(err.Error(), "config validation error: mcp_servers[0]") {
		t.Errorf("Error() = %q, want the generic prefix and the field", err.Error())
	}
}

func TestIsValidLogLevel(t *testing.T) {
	validLevels := []string{"trace", "debug", "info", "warn", "error"}
	for _, level := range validLevels {
		if !isValidLogLevel(level) {
			t.Errorf("expected %s to be valid", level)
		}
	}

	invalidLevels := []string{"", "TRACE", "verbose", "fatal", "panic"}
	for _, level := range invalidLevels {
		if isValidLogLevel(level) {
			t.Errorf("expected %s to be invalid", level)
		}
	}
}

func TestLoggingConfigSpec_Validate_ModuleIndexFormat(t *testing.T) {
	// Verify that the module index in the error field uses numeric format
	// (e.g. "modules[0].name") not ASCII rune conversion.
	cfg := LoggingConfigSpec{
		Modules: []ModuleLoggingConfig{
			{Name: "valid", Level: LogLevelDebug},
			{Name: "", Level: LogLevelDebug}, // index 1 has empty name
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty module name")
	}

	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if ve.Field != "modules[1].name" {
		t.Errorf("expected field 'modules[1].name', got %q", ve.Field)
	}
}

func TestLoggingConfigSpec_Validate_HighModuleIndex(t *testing.T) {
	// For indices >= 10, the old string(rune('0'+i)) would produce wrong characters.
	modules := make([]ModuleLoggingConfig, 11)
	for i := range 10 {
		modules[i] = ModuleLoggingConfig{Name: fmt.Sprintf("mod%d", i), Level: LogLevelInfo}
	}
	modules[10] = ModuleLoggingConfig{Name: "", Level: LogLevelInfo} // empty at index 10

	cfg := LoggingConfigSpec{Modules: modules}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty module name at index 10")
	}
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if ve.Field != "modules[10].name" {
		t.Errorf("expected field 'modules[10].name', got %q", ve.Field)
	}
}

func TestLoggingConfig_Structure(t *testing.T) {
	cfg := LoggingConfig{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "LoggingConfig",
		Metadata: ObjectMeta{
			Name: "test",
		},
		Spec: LoggingConfigSpec{
			DefaultLevel: LogLevelInfo,
			Format:       LogFormatText,
			CommonFields: map[string]string{
				"service": "test",
			},
			Modules: []ModuleLoggingConfig{
				{
					Name:  "runtime",
					Level: LogLevelDebug,
					Fields: map[string]string{
						"component": "core",
					},
				},
			},
		},
	}

	if cfg.APIVersion != "promptkit.altairalabs.ai/v1alpha1" {
		t.Errorf("unexpected APIVersion: %s", cfg.APIVersion)
	}
	if cfg.Kind != "LoggingConfig" {
		t.Errorf("unexpected Kind: %s", cfg.Kind)
	}
	if cfg.Metadata.Name != "test" {
		t.Errorf("unexpected Metadata.Name: %s", cfg.Metadata.Name)
	}
	if len(cfg.Spec.Modules) != 1 {
		t.Errorf("unexpected number of modules: %d", len(cfg.Spec.Modules))
	}
}
