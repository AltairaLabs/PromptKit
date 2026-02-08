package sdk

import (
	"testing"
)

func TestA2AOpener(t *testing.T) {
	// A2AOpener returns an A2AConversationOpener function. We can verify the
	// function signature is correct by assigning it to the interface type.
	// Actually calling it would require a valid pack file, so we just
	// verify the type contract.
	var opener A2AConversationOpener = A2AOpener("nonexistent.pack.json", "prompt")
	_, err := opener("ctx-1")
	if err == nil {
		t.Fatal("expected error for nonexistent pack file")
	}
}
