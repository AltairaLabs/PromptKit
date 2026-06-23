package voice

import "testing"

func TestEchoGuard_AllowsWhenAgentSilent(t *testing.T) {
	g := NewEchoGuard(0.5)
	if !g.Allow(make([]byte, 64)) {
		t.Fatal("expected mic allowed when agent not speaking")
	}
}

func TestEchoGuard_GatesQuietMicWhileAgentSpeaks(t *testing.T) {
	g := NewEchoGuard(0.5)
	g.SetAgentSpeaking(true)
	if g.Allow(make([]byte, 64)) { // silence < threshold
		t.Fatal("expected quiet mic gated while agent speaks")
	}
}

func TestEchoGuard_AllowsLoudBargeInWhileAgentSpeaks(t *testing.T) {
	g := NewEchoGuard(0.001) // very low threshold
	g.SetAgentSpeaking(true)
	loud := []byte{0xff, 0x7f, 0xff, 0x7f} // near max amplitude
	if !g.Allow(loud) {
		t.Fatal("expected loud barge-in to pass the gate")
	}
}
