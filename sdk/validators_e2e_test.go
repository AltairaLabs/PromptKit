package sdk_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// syncBuffer is a mutex-guarded bytes.Buffer. Needed because the SDK's
// global logger is shared across pipeline goroutines which keep emitting
// log records concurrently with the test reading the captured output.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureValidatorLogs redirects the global logger to a concurrency-safe
// buffer for the duration of a test. Local to this file because
// eval_middleware_test.go's captureLogs lives in package sdk and this file
// is in sdk_test.
func captureValidatorLogs(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	h := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger.SetLogger(slog.New(h))
	return buf
}

// Regression coverage for https://github.com/AltairaLabs/PromptKit/issues/933.
//
// Prior to the fix, every turn of a conversation with a `max_length` validator
// declared (even one with a generous 2000-character cap) had its assistant
// content silently replaced by DefaultBlockedMessage. The root cause was a
// mismatch between the PromptPack spec's `params` field and the SDK's
// internal struct, which unmarshalled as nil and caused the validator to
// emit an "impossible" violation on every response.
//
// These tests exercise the full path — promptpack JSON on disk, sdk.Open,
// mock provider, Send — and assert the content is what we expect.

// buildTestPackWithMaxLength returns promptpack JSON with a max_length
// validator configured against the supplied parameters.
func buildTestPackWithMaxLength(t *testing.T, maxChars int, failOnViolation bool) []byte {
	t.Helper()
	failField := ""
	if failOnViolation {
		failField = `"fail_on_violation": true,`
	}
	packJSON := fmt.Sprintf(`{
  "id": "test-pack-933",
  "version": "1.0.0",
  "description": "issue #933 regression",
  "prompts": {
    "default": {
      "id": "default",
      "name": "Default",
      "system_template": "You are helpful.",
      "validators": [
        {
          "type": "max_length",
          "enabled": true,
          %s
          "params": {"max_characters": %d}
        }
      ]
    }
  }
}`, failField, maxChars)
	return []byte(packJSON)
}

// writeTestPack writes the given promptpack JSON to a temp file and returns
// its path.
func writeTestPack(t *testing.T, packJSON []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pack.json")
	require.NoError(t, os.WriteFile(path, packJSON, 0o600))
	return path
}

// openWithMockResponse opens a conversation against a promptpack file backed
// by a mock provider that returns the given canned response.
func openWithMockResponse(t *testing.T, packPath, cannedResponse string) *sdk.Conversation {
	t.Helper()
	repo := mock.NewInMemoryMockRepository(cannedResponse)
	provider := mock.NewProviderWithRepository("mock-test", "mock-model", false, repo)

	conv, err := sdk.Open(packPath, "default",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv
}

// TestIssue933_MaxLengthValidatorDoesNotBlockShortResponse is the direct
// regression for #933. A short assistant response must flow through a
// max_length validator with a generous cap untouched.
func TestIssue933_MaxLengthValidatorDoesNotBlockShortResponse(t *testing.T) {
	logs := captureValidatorLogs(t)

	const cannedResponse = "Hi there, this is a short reply." // 32 chars
	packPath := writeTestPack(t, buildTestPackWithMaxLength(t, 2000, true))
	conv := openWithMockResponse(t, packPath, cannedResponse)

	resp, err := conv.Send(context.Background(), "Hello")
	require.NoError(t, err)
	require.NotNil(t, resp)

	got := resp.Text()
	assert.Equal(t, cannedResponse, got,
		"short response should pass through untouched when max_characters=2000")
	assert.NotEqual(t, prompt.DefaultBlockedMessage, got,
		"response must not be replaced with DefaultBlockedMessage (issue #933)")
	assert.NotContains(t, got, "blocked",
		"response must not mention being blocked (issue #933 regression)")

	// Structural assertion: without this, the content-equality check above
	// is a tautology — if the validator hook is silently dropped by
	// convertPackValidatorsToHooks (e.g. because params unmarshal to nil
	// from a struct-tag regression), a short response still passes through
	// unchanged for the wrong reason. Pinning the absence of the skip
	// warning proves the hook was actually registered.
	assert.NotContains(t, logs.String(), "Skipping unusable pack validator",
		"validator must be registered, not silently skipped (regression for #933)")
}

// TestValidatorEnforcesOnRealViolation verifies that fail_on_violation:true
// still enforces when the response actually exceeds the limit. For
// max_length the enforcement action is truncation.
func TestValidatorEnforcesOnRealViolation(t *testing.T) {
	const cannedResponse = "This response is definitely longer than ten characters."
	const maxChars = 10
	packPath := writeTestPack(t, buildTestPackWithMaxLength(t, maxChars, true))
	conv := openWithMockResponse(t, packPath, cannedResponse)

	resp, err := conv.Send(context.Background(), "Hello")
	require.NoError(t, err)
	require.NotNil(t, resp)

	got := resp.Text()
	assert.NotEqual(t, cannedResponse, got,
		"enforcement should have modified the response")
	assert.LessOrEqual(t, len(got), maxChars,
		"content must be truncated to at most max_characters when fail_on_violation=true")
}

// TestValidatorMonitorOnlyDoesNotEnforce verifies the spec-default behaviour:
// with fail_on_violation absent, the validator logs violations but leaves
// content untouched.
func TestValidatorMonitorOnlyDoesNotEnforce(t *testing.T) {
	const cannedResponse = "This response is definitely longer than ten characters."
	packPath := writeTestPack(t, buildTestPackWithMaxLength(t, 10, false))
	conv := openWithMockResponse(t, packPath, cannedResponse)

	resp, err := conv.Send(context.Background(), "Hello")
	require.NoError(t, err)
	require.NotNil(t, resp)

	got := resp.Text()
	assert.Equal(t, cannedResponse, got,
		"monitor-only mode must not modify content even on violation")
}
