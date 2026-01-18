//go:build e2e

package sdk

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Document E2E Tests
//
// These tests verify document (PDF) attachment functionality across providers.
// They test single documents, multiple documents, mixed media (doc+image),
// streaming with documents, and provider capabilities.
//
// Run with: go test -tags=e2e ./sdk/... -run TestE2E_Document
// =============================================================================

// createTestPDF creates a minimal valid PDF for testing
func createTestPDF(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	pdfPath := filepath.Join(tmpDir, "test.pdf")

	// Minimal valid PDF content
	pdfContent := `%PDF-1.4
1 0 obj
<< /Type /Catalog /Pages 2 0 R >>
endobj
2 0 obj
<< /Type /Pages /Kids [3 0 R] /Count 1 >>
endobj
3 0 obj
<< /Type /Page /Parent 2 0 R /Resources 4 0 R /MediaBox [0 0 612 792] /Contents 5 0 R >>
endobj
4 0 obj
<< /Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >> >>
endobj
5 0 obj
<< /Length 44 >>
stream
BT
/F1 12 Tf
100 700 Td
(Test PDF) Tj
ET
endstream
endobj
xref
0 6
0000000000 65535 f
0000000009 00000 n
0000000058 00000 n
0000000115 00000 n
0000000229 00000 n
0000000327 00000 n
trailer
<< /Size 6 /Root 1 0 R >>
startxref
420
%%EOF`

	err := os.WriteFile(pdfPath, []byte(pdfContent), 0644)
	require.NoError(t, err)

	return pdfPath
}

// TestE2E_Document_SingleFile tests basic single document attachment.
func TestE2E_Document_SingleFile(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapVision, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" || provider.ID == "openai" {
			t.Skip("Skipping mock and openai for document tests")
		}

		conv := NewVisionConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

		pdfPath := createTestPDF(t)

		resp, err := conv.Send(ctx,
			"What does this PDF contain? Give a very brief answer.",
			WithDocumentFile(pdfPath))
		require.NoError(t, err)
		assert.NotEmpty(t, resp.Text())
		t.Logf("%s document response: %s", provider.ID, resp.Text())
	})
}

// TestE2E_Document_Data tests document attachment from in-memory data.
func TestE2E_Document_Data(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapVision, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" || provider.ID == "openai" {
			t.Skip("Skipping mock and openai for document tests")
		}

		conv := NewVisionConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

		pdfPath := createTestPDF(t)
		pdfData, err := os.ReadFile(pdfPath)
		require.NoError(t, err)

		resp, err := conv.Send(ctx,
			"What does this PDF contain? Give a very brief answer.",
			WithDocumentData(pdfData, "application/pdf"))
		require.NoError(t, err)
		assert.NotEmpty(t, resp.Text())
		t.Logf("%s document data response: %s", provider.ID, resp.Text())
	})
}

// TestE2E_Document_MultipleDocuments tests sending multiple PDFs in one message.
func TestE2E_Document_MultipleDocuments(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapVision, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" || provider.ID == "openai" {
			t.Skip("Skipping mock and openai for document tests")
		}

		conv := NewVisionConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

		pdf1 := createTestPDF(t)
		pdf2 := createTestPDF(t)

		resp, err := conv.Send(ctx,
			"I'm sending you two PDFs. Just confirm you received them.",
			WithDocumentFile(pdf1),
			WithDocumentFile(pdf2))
		require.NoError(t, err)
		assert.NotEmpty(t, resp.Text())
		t.Logf("%s multiple documents response: %s", provider.ID, resp.Text())
	})
}

// TestE2E_Document_MixedMedia tests combining document with image.
func TestE2E_Document_MixedMedia(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapVision, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" || provider.ID == "openai" {
			t.Skip("Skipping mock and openai for document tests")
		}

		conv := NewVisionConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

		pdfPath := createTestPDF(t)

		// Create a simple test image (blue pixel) - use existing function from e2e_multimodal_test.go
		imageData := createColoredPNG(0, 0, 255)

		resp, err := conv.Send(ctx,
			"I'm sending you a PDF and an image. Just confirm you received both.",
			WithDocumentFile(pdfPath),
			WithImageData(imageData, "image/png"))
		require.NoError(t, err)
		assert.NotEmpty(t, resp.Text())
		t.Logf("%s mixed media response: %s", provider.ID, resp.Text())
	})
}

// TestE2E_Document_Streaming tests streaming responses with document attachment.
func TestE2E_Document_Streaming(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapStreaming, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" || provider.ID == "openai" {
			t.Skip("Skipping mock and openai for document tests")
		}

		conv := NewVisionConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

		pdfPath := createTestPDF(t)

		stream := conv.Stream(ctx,
			"What does this PDF contain? Give a very brief answer.",
			WithDocumentFile(pdfPath))

		var fullText string
		for chunk := range stream {
			if chunk.Error != nil {
				require.NoError(t, chunk.Error)
			}
			fullText += chunk.Text
		}

		assert.NotEmpty(t, fullText)
		t.Logf("%s streaming with document: %s", provider.ID, fullText)
	})
}
