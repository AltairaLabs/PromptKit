#!/usr/bin/env bash
#
# Guards issue #1536: the pure-Go library foundation must NOT transitively import
# the PortAudio device binding (github.com/ebitengine/purego). purego uses
# //go:cgo_import_dynamic directives that force the whole binary to be dynamically
# linked against glibc even at CGO_ENABLED=0, which breaks server/controller images
# built on gcr.io/distroless/static or scratch (they have no dynamic loader).
#
# The device I/O lives in tools/arena/voice/portaudio (and, for demos, in
# sdk/examples/audiohelper). Only binaries that genuinely open a sound card may
# pull purego. Every foundational package below must stay purego-free.
#
# Run from the repo root: scripts/check-no-audio-device-leak.sh
set -euo pipefail

# Package import paths that ship into pure-Go server/controller binaries.
PKGS=(
	./runtime/providers/base
	./pkg/config
	./sdk
)

fail=0
for pkg in "${PKGS[@]}"; do
	if go list -deps "$pkg" 2>/dev/null | grep -q 'github.com/ebitengine/purego'; then
		echo "FAIL: $pkg transitively imports github.com/ebitengine/purego (issue #1536)"
		fail=1
	else
		echo "OK:   $pkg is purego-free"
	fi
done

if [ "$fail" -ne 0 ]; then
	echo ""
	echo "A pure-Go package now drags in the PortAudio device binding. Keep audio.Chunk"
	echo "and the DSP core in runtime/audio purego-free; device I/O belongs in"
	echo "tools/arena/voice/portaudio (or sdk/examples/audiohelper for demos)."
	exit 1
fi

echo "All foundational packages are statically linkable (no purego)."
