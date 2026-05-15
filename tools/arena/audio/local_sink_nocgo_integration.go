//go:build !cgo

package audio

import "errors"

// realOtoContext returns an error indicating that local-sink playback is
// unavailable in this build. github.com/ebitengine/oto/v3 requires CGO on
// Linux (driver_unix.go imports "C"); cross-compiled binaries built with
// CGO_ENABLED=0 (GoReleaser, CI release pipeline) substitute this stub.
// NewLocalSink converts the error into noop mode so duplex scenarios still
// run end-to-end — only host-speaker playback is unavailable, matching the
// behavior users get when audio_files is absent.
func realOtoContext(_, _ int) (otoContext, error) {
	return nil, errors.New("local audio sink unavailable (built without CGO)")
}
