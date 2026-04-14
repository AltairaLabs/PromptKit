package kubectlexec

import (
	"reflect"
	"testing"
)

func TestBuildArgs_MinimalConfig(t *testing.T) {
	sb := New(Config{Pod: "my-pod"})
	got := sb.buildArgs("/hooks/pii.py", []string{"--strict"})
	want := []string{
		"exec", "-i",
		"my-pod", "--",
		"/hooks/pii.py", "--strict",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildArgs = %v, want %v", got, want)
	}
}

func TestBuildArgs_FullConfig(t *testing.T) {
	sb := New(Config{
		Pod:        "my-pod",
		Namespace:  "default",
		Container:  "hooks",
		Kubeconfig: "/etc/kube.yaml",
		Context:    "prod",
		ExtraArgs:  []string{"--request-timeout=5s"},
	})
	got := sb.buildArgs("python", []string{"-m", "pii"})
	want := []string{
		"--kubeconfig=/etc/kube.yaml",
		"--context=prod",
		"exec", "-i",
		"-n", "default",
		"-c", "hooks",
		"--request-timeout=5s",
		"my-pod", "--",
		"python", "-m", "pii",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildArgs mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestFactory_RequiresPod(t *testing.T) {
	if _, err := Factory("m", nil); err == nil {
		t.Fatal("Factory with no pod should error")
	}
	if _, err := Factory("m", map[string]any{"namespace": "default"}); err == nil {
		t.Fatal("Factory with only namespace should error")
	}
}

func TestFactory_ParsesConfig(t *testing.T) {
	sb, err := Factory("my_sidecar", map[string]any{
		"pod":        "my-pod",
		"namespace":  "default",
		"container":  "hooks",
		"extra_args": []any{"--request-timeout=5s"},
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
		"-n", "default",
		"-c", "hooks",
		"--request-timeout=5s",
		"my-pod", "--",
		"cmd",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildArgs mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestName_Defaults(t *testing.T) {
	if got := New(Config{Pod: "x"}).Name(); got != ModeName {
		t.Errorf("Name() = %q, want %q", got, ModeName)
	}
}
