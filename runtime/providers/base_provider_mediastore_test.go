package providers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestBaseProvider_MediaLoaderUsesInjectedStore(t *testing.T) {
	b := NewBaseProvider("t", false, nil)
	ref := "s3://bucket/key"
	media := &types.MediaContent{StorageReference: &ref, MIMEType: "image/png"}

	// Without a store: ResolveURL cannot make a URL.
	if _, ok, _ := b.MediaLoader().ResolveURL(context.Background(), media); ok {
		t.Fatal("expected ok=false with no store")
	}

	b.SetMediaStorageService(resolveURLFakeStore{url: "https://s3/x?sig=1"})
	url, ok, err := b.MediaLoader().ResolveURL(context.Background(), media)
	if err != nil || !ok || url != "https://s3/x?sig=1" {
		t.Fatalf("got (%q,%v,%v) after injecting store", url, ok, err)
	}
}
