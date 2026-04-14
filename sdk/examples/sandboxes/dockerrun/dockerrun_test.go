package dockerrun

import (
	"reflect"
	"testing"
)

func TestBuildArgs_MinimalConfig(t *testing.T) {
	sb := New(Config{Image: "python:3.12-slim"})
	got := sb.buildArgs("/hooks/pii.py", []string{"--strict"})
	want := []string{
		"run", "--rm", "-i",
		"python:3.12-slim",
		"/hooks/pii.py", "--strict",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildArgs = %v, want %v", got, want)
	}
}

func TestBuildArgs_WithNetworkAndMounts(t *testing.T) {
	sb := New(Config{
		Image:   "python:3.12-slim",
		Network: "none",
		Mounts:  []string{"./hooks:/hooks:ro", "./scratch:/scratch"},
	})
	got := sb.buildArgs("python", []string{"-m", "pii"})
	want := []string{
		"run", "--rm", "-i",
		"--network=none",
		"-v", "./hooks:/hooks:ro",
		"-v", "./scratch:/scratch",
		"python:3.12-slim",
		"python", "-m", "pii",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildArgs mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestBuildArgs_WithExtraArgs(t *testing.T) {
	sb := New(Config{
		Image:     "python:3.12-slim",
		ExtraArgs: []string{"--memory=256m", "--cpus=0.5"},
	})
	got := sb.buildArgs("cmd", nil)
	want := []string{
		"run", "--rm", "-i",
		"--memory=256m", "--cpus=0.5",
		"python:3.12-slim", "cmd",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildArgs = %v, want %v", got, want)
	}
}

func TestFactory_RequiresImage(t *testing.T) {
	_, err := Factory("m", nil)
	if err == nil {
		t.Fatal("Factory with no image should error")
	}
	_, err = Factory("m", map[string]any{"network": "none"})
	if err == nil {
		t.Fatal("Factory with no image should error even with other fields set")
	}
}

func TestFactory_ParsesConfig(t *testing.T) {
	sb, err := Factory("my_docker", map[string]any{
		"image":       "python:3.12-slim",
		"network":     "none",
		"mounts":      []any{"./a:/a", "./b:/b:ro"},
		"extra_args":  []string{"--memory=256m"},
		"docker_path": "/usr/local/bin/docker",
	})
	if err != nil {
		t.Fatalf("Factory: %v", err)
	}
	if sb.Name() != "my_docker" {
		t.Errorf("Name = %q, want %q", sb.Name(), "my_docker")
	}
	got := sb.(*Sandbox).buildArgs("cmd", nil)
	want := []string{
		"run", "--rm", "-i",
		"--network=none",
		"-v", "./a:/a",
		"-v", "./b:/b:ro",
		"--memory=256m",
		"python:3.12-slim", "cmd",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildArgs mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestName_Defaults(t *testing.T) {
	if got := New(Config{Image: "x"}).Name(); got != ModeName {
		t.Errorf("Name() = %q, want %q", got, ModeName)
	}
	if got := NewNamed("custom", Config{Image: "x"}).Name(); got != "custom" {
		t.Errorf("Name() = %q, want %q", got, "custom")
	}
}
