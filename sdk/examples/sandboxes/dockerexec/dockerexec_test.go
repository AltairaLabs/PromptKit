package dockerexec

import (
	"reflect"
	"testing"
)

func TestBuildArgs_MinimalConfig(t *testing.T) {
	sb := New(Config{Container: "my-sidecar"})
	got := sb.buildArgs("/hooks/pii.py", []string{"--strict"})
	want := []string{
		"exec", "-i",
		"my-sidecar",
		"/hooks/pii.py", "--strict",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildArgs = %v, want %v", got, want)
	}
}

func TestBuildArgs_WithWorkdirAndUser(t *testing.T) {
	sb := New(Config{
		Container: "my-sidecar",
		Workdir:   "/app",
		User:      "hookuser",
	})
	got := sb.buildArgs("cmd", nil)
	want := []string{
		"exec", "-i",
		"--workdir=/app",
		"--user=hookuser",
		"my-sidecar", "cmd",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildArgs mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestBuildArgs_ExtraArgs(t *testing.T) {
	sb := New(Config{
		Container: "c",
		ExtraArgs: []string{"--env=FOO=bar"},
	})
	got := sb.buildArgs("cmd", []string{"arg"})
	want := []string{
		"exec", "-i",
		"--env=FOO=bar",
		"c", "cmd", "arg",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildArgs mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestFactory_RequiresContainer(t *testing.T) {
	if _, err := Factory("m", nil); err == nil {
		t.Fatal("Factory with no container should error")
	}
	if _, err := Factory("m", map[string]any{"workdir": "/app"}); err == nil {
		t.Fatal("Factory with only workdir should error")
	}
}

func TestFactory_ParsesConfig(t *testing.T) {
	sb, err := Factory("my_sidecar", map[string]any{
		"container":  "my-sidecar",
		"workdir":    "/app",
		"user":       "hookuser",
		"extra_args": []any{"--env=FOO=bar"},
	})
	if err != nil {
		t.Fatalf("Factory: %v", err)
	}
	if sb.Name() != "my_sidecar" {
		t.Errorf("Name = %q", sb.Name())
	}
	got := sb.(*Sandbox).buildArgs("cmd", nil)
	want := []string{
		"exec", "-i",
		"--workdir=/app",
		"--user=hookuser",
		"--env=FOO=bar",
		"my-sidecar", "cmd",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildArgs mismatch\n got: %v\nwant: %v", got, want)
	}
}
