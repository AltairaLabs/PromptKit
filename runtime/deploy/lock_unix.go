//go:build !windows

package deploy

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// lockFileExclusive acquires an exclusive non-blocking lock using flock(2).
func lockFileExclusive(f *os.File) error {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return fmt.Errorf(
				"deploy lock is held by another process; " +
					"wait for the other deploy to finish or remove the lock file")
		}
		return fmt.Errorf("failed to acquire deploy lock: %w", err)
	}
	return nil
}

// unlockFile releases the flock.
func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
