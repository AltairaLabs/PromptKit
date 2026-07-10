package providers

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// resolveURLFakeStore is a minimal MediaStorageService for tests in this package.
// GetURL returns url/err; RetrieveMedia returns bytes as inline Data when set.
type resolveURLFakeStore struct {
	url   string
	err   error
	bytes string // base64; returned by RetrieveMedia when non-empty
}

func (f resolveURLFakeStore) StoreMedia(context.Context, *types.MediaContent, *storage.MediaMetadata) (storage.Reference, error) {
	return "", nil
}

func (f resolveURLFakeStore) RetrieveMedia(context.Context, storage.Reference) (*types.MediaContent, error) {
	d := f.bytes
	return &types.MediaContent{Data: &d, MIMEType: "image/png"}, nil
}

func (f resolveURLFakeStore) DeleteMedia(context.Context, storage.Reference) error { return nil }

func (f resolveURLFakeStore) GetURL(context.Context, storage.Reference, time.Duration) (string, error) {
	return f.url, f.err
}

func TestResolveURL(t *testing.T) {
	ref := "s3://bucket/key"
	explicit := "https://cdn.example.com/a.png"
	cases := []struct {
		name    string
		loader  *MediaLoader
		media   *types.MediaContent
		wantURL string
		wantOK  bool
	}{
		{"explicit url passthrough", NewMediaLoader(MediaLoaderConfig{}),
			&types.MediaContent{URL: &explicit, MIMEType: "image/png"}, explicit, true},
		{"remote storage ref", NewMediaLoader(MediaLoaderConfig{StorageService: resolveURLFakeStore{url: "https://s3/x?sig=1"}}),
			&types.MediaContent{StorageReference: &ref, MIMEType: "image/png"}, "https://s3/x?sig=1", true},
		{"local file:// ref falls back", NewMediaLoader(MediaLoaderConfig{StorageService: resolveURLFakeStore{url: "file:///tmp/x.png"}}),
			&types.MediaContent{StorageReference: &ref, MIMEType: "image/png"}, "", false},
		{"storage ref without store", NewMediaLoader(MediaLoaderConfig{}),
			&types.MediaContent{StorageReference: &ref, MIMEType: "image/png"}, "", false},
		{"inline data not url-able", NewMediaLoader(MediaLoaderConfig{}),
			&types.MediaContent{Data: stringPtr("abc"), MIMEType: "image/png"}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url, ok, err := tc.loader.ResolveURL(context.Background(), tc.media)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if ok != tc.wantOK || url != tc.wantURL {
				t.Fatalf("got (%q,%v), want (%q,%v)", url, ok, tc.wantURL, tc.wantOK)
			}
		})
	}
}
