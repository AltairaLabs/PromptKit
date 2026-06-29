//go:build windows

package audio

import "syscall"

// openSharedLib loads a DLL by name and returns its module handle. purego's
// POSIX Dlopen/RTLD_* are not available on Windows, so we use LoadLibrary; the
// returned HMODULE works directly with purego.RegisterLibFunc.
func openSharedLib(name string) (uintptr, error) {
	h, err := syscall.LoadLibrary(name)
	if err != nil {
		return 0, err
	}
	return uintptr(h), nil
}
