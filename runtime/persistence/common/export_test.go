package common

import "testing"

// SetBeforeRootedRead runs fn inside each fragment read, after the
// lookup has resolved where it may read and before it reads, until t ends. It
// lets the external tests, which drive the real yaml and json repositories,
// change the files in that window.
func SetBeforeRootedRead(t *testing.T, fn func()) {
	t.Helper()
	beforeRootedRead = fn
	t.Cleanup(func() { beforeRootedRead = nil })
}
