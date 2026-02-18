package deploy

import (
	"fmt"
	"os"
	"path/filepath"
)

const lockFileName = "deploy.lock"

// Locker provides file-based locking for deploy operations.
// It uses OS-level file locking to prevent concurrent deploys from
// different processes targeting the same project directory.
type Locker struct {
	baseDir string
	file    *os.File
}

// NewLocker creates a Locker for the given project directory.
func NewLocker(baseDir string) *Locker {
	return &Locker{baseDir: baseDir}
}

// lockPath returns the full path to the lock file.
func (l *Locker) lockPath() string {
	return filepath.Join(l.baseDir, stateDir, lockFileName)
}

// Lock acquires an exclusive file lock. Returns an error if the lock
// is already held by another process or if the locker already holds a lock.
func (l *Locker) Lock() error {
	if l.file != nil {
		return fmt.Errorf("locker already holds a lock")
	}

	dir := filepath.Join(l.baseDir, stateDir)
	if err := os.MkdirAll(dir, stateDirPerm); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	f, err := os.OpenFile(l.lockPath(), os.O_CREATE|os.O_RDWR, stateFilePerm)
	if err != nil {
		return fmt.Errorf("failed to open lock file: %w", err)
	}

	if err := lockFileExclusive(f); err != nil {
		_ = f.Close()
		return err
	}

	l.file = f
	return nil
}

// Unlock releases the lock and removes the lock file.
// If no lock is held, Unlock is a no-op and returns nil.
func (l *Locker) Unlock() error {
	if l.file == nil {
		return nil
	}

	if err := unlockFile(l.file); err != nil {
		return fmt.Errorf("failed to release deploy lock: %w", err)
	}

	if err := l.file.Close(); err != nil {
		return fmt.Errorf("failed to close lock file: %w", err)
	}

	// Best-effort removal of the lock file.
	_ = os.Remove(l.lockPath())

	l.file = nil
	return nil
}
