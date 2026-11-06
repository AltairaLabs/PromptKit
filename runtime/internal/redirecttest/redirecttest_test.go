package redirecttest

import (
	"testing"

	provbase "github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// The check passes for a guarded copy and sees the leak on an unguarded
// client, so it can fail.
func TestSendThroughCrossHostRedirect_SeesTheLeak(t *testing.T) {
	caller, hops := CallerClient()
	_, atTarget := SendThroughCrossHostRedirect(t, caller)
	if atTarget.Get(ProbeHeader) == "" {
		t.Fatal("an unguarded client should carry the header to the other host")
	}
	if hops.Load() != 1 {
		t.Fatalf("hops = %d, want 1", hops.Load())
	}
	fresh, freshHops := CallerClient()
	r := Probe(t, provbase.WithCredentialRedirectPolicy(fresh), fresh, freshHops)
	if r.AtOrigin != "secret" || r.AtTarget != "" || r.CallerHops == 0 || r.CallerDirect != "secret" {
		t.Fatalf("guarded copy: %+v", r)
	}
}
