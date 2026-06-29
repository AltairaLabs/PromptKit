//go:build !windows

package audio

import "github.com/ebitengine/purego"

// openSharedLib loads a shared library by name via the POSIX dynamic loader.
// purego's Dlopen/RTLD_* are only defined on non-Windows platforms.
func openSharedLib(name string) (uintptr, error) {
	return purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
}
