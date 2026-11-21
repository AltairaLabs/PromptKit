package a2aserver

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
)

// writeCacheable writes a JSON body with the caching headers A2A 1.0 §8.6 asks
// of an agent card endpoint: Cache-Control with maxAge, a strong ETag hashed
// from the body, and Vary on the version header, which picks the card's shape.
// A request whose If-None-Match matches the tag gets 304 Not Modified and no
// body.
func writeCacheable(w http.ResponseWriter, r *http.Request, body []byte, maxAge time.Duration) {
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:]) + `"`

	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Vary", a2a.HeaderVersion)
	if maxAge > 0 {
		h.Set("Cache-Control", "max-age="+strconv.FormatInt(int64(maxAge/time.Second), 10))
	} else {
		h.Set("Cache-Control", "no-cache")
	}

	if etagListMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// etagListMatches reports whether an If-None-Match value names etag, using the
// weak comparison RFC 9110 §13.1.2 prescribes for it: "*" matches any tag, and
// a W/ prefix on a candidate is ignored. etag is the server's strong tag.
func etagListMatches(header, etag string) bool {
	header = strings.TrimSpace(header)
	if header == "*" {
		return true
	}
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "W/")
		if candidate == etag {
			return true
		}
	}
	return false
}
