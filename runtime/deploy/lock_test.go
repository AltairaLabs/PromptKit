package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocker_LockAndUnlock(t *testing.T) {
	dir := t.TempDir()
	l := NewLocker(dir)

	if err := l.Lock(); err != nil {
		t.Fatalf("Lock() failed: %v", err)
	}

	// Lock file should exist.
	if _, err := os.Stat(l.lockPath()); err != nil {
		t.Fatalf("lock file does not exist after Lock(): %v", err)
	}

	if err := l.Unlock(); err != nil {
		t.Fatalf("Unlock() failed: %v", err)
	}

	// Lock file should be removed after unlock.
	if _, err := os.Stat(l.lockPath()); !os.IsNotExist(err) {
		t.Fatalf("lock file still exists after Unlock()")
	}
}

func TestLocker_DoubleLock(t *testing.T) {
	dir := t.TempDir()
	l := NewLocker(dir)

	if err := l.Lock(); err != nil {
		t.Fatalf("first Lock() failed: %v", err)
	}
	defer func() { _ = l.Unlock() }()

	err := l.Lock()
	if err == nil {
		t.Fatal("second Lock() should have returned an error")
	}
	if !strings.Contains(err.Error(), "already holds") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocker_ConcurrentLock(t *testing.T) {
	dir := t.TempDir()

	l1 := NewLocker(dir)
	l2 := NewLocker(dir)

	if err := l1.Lock(); err != nil {
		t.Fatalf("l1.Lock() failed: %v", err)
	}
	defer func() { _ = l1.Unlock() }()

	err := l2.Lock()
	if err == nil {
		_ = l2.Unlock()
		t.Fatal("l2.Lock() should have returned an error while l1 holds the lock")
	}
	if !strings.Contains(err.Error(), "held by another process") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocker_UnlockWithoutLock(t *testing.T) {
	dir := t.TempDir()
	l := NewLocker(dir)

	if err := l.Unlock(); err != nil {
		t.Fatalf("Unlock() without Lock() should be a no-op, got: %v", err)
	}
}

func TestLocker_LockCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".promptarena")

	// Ensure the state directory does not exist yet.
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatalf(".promptarena dir should not exist yet")
	}

	l := NewLocker(dir)
	if err := l.Lock(); err != nil {
		t.Fatalf("Lock() failed: %v", err)
	}
	defer func() { _ = l.Unlock() }()

	info, err := os.Stat(stateDir)
	if err != nil {
		t.Fatalf(".promptarena dir was not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal(".promptarena should be a directory")
	}
}
