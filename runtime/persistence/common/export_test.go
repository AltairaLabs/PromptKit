package common

import "testing"

// SetAfterContainmentCheck runs fn inside each fragment read, after the
// lookup has resolved where it may read and before it reads, until t ends. It
// lets the external tests, which drive the real yaml and json repositories,
// change the files in that window.
func SetAfterContainmentCheck(t *testing.T, fn func()) {
	t.Helper()
	afterContainmentCheck = fn
	t.Cleanup(func() { afterContainmentCheck = nil })
}
