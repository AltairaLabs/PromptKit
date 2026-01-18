package types

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewDocumentPart(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name         string
		fileName     string
		fileContent  []byte
		wantMIMEType string
		wantErr      bool
	}{
		{
			name:         "PDF file",
			fileName:     "test.pdf",
			fileContent:  []byte("%PDF-1.4\n..."),
			wantMIMEType: MIMETypePDF,
			wantErr:      false,
		},
		{
			name:         "Word docx file",
			fileName:     "test.docx",
			fileContent:  []byte("fake docx"),
			wantMIMEType: MIMETypeDocx,
			wantErr:      false,
		},
		{
			name:         "Markdown file",
			fileName:     "README.md",
			fileContent:  []byte("# Title\n\nContent"),
			wantMIMEType: MIMETypeMarkdown,
			wantErr:      false,
		},
		{
			name:         "Plain text file",
			fileName:     "notes.txt",
			fileContent:  []byte("Some notes"),
			wantMIMEType: MIMETypePlainText,
			wantErr:      false,
		},
		{
			name:         "CSV file",
			fileName:     "data.csv",
			fileContent:  []byte("a,b,c\n1,2,3"),
			wantMIMEType: MIMETypeCSV,
			wantErr:      false,
		},
		{
			name:        "Unsupported extension",
			fileName:    "test.xyz",
			fileContent: []byte("data"),
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filePath := filepath.Join(tmpDir, tt.fileName)
			err := os.WriteFile(filePath, tt.fileContent, 0644)
			if err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			part, err := NewDocumentPart(filePath)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if part.Type != ContentTypeDocument {
				t.Errorf("Part type = %s, want %s", part.Type, ContentTypeDocument)
			}

			if part.Media == nil {
				t.Fatal("Part media is nil")
			}

			if part.Media.MIMEType != tt.wantMIMEType {
				t.Errorf("MIME type = %s, want %s", part.Media.MIMEType, tt.wantMIMEType)
			}

			if part.Media.FilePath == nil || *part.Media.FilePath != filePath {
				t.Errorf("FilePath = %v, want %s", part.Media.FilePath, filePath)
			}
		})
	}
}

func TestNewDocumentPartFromData(t *testing.T) {
	base64Data := "JVBERi0xLjQKdGVzdCBjb250ZW50" // base64 encoded

	part := NewDocumentPartFromData(base64Data, MIMETypePDF)

	if part.Type != ContentTypeDocument {
		t.Errorf("Part type = %s, want %s", part.Type, ContentTypeDocument)
	}

	if part.Media == nil {
		t.Fatal("Part media is nil")
	}

	if part.Media.MIMEType != MIMETypePDF {
		t.Errorf("MIME type = %s, want %s", part.Media.MIMEType, MIMETypePDF)
	}

	if part.Media.Data == nil || *part.Media.Data != base64Data {
		t.Errorf("Data = %v, want %s", part.Media.Data, base64Data)
	}
}

func TestMessageAddDocumentPart(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.pdf")
	if err := os.WriteFile(tmpFile, []byte("%PDF-1.4\ntest"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	msg := Message{Role: "user"}
	err := msg.AddDocumentPart(tmpFile)
	if err != nil {
		t.Fatalf("AddDocumentPart() error = %v", err)
	}

	if len(msg.Parts) != 1 {
		t.Fatalf("Parts length = %d, want 1", len(msg.Parts))
	}

	if msg.Parts[0].Type != ContentTypeDocument {
		t.Errorf("Part type = %s, want %s", msg.Parts[0].Type, ContentTypeDocument)
	}

	if msg.Parts[0].Media == nil {
		t.Fatal("Part media is nil")
	}

	if msg.Parts[0].Media.MIMEType != MIMETypePDF {
		t.Errorf("MIME type = %s, want %s", msg.Parts[0].Media.MIMEType, MIMETypePDF)
	}
}

func TestContentPartValidateDocument(t *testing.T) {
	base64data := "base64data"
	validPart := ContentPart{
		Type: ContentTypeDocument,
		Media: &MediaContent{
			Data:     &base64data,
			MIMEType: MIMETypePDF,
		},
	}

	if err := validPart.Validate(); err != nil {
		t.Errorf("Valid document part failed validation: %v", err)
	}

	invalidPart := ContentPart{
		Type:  ContentTypeDocument,
		Media: nil,
	}

	if err := invalidPart.Validate(); err == nil {
		t.Error("Invalid document part passed validation")
	}
}

func TestInferMIMETypeDocuments(t *testing.T) {
	tests := []struct {
		path     string
		wantType string
		wantErr  bool
	}{
		{"/path/to/file.pdf", MIMETypePDF, false},
		{"/path/to/file.docx", MIMETypeDocx, false},
		{"/path/to/file.doc", MIMETypeDoc, false},
		{"/path/to/file.md", MIMETypeMarkdown, false},
		{"/path/to/file.markdown", MIMETypeMarkdown, false},
		{"/path/to/file.txt", MIMETypePlainText, false},
		{"/path/to/file.csv", MIMETypeCSV, false},
		{"/path/to/file.json", MIMETypeJSON, false},
		{"/path/to/file.xml", MIMETypeXML, false},
		{"/path/to/file.unknown", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, err := inferMIMEType(tt.path)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error for %s, got nil", tt.path)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error for %s: %v", tt.path, err)
				return
			}

			if got != tt.wantType {
				t.Errorf("inferMIMEType(%s) = %s, want %s", tt.path, got, tt.wantType)
			}
		})
	}
}

func TestHasMediaContentWithDocuments(t *testing.T) {
	// Message with only text
	msgText := Message{
		Role: "user",
		Parts: []ContentPart{
			NewTextPart("Hello"),
		},
	}
	if msgText.HasMediaContent() {
		t.Error("Text-only message should not have media content")
	}

	// Message with document
	msgDoc := Message{
		Role: "user",
		Parts: []ContentPart{
			NewDocumentPartFromData("data", MIMETypePDF),
		},
	}
	if !msgDoc.HasMediaContent() {
		t.Error("Message with document should have media content")
	}

	// Message with mixed content
	msgMixed := Message{
		Role: "user",
		Parts: []ContentPart{
			NewTextPart("Check this document:"),
			NewDocumentPartFromData("data", MIMETypePDF),
		},
	}
	if !msgMixed.HasMediaContent() {
		t.Error("Message with text and document should have media content")
	}
}
