package pack

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

func TestSpecToolPolicyKeys_FollowThePackspecType(t *testing.T) {
	assert.Equal(t, map[string]bool{
		"blocklist": true, "max_rounds": true, "max_tool_calls_per_turn": true, "tool_choice": true,
	}, specToolPolicyKeys)
}

func TestWarnDroppedToolPolicyKeys_IgnoresUnparseableAndCleanPacks(t *testing.T) {
	var buf bytes.Buffer
	logger.SetLogger(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { logger.SetLogger(nil) })

	warnDroppedToolPolicyKeys("/p1", []byte(`{not json`))
	warnDroppedToolPolicyKeys("/p1", []byte(`{"prompts": {"a": {"tool_policy": {"max_rounds": 2}}, "b": {}}}`))
	assert.Empty(t, buf.String())

	dropped := []byte(`{"prompts": {"b": {"tool_policy": {"x": 1}}, "a": {"tool_policy": {"y": 1}}}}`)
	warnDroppedToolPolicyKeys("/p1", dropped)
	out := buf.String()
	assert.Contains(t, out, `"prompt":"a","key":"y"`)
	assert.Contains(t, out, `"prompt":"b","key":"x"`)
	assert.Less(t, bytes.Index(buf.Bytes(), []byte(`"key":"y"`)), bytes.Index(buf.Bytes(), []byte(`"key":"x"`)),
		"prompts are reported in name order")

	// A reload of the same pack (every workflow state opens it again) does
	// not repeat the warning; another pack with the same key gets its own.
	warnDroppedToolPolicyKeys("/p1", dropped)
	assert.Equal(t, out, buf.String(), "already reported for this pack")
	warnDroppedToolPolicyKeys("/p2", dropped)
	assert.Contains(t, strings.TrimPrefix(buf.String(), out), `"key":"x"`)
}
