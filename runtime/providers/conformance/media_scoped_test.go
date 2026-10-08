package conformance_test

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/storage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// oneFileStore resolves every storage reference to one image file.
type oneFileStore struct{ path string }

func (s oneFileStore) StoreMedia(context.Context, *types.MediaContent, *storage.MediaMetadata) (storage.Reference, error) {
	return "", errors.New("read-only")
}

func (s oneFileStore) RetrieveMedia(context.Context, storage.Reference) (*types.MediaContent, error) {
	return &types.MediaContent{FilePath: &s.path, MIMEType: "image/png"}, nil
}

func (s oneFileStore) DeleteMedia(context.Context, storage.Reference) error { return nil }

func (s oneFileStore) GetURL(context.Context, storage.Reference, time.Duration) (string, error) {
	return scopedMediaURL, nil
}

// scopedMediaURL is the URL oneFileStore gives for its file, which URL-first
// providers send in place of the bytes.
const scopedMediaURL = "https://media.example/scoped-ref"

// writeScopedImage writes the image oneFileStore serves, returning its path
// and its base64 form, which byte-based providers send.
func writeScopedImage(t *testing.T) (path, encoded string) {
	t.Helper()
	img := []byte("\x89PNG\r\n\x1a\nscoped-image")
	path = filepath.Join(t.TempDir(), "img.png")
	require.NoError(t, os.WriteFile(path, img, 0o600))
	return path, base64.StdEncoding.EncodeToString(img)
}

// A conversation's media settings live on its own view of a shared provider
// (#2216): the view resolves a storage reference through the conversation's
// store, and the shared provider, and every other conversation on it, does not
// see that store.
func TestProviders_MediaSettingsAreScopedToTheView(t *testing.T) {
	path, want := writeScopedImage(t)
	ref := "ref-1"
	text := "describe"
	req := providers.PredictionRequest{MaxTokens: 64, Messages: []types.Message{{Role: "user", Parts: []types.ContentPart{
		{Type: types.ContentTypeText, Text: &text},
		{Type: types.ContentTypeImage, Media: &types.MediaContent{StorageReference: &ref, MIMEType: "image/png"}},
	}}}}

	for _, spec := range []providers.ProviderSpec{
		{Type: "openai", Model: "gpt-6-luna", AdditionalConfig: map[string]any{"api_mode": "completions"}},
		{Type: "openai", Model: "gpt-6-luna", AdditionalConfig: map[string]any{"api_mode": "responses"}},
		{Type: "claude", Model: "claude-sonnet-5-5"},
		{Type: "gemini", Model: "gemini-3.8-flash"},
		{Type: "ollama", Model: "llava"},
	} {
		name := spec.Type + "/" + spec.Model
		if mode, ok := spec.AdditionalConfig["api_mode"].(string); ok {
			name += "/" + mode
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("OPENAI_API_KEY", "k")
			t.Setenv("ANTHROPIC_API_KEY", "k")
			t.Setenv("GEMINI_API_KEY", "k")
			cs := newCaptureServer(t, `{}`)
			spec.ID, spec.BaseURL = "shared-"+spec.Type, cs.srv.URL
			shared, err := providers.CreateProviderFromSpec(spec)
			require.NoError(t, err)
			defer func() { _ = shared.Close() }()

			scoped, ok := shared.(providers.MediaScoped)
			require.Truef(t, ok, "%T is not MediaScoped", shared)
			view := scoped.WithMediaSettings(providers.MediaSettings{Storage: oneFileStore{path}})
			assert.Equal(t, shared.ID(), view.ID())

			_, _ = view.Predict(context.Background(), req)
			body := cs.lastBody()
			assert.Truef(t, strings.Contains(body, want) || strings.Contains(body, scopedMediaURL),
				"the view resolves through its conversation's store\nrequest=%s", body)

			// The shared provider has no store, so the image cannot load: an
			// error, not the store another conversation set, and not the text
			// sent without its image (Claude and Gemini used to do that).
			before := len(cs.bodies)
			_, err = shared.Predict(context.Background(), req)
			assert.Error(t, err)
			assert.Len(t, cs.bodies, before, "a request went out without its image")
		})
	}
}
