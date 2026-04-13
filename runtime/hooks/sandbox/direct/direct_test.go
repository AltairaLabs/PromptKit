package direct

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/hooks/sandbox"
)

func TestDirect_Name(t *testing.T) {
	if got := New("").Name(); got != ModeName {
		t.Errorf("Name() with empty input = %q, want %q", got, ModeName)
	}
	if got := New("custom").Name(); got != "custom" {
		t.Errorf("Name() = %q, want %q", got, "custom")
	}
}

func TestDirect_SpawnEchoesStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cat isn't portable on Windows")
	}
	sb := New("")
	resp, err := sb.Spawn(context.Background(), sandbox.Request{
		Command: "cat",
		Stdin:   []byte(`{"hello":"world"}`),
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if resp.Err != nil {
		t.Fatalf("Response.Err = %v", resp.Err)
	}
	if got := string(resp.Stdout); got != `{"hello":"world"}` {
		t.Errorf("Stdout = %q, want echo of stdin", got)
	}
}

func TestDirect_SpawnNonZeroExitIsReported(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("false isn't portable on Windows")
	}
	sb := New("")
	resp, err := sb.Spawn(context.Background(), sandbox.Request{
		Command: "false",
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if resp.Err == nil {
		t.Fatal("expected Response.Err to be non-nil on non-zero exit")
	}
}

func TestDirect_SpawnTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sleep isn't portable on Windows")
	}
	sb := New("")
	start := time.Now()
	resp, err := sb.Spawn(context.Background(), sandbox.Request{
		Command: "sleep",
		Args:    []string{"5"},
		Timeout: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if resp.Err == nil {
		t.Fatal("expected Response.Err on timeout")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Spawn blocked for %v, timeout should have fired within ~100ms", elapsed)
	}
}

// TestDirect_EnvForwarding verifies both supported Env forms round-trip:
//   - "KEY=value" is forwarded verbatim
//   - "KEY" looks up the host value and forwards "KEY=<value>"
func TestDirect_EnvForwarding(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh isn't portable on Windows")
	}
	t.Setenv("PK_SB_HOST_VAR", "from_host")

	sb := New("")
	resp, err := sb.Spawn(context.Background(), sandbox.Request{
		Command: "sh",
		Args:    []string{"-c", `echo "$PK_SB_LITERAL:$PK_SB_HOST_VAR"`},
		Env:     []string{"PK_SB_LITERAL=literal", "PK_SB_HOST_VAR"},
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if resp.Err != nil {
		t.Fatalf("Response.Err = %v", resp.Err)
	}
	got := strings.TrimSpace(string(resp.Stdout))
	if got != "literal:from_host" {
		t.Errorf("env round-trip: got %q, want %q", got, "literal:from_host")
	}
}

// TestDirect_RegisteredAsDefault verifies the package-init side-effect
// registers the direct factory under ModeName.
func TestDirect_RegisteredAsDefault(t *testing.T) {
	factory, err := sandbox.LookupFactory(ModeName)
	if err != nil {
		t.Fatalf("LookupFactory: %v", err)
	}
	sb, err := factory("default", nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if sb == nil {
		t.Fatal("factory returned nil sandbox")
	}
}
