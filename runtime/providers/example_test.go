package providers_test

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ExampleProviderDefaults shows the defaults passed to every provider constructor.
func ExampleProviderDefaults() {
	d := providers.ProviderDefaults{Temperature: 0.7, MaxTokens: 2000}
	fmt.Printf("temp=%.1f max=%d\n", d.Temperature, d.MaxTokens)
	// Output: temp=0.7 max=2000
}

// ExampleNewRegistry shows creating a provider registry and listing the
// (initially empty) set of registered inference providers.
func ExampleNewRegistry() {
	reg := providers.NewRegistry()
	fmt.Println("providers:", len(reg.List()))
	// Output: providers: 0
}

// ExampleMediaLoader_GetBase64Data shows loading inline base64 media data via
// a MediaLoader. Inline data is returned as-is with no file or network
// access, so this example needs no external resources or credentials.
func ExampleMediaLoader_GetBase64Data() {
	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{})

	data := "aGVsbG8=" // base64 for "hello"
	media := &types.MediaContent{Data: &data, MIMEType: "text/plain"}

	encoded, err := loader.GetBase64Data(context.Background(), media)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(encoded)
	// Output: aGVsbG8=
}
