package sdk

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// applyStorageRefParts runs the given send-options through a sendConfig and
// materializes their parts onto a fresh user message via addContentParts.
func applyStorageRefParts(t *testing.T, opts ...SendOption) *types.Message {
	t.Helper()
	cfg := &sendConfig{}
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			t.Fatalf("apply option: %v", err)
		}
	}
	msg := &types.Message{Role: "user"}
	if err := (&Conversation{}).addContentParts(msg, cfg.parts); err != nil {
		t.Fatalf("addContentParts: %v", err)
	}
	return msg
}

func firstMedia(t *testing.T, msg *types.Message, wantType string) *types.MediaContent {
	t.Helper()
	if len(msg.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(msg.Parts))
	}
	p := msg.Parts[0]
	if p.Type != wantType {
		t.Fatalf("part type = %q, want %q", p.Type, wantType)
	}
	if p.Media == nil {
		t.Fatal("part has nil Media")
	}
	return p.Media
}

func assertStorageRef(t *testing.T, m *types.MediaContent, wantRef, wantMIME string) {
	t.Helper()
	if m.StorageReference == nil || *m.StorageReference != wantRef {
		t.Fatalf("StorageReference = %v, want %q", m.StorageReference, wantRef)
	}
	if m.MIMEType != wantMIME {
		t.Fatalf("MIMEType = %q, want %q", m.MIMEType, wantMIME)
	}
}

func TestWithImageStorageRef(t *testing.T) {
	detail := "high"
	msg := applyStorageRefParts(t, WithImageStorageRef("s3://bucket/img", "image/png", &detail))
	m := firstMedia(t, msg, types.ContentTypeImage)
	assertStorageRef(t, m, "s3://bucket/img", "image/png")
	if m.Detail == nil || *m.Detail != "high" {
		t.Fatalf("Detail = %v, want %q", m.Detail, "high")
	}
}

func TestWithImageStorageRef_NoDetail(t *testing.T) {
	msg := applyStorageRefParts(t, WithImageStorageRef("s3://bucket/img", "image/jpeg"))
	m := firstMedia(t, msg, types.ContentTypeImage)
	assertStorageRef(t, m, "s3://bucket/img", "image/jpeg")
	if m.Detail != nil {
		t.Fatalf("Detail = %v, want nil", m.Detail)
	}
}

func TestWithAudioStorageRef(t *testing.T) {
	msg := applyStorageRefParts(t, WithAudioStorageRef("s3://bucket/aud", "audio/mp3"))
	m := firstMedia(t, msg, types.ContentTypeAudio)
	assertStorageRef(t, m, "s3://bucket/aud", "audio/mp3")
}

func TestWithVideoStorageRef(t *testing.T) {
	msg := applyStorageRefParts(t, WithVideoStorageRef("s3://bucket/vid", "video/mp4"))
	m := firstMedia(t, msg, types.ContentTypeVideo)
	assertStorageRef(t, m, "s3://bucket/vid", "video/mp4")
}

func TestWithFileStorageRef(t *testing.T) {
	msg := applyStorageRefParts(t, WithFileStorageRef("contract.pdf", "s3://bucket/doc", types.MIMETypePDF))
	m := firstMedia(t, msg, types.ContentTypeDocument)
	assertStorageRef(t, m, "s3://bucket/doc", types.MIMETypePDF)
	if m.Caption == nil || *m.Caption != "contract.pdf" {
		t.Fatalf("Caption = %v, want %q", m.Caption, "contract.pdf")
	}
}
