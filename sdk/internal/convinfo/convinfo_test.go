package convinfo

import "testing"

func TestRewritesToolResults_DefaultsToFalse(t *testing.T) {
	if RewritesToolResults(struct{}{}) {
		t.Fatal("a value that is not a conversation does not rewrite tool results")
	}
}
