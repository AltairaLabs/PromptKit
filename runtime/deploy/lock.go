package deploy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const lockFile = "deploy.lock"

// Locker provides file-based locking for deploy operations.
// It uses syscall.Flock to prevent concurrent deploys from
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
	return filepath.Join(l.baseDir, stateDir, lockFile)
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

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return fmt.Errorf(
				"deploy lock is held by another process; "+
					"wait for the other deploy to finish or remove %s", l.lockPath())
		}
		return fmt.Errorf("failed to acquire deploy lock: %w", err)
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

	fd := int(l.file.Fd())

	if err := syscall.Flock(fd, syscall.LOCK_UN); err != nil {
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
