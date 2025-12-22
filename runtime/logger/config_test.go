package logger

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestModuleConfig_LevelFor(t *testing.T) {
	mc := NewModuleConfig(slog.LevelInfo)

	// Set up some module levels
	mc.SetModuleLevel("runtime", slog.LevelWarn)
	mc.SetModuleLevel("runtime.pipeline", slog.LevelDebug)
	mc.SetModuleLevel("providers.openai", slog.LevelError)

	tests := []struct {
		module   string
		expected slog.Level
	}{
		// Exact matches
		{"runtime", slog.LevelWarn},
		{"runtime.pipeline", slog.LevelDebug},
		{"providers.openai", slog.LevelError},

		// Hierarchy matches
		{"runtime.pipeline.stage", slog.LevelDebug}, // inherits from runtime.pipeline
		{"runtime.streaming", slog.LevelWarn},       // inherits from runtime
		{"providers.openai.chat", slog.LevelError},  // inherits from providers.openai

		// No match - use default
		{"sdk", slog.LevelInfo},
		{"providers.anthropic", slog.LevelInfo},
		{"unknown", slog.LevelInfo},
		{"", slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.module, func(t *testing.T) {
			result := mc.LevelFor(tt.module)
			if result != tt.expected {
				t.Errorf("LevelFor(%q) = %v, want %v", tt.module, result, tt.expected)
			}
		})
	}
}

func TestModuleConfig_SetDefaultLevel(t *testing.T) {
	mc := NewModuleConfig(slog.LevelInfo)

	// Initially should be Info
	if mc.LevelFor("anything") != slog.LevelInfo {
		t.Error("Expected initial default to be Info")
	}

	// Change default
	mc.SetDefaultLevel(slog.LevelDebug)

	if mc.LevelFor("anything") != slog.LevelDebug {
		t.Error("Expected default to change to Debug")
	}
}

func TestConfigure(t *testing.T) {
	// Save original logger state
	originalLogger := DefaultLogger
	defer func() { DefaultLogger = originalLogger }()

	cfg := &LoggingConfigSpec{
		DefaultLevel: "warn",
		Format:       FormatText,
		CommonFields: map[string]string{
			"service": "test",
		},
		Modules: []ModuleLoggingSpec{
			{Name: "runtime", Level: "debug"},
		},
	}

	err := Configure(cfg)
	if err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	// Verify the module config was set
	mc := GetModuleConfig()
	if mc.LevelFor("runtime") != slog.LevelDebug {
		t.Error("Expected runtime module to be debug level")
	}
	if mc.LevelFor("other") != slog.LevelWarn {
		t.Error("Expected default level to be warn")
	}
}

func TestConfigure_Nil(t *testing.T) {
	err := Configure(nil)
	if err != nil {
		t.Errorf("Configure(nil) should not error, got: %v", err)
	}
}

func TestConfigure_JSONFormat(t *testing.T) {
	// Save original state
	originalLogger := DefaultLogger
	originalOutput := logOutput
	defer func() {
		DefaultLogger = originalLogger
		logOutput = originalOutput
	}()

	var buf bytes.Buffer
	logOutput = &buf

	cfg := &LoggingConfigSpec{
		DefaultLevel: "info",
		Format:       FormatJSON,
	}

	err := Configure(cfg)
	if err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	// Log something
	Info("test message", "key", "value")

	output := buf.String()

	// JSON output should contain JSON markers
	if !strings.Contains(output, `"msg"`) {
		t.Errorf("Expected JSON output, got: %s", output)
	}
	if !strings.Contains(output, `"key"`) {
		t.Errorf("Expected key in JSON output, got: %s", output)
	}
}

func TestModuleHandler_FiltersByLevel(t *testing.T) {
	var buf bytes.Buffer

	// Create module config that sets runtime.test to warn level
	mc := NewModuleConfig(slog.LevelInfo)
	mc.SetModuleLevel("runtime.logger", slog.LevelWarn)

	textHandler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug, // Base level allows all
	})

	handler := NewModuleHandler(textHandler, mc)
	logger := slog.New(handler)

	// This should be filtered because runtime.logger is at warn level
	logger.Info("this should be filtered")

	// This should appear
	logger.Warn("this should appear")

	output := buf.String()

	if strings.Contains(output, "filtered") {
		t.Errorf("Info message should have been filtered, got: %s", output)
	}
	if !strings.Contains(output, "should appear") {
		t.Errorf("Warn message should appear, got: %s", output)
	}
}

