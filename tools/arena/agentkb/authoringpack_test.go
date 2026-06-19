package agentkb

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// committed pack path, relative to this package dir (tools/arena/agentkb).
const authoringPackPath = "../../../examples/test-a-codegen-agent/packs/authoring-agent.yaml"

func TestAuthoringPackYAML_EmbedsBrief(t *testing.T) {
	got := AuthoringPackYAML()
	// The brief is embedded indented under system_template: |, so search for the
	// indented form of its first non-empty line (4 spaces + brief prefix).
	briefPrefix := "    " + strings.SplitN(string(AgentsBrief()), "\n", 2)[0]
	if !bytes.Contains(got, []byte(briefPrefix)) {
		t.Fatalf("AuthoringPackYAML does not contain the indented brief prefix %q", briefPrefix)
	}
	if !bytes.Contains(got, []byte("kind: PromptConfig")) {
		t.Fatalf("AuthoringPackYAML missing kind: PromptConfig")
	}
}

func TestAuthoringPack_CommittedFileInSync(t *testing.T) {
	want := AuthoringPackYAML()
	have, err := os.ReadFile(authoringPackPath)
	if err != nil {
		t.Fatalf("read committed pack: %v (run `go generate ./tools/arena/agentkb/...`)", err)
	}
	if !bytes.Equal(bytes.TrimRight(have, "\n"), bytes.TrimRight(want, "\n")) {
		t.Fatalf("committed authoring-agent.yaml is stale; run `go generate ./tools/arena/agentkb/...`")
	}
}
