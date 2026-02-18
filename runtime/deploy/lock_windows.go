//go:build windows

package deploy

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// lockFileExclusive acquires an exclusive non-blocking lock using LockFileEx.
func lockFileExclusive(f *os.File) error {
	h := windows.Handle(f.Fd())
	ol := new(windows.Overlapped)
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)
	err := windows.LockFileEx(h, flags, 0, 1, 0, ol)
	if err != nil {
		if err == windows.ERROR_LOCK_VIOLATION {
			return fmt.Errorf(
				"deploy lock is held by another process; "+
					"wait for the other deploy to finish or remove the lock file")
		}
		return fmt.Errorf("failed to acquire deploy lock: %w", err)
	}
	return nil
}

// unlockFile releases the lock using UnlockFileEx.
func unlockFile(f *os.File) error {
	h := windows.Handle(f.Fd())
	ol := new(windows.Overlapped)
	return windows.UnlockFileEx(h, 0, 1, 0, ol)
}