func TestModuleHandler_AddsLoggerField(t *testing.T) {
	var buf bytes.Buffer

	mc := NewModuleConfig(slog.LevelDebug)

	textHandler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	handler := NewModuleHandler(textHandler, mc)
	logger := slog.New(handler)

	logger.Info("test message")

	output := buf.String()

	// Should contain logger field with module name
	if !strings.Contains(output, "logger=") {
		t.Errorf("Expected logger field in output, got: %s", output)
	}
}

func TestModuleHandler_WithContextFields(t *testing.T) {
	var buf bytes.Buffer

	mc := NewModuleConfig(slog.LevelDebug)

	textHandler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	handler := NewModuleHandler(textHandler, mc)
	logger := slog.New(handler)

	ctx := WithTurnID(context.Background(), "turn-123")
	logger.InfoContext(ctx, "test message")

	output := buf.String()

	// Should contain turn_id from context
	if !strings.Contains(output, "turn_id=turn-123") {
		t.Errorf("Expected turn_id in output, got: %s", output)
	}
}

func TestSetOutput(t *testing.T) {
	// Save original state
	originalLogger := DefaultLogger
	defer func() {
		DefaultLogger = originalLogger
		SetOutput(nil) // Reset to stderr
	}()

	var buf bytes.Buffer
	SetOutput(&buf)

	Info("test message")

	output := buf.String()
	if !strings.Contains(output, "test message") {
		t.Errorf("Expected message in buffer, got: %s", output)
	}
}

func TestSetOutput_NilResetsToStderr(t *testing.T) {
	// This just verifies SetOutput(nil) doesn't panic
	SetOutput(nil)
}

func TestExtractModuleFromFunction(t *testing.T) {
	tests := []struct {
		fn       string
		expected string
	}{
		{
			"github.com/AltairaLabs/PromptKit/runtime/pipeline.(*Executor).Run",
			"runtime.pipeline",
		},
		{
			"github.com/AltairaLabs/PromptKit/runtime/logger.Info",
			"runtime.logger",
		},
		{
			"github.com/AltairaLabs/PromptKit/tools/arena/engine.Execute",
			"tools.arena.engine",
		},
		{
			"github.com/AltairaLabs/PromptKit/sdk.Init",
			"sdk",
		},
		{
			"github.com/other/package.Func",
			"", // Not our module
		},
		{
			"",
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.fn, func(t *testing.T) {
			result := extractModuleFromFunction(tt.fn)
			if result != tt.expected {
				t.Errorf("extractModuleFromFunction(%q) = %q, want %q", tt.fn, result, tt.expected)
			}
		})
	}
}

func TestModuleHandler_WithAttrs(t *testing.T) {
	var buf bytes.Buffer

	mc := NewModuleConfig(slog.LevelDebug)

	textHandler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	handler := NewModuleHandler(textHandler, mc)

	// Test WithAttrs returns a new handler with the attrs
	newHandler := handler.WithAttrs([]slog.Attr{slog.String("test_attr", "value")})

	if newHandler == nil {
		t.Error("WithAttrs returned nil")
	}

	// Verify it's a ModuleHandler
	if _, ok := newHandler.(*ModuleHandler); !ok {
		t.Error("WithAttrs should return a *ModuleHandler")
	}
}

func TestModuleHandler_WithGroup(t *testing.T) {
	var buf bytes.Buffer

	mc := NewModuleConfig(slog.LevelDebug)

	textHandler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	handler := NewModuleHandler(textHandler, mc)

	// Test WithGroup returns a new handler with the group
	newHandler := handler.WithGroup("test_group")

	if newHandler == nil {
		t.Error("WithGroup returned nil")
	}

	// Verify it's a ModuleHandler
	if _, ok := newHandler.(*ModuleHandler); !ok {
		t.Error("WithGroup should return a *ModuleHandler")
	}
}

func TestModuleHandler_Handle_FiltersLowLevelLogs(t *testing.T) {
	var buf bytes.Buffer

	// Create module config that sets high level for runtime.logger
	mc := NewModuleConfig(slog.LevelError) // Default to error only

	textHandler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug, // Base level allows all
	})

	handler := NewModuleHandler(textHandler, mc)
	logger := slog.New(handler)

	// Debug and Info should be filtered at error level
	logger.Debug("debug message")
	logger.Info("info message")

	output := buf.String()

	// Neither message should appear
	if strings.Contains(output, "debug message") {
		t.Error("Debug message should have been filtered")
	}
	if strings.Contains(output, "info message") {
		t.Error("Info message should have been filtered")
	}
}
