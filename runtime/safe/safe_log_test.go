package safe

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

// captureLog routes the package logger to a buffer for the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	t.Cleanup(func() { logger.SetOutput(nil) })
	return &buf
}

func assertPanicLogged(t *testing.T, out, name, value string) {
	t.Helper()
	for _, want := range []string{"recovered panic in goroutine", "name=" + name, "panic=" + value, "stack="} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q:\n%s", want, out)
		}
	}
}

func TestRun_NilOnPanicStillRecoversAndLogs(t *testing.T) {
	buf := captureLog(t)
	returned := false
	func() {
		Run("worker-a", func() { panic(errors.New("x")) }, nil)
		returned = true
	}()
	if !returned {
		t.Fatal("Run did not return after a panic")
	}
	assertPanicLogged(t, buf.String(), "worker-a", "x")
}

func TestRecover_DeferredRecoversAndLogs(t *testing.T) {
	buf := captureLog(t)
	func() {
		defer Recover("worker-b")
		panic("boom")
	}()
	// Reaching here means the panic did not escape.
	assertPanicLogged(t, buf.String(), "worker-b", "boom")
}

func TestRun_NoPanicLogsNothing(t *testing.T) {
	buf := captureLog(t)
	Run("quiet", func() {}, func(any) { t.Error("onPanic called without a panic") })
	if buf.Len() != 0 {
		t.Errorf("logged without a panic: %s", buf.String())
	}
}
