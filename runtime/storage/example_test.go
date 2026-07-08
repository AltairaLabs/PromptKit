package storage_test

import (
	"context"
	"fmt"
	"os"

	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/storage/local"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ExampleMediaStorageService shows storing and retrieving media content
// through the local filesystem implementation of MediaStorageService. It
// needs no network access or cloud credentials — media lands under a
// temporary directory that is cleaned up at the end of the example.
func ExampleMediaStorageService() {
	dir, err := os.MkdirTemp("", "storage-example")
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	defer os.RemoveAll(dir)

	fs, err := local.NewFileStore(local.FileStoreConfig{BaseDir: dir})
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	data := "aGVsbG8=" // base64 for "hello"
	ref, err := fs.StoreMedia(context.Background(),
		&types.MediaContent{Data: &data, MIMEType: "text/plain"},
		&storage.MediaMetadata{SessionID: "s1", MIMEType: "text/plain"})
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	out, err := fs.RetrieveMedia(context.Background(), ref)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(out.MIMEType)
	// Output: text/plain
}
