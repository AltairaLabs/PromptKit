package types

import "testing"

func TestMediaContent_Validate_AcceptsStorageReference(t *testing.T) {
	ref := "s3://bucket/key"
	mc := &MediaContent{StorageReference: &ref, MIMEType: "image/png"}
	if err := mc.Validate(); err != nil {
		t.Fatalf("storage reference should be a valid source, got: %v", err)
	}
}

func TestMediaContent_Validate_StillRejectsNoSource(t *testing.T) {
	mc := &MediaContent{MIMEType: "image/png"}
	if err := mc.Validate(); err == nil {
		t.Fatal("expected error when no data source is present")
	}
}

func TestNewImagePartFromStorageRef(t *testing.T) {
	detail := "high"
	cp := NewImagePartFromStorageRef("s3://b/k", "image/png", &detail)
	if cp.Type != ContentTypeImage {
		t.Fatalf("type = %s, want image", cp.Type)
	}
	if cp.Media == nil || cp.Media.StorageReference == nil || *cp.Media.StorageReference != "s3://b/k" {
		t.Fatal("storage reference not set")
	}
	if cp.Media.MIMEType != "image/png" {
		t.Fatalf("mime = %q", cp.Media.MIMEType)
	}
	if cp.Media.Detail == nil || *cp.Media.Detail != "high" {
		t.Fatal("detail not set")
	}
	if err := cp.Media.Validate(); err != nil {
		t.Fatalf("constructed part must validate: %v", err)
	}
}

func TestNewAudioVideoDocumentPartFromStorageRef(t *testing.T) {
	for _, tc := range []struct {
		name string
		cp   ContentPart
		want string
	}{
		{"audio", NewAudioPartFromStorageRef("s3://a", "audio/mp3"), ContentTypeAudio},
		{"video", NewVideoPartFromStorageRef("s3://v", "video/mp4"), ContentTypeVideo},
		{"document", NewDocumentPartFromStorageRef("s3://d", MIMETypePDF), ContentTypeDocument},
	} {
		if tc.cp.Type != tc.want {
			t.Errorf("%s: type = %s, want %s", tc.name, tc.cp.Type, tc.want)
		}
		if err := tc.cp.Media.Validate(); err != nil {
			t.Errorf("%s: must validate: %v", tc.name, err)
		}
	}
}
