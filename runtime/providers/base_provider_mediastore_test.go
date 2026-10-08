package providers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
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

// namedMediaStore is a store told apart by name.
type namedMediaStore struct {
	nilMediaStore
	name string
}

// WithMedia copies the provider with the conversation's settings, keeping its
// own where a setting is zero, and leaves the original untouched.
func TestBaseProvider_WithMediaCopiesAndKeepsZeroFields(t *testing.T) {
	own := namedMediaStore{name: "own"}
	conv := namedMediaStore{name: "conversation"}
	b := NewBaseProvider("p", false, nil)
	b.SetMediaStorageService(own)

	view := b.WithMedia(MediaSettings{Storage: conv, AllowPrivateNetworks: true})
	if view.mediaStorage != conv || !view.allowPrivateMediaURLs {
		t.Fatalf("view = %v/%v, want the conversation's store and private networks allowed",
			view.mediaStorage, view.allowPrivateMediaURLs)
	}
	if b.mediaStorage != own || b.allowPrivateMediaURLs {
		t.Fatal("WithMedia changed the original provider")
	}
	kept := b.WithMedia(MediaSettings{})
	if kept.mediaStorage != own || kept.allowPrivateMediaURLs {
		t.Fatal("zero settings must keep the provider's own")
	}
}
