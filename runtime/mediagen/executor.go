package mediagen

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Executor resolves an image/video provider from the pool and invokes its
// Generate method, returning the result as a media ContentPart that flows into
// the conversation message log. It implements tools.MultimodalExecutor.
type Executor struct {
	pool *base.Registry
}

// NewExecutor returns an Executor backed by the given provider pool. The pool
// may be nil; in that case every call resolves to a "no provider configured"
// error returned to the calling agent.
func NewExecutor(pool *base.Registry) *Executor {
	return &Executor{pool: pool}
}

// Name returns the executor mode string. It must match ToolDescriptor.Mode.
func (e *Executor) Name() string { return ExecutorMode }

// Execute satisfies tools.Executor. mediagen always produces media parts, so
// this returns the JSON summary and drops the parts; the registry prefers
// ExecuteMultimodal where available.
func (e *Executor) Execute(
	ctx context.Context, descriptor *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	result, _, err := e.ExecuteMultimodal(ctx, descriptor, args)
	return result, err
}

// ExecuteMultimodal generates media and returns a JSON summary plus the media
// ContentParts. An unresolved provider, a provider error, or empty output is
// returned as an error so the agent sees a clear tool failure rather than a
// silent no-op.
func (e *Executor) ExecuteMultimodal(
	ctx context.Context, descriptor *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, []types.ContentPart, error) {
	switch descriptor.Name {
	case ImageGenerateToolName:
		return e.generateImage(ctx, args)
	case VideoGenerateToolName:
		return e.generateVideo(ctx, args)
	default:
		return nil, nil, fmt.Errorf("mediagen: unsupported tool %q", descriptor.Name)
	}
}

type imageArgs struct {
	Prompt string `json:"prompt"`
	Size   string `json:"size"`
}

type videoArgs struct {
	Prompt      string `json:"prompt"`
	AspectRatio string `json:"aspect_ratio"`
}

func (e *Executor) generateImage(
	ctx context.Context, args json.RawMessage,
) (json.RawMessage, []types.ContentPart, error) {
	var a imageArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, nil, fmt.Errorf("mediagen: invalid image__generate args: %w", err)
	}
	if a.Prompt == "" {
		return nil, nil, fmt.Errorf("mediagen: image__generate requires a non-empty prompt")
	}

	prov, err := resolveProvider[base.ImageProvider](e.pool, base.ProviderTypeImage, "image")
	if err != nil {
		return nil, nil, err
	}

	resp, err := prov.Generate(ctx, base.ImageRequest{Prompt: a.Prompt, Size: a.Size})
	if err != nil {
		return nil, nil, fmt.Errorf("mediagen: image generation failed: %w", err)
	}

	parts := mediaParts(resp.Images, resp.MIMEType, types.MIMETypeImagePNG, newImagePart)
	if len(parts) == 0 {
		return nil, nil, fmt.Errorf("mediagen: image provider %q returned no images", prov.Name())
	}

	summary := summaryJSON(prov.Name(), partsMIME(parts), len(parts))
	return summary, parts, nil
}

func (e *Executor) generateVideo(
	ctx context.Context, args json.RawMessage,
) (json.RawMessage, []types.ContentPart, error) {
	var a videoArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, nil, fmt.Errorf("mediagen: invalid video__generate args: %w", err)
	}
	if a.Prompt == "" {
		return nil, nil, fmt.Errorf("mediagen: video__generate requires a non-empty prompt")
	}

	prov, err := resolveProvider[base.VideoProvider](e.pool, base.ProviderTypeVideo, "video")
	if err != nil {
		return nil, nil, err
	}

	resp, err := prov.Generate(ctx, base.VideoRequest{Prompt: a.Prompt, AspectRatio: a.AspectRatio})
	if err != nil {
		return nil, nil, fmt.Errorf("mediagen: video generation failed: %w", err)
	}

	parts := mediaParts(resp.Videos, resp.MIMEType, types.MIMETypeVideoMP4, types.NewVideoPartFromData)
	if len(parts) == 0 {
		return nil, nil, fmt.Errorf("mediagen: video provider %q returned no videos", prov.Name())
	}

	summary := summaryJSON(prov.Name(), partsMIME(parts), len(parts))
	return summary, parts, nil
}

// resolveProvider returns the default provider of the given type, deterministically
// chosen as the lexically-first provider by Name(). base.Registry.GetAll returns
// map order, so it is sorted here. Multi-provider selection (an explicit
// provider_id argument) is a planned follow-up; until then the first provider wins.
func resolveProvider[T base.Provider](pool *base.Registry, typ base.ProviderType, kind string) (T, error) {
	var zero T
	if pool == nil {
		return zero, fmt.Errorf("mediagen: no %s provider configured", kind)
	}
	provs := pool.GetAll(typ)
	if len(provs) == 0 {
		return zero, fmt.Errorf("mediagen: no %s provider configured", kind)
	}
	sort.Slice(provs, func(i, j int) bool { return provs[i].Name() < provs[j].Name() })
	chosen := provs[0]
	typed, ok := chosen.(T)
	if !ok {
		return zero, fmt.Errorf("mediagen: provider %q does not support %s generation", chosen.Name(), kind)
	}
	return typed, nil
}

// mediaParts builds media ContentParts from generated media bytes. Each entry is
// treated as base64-encoded media data (the convention the imagen provider uses),
// so it is passed through to the part factory as a base64 string. fallbackMIME is
// used when the provider did not report a MIME type.
func mediaParts(
	media [][]byte, mimeType, fallbackMIME string, newPart func(base64Data, mimeType string) types.ContentPart,
) []types.ContentPart {
	if mimeType == "" {
		mimeType = fallbackMIME
	}
	parts := make([]types.ContentPart, 0, len(media))
	for _, m := range media {
		if len(m) == 0 {
			continue
		}
		parts = append(parts, newPart(string(m), mimeType))
	}
	return parts
}

// newImagePart adapts types.NewImagePartFromData (which takes a detail arg) to
// the uniform (base64Data, mimeType) factory signature used by mediaParts.
func newImagePart(base64Data, mimeType string) types.ContentPart {
	return types.NewImagePartFromData(base64Data, mimeType, nil)
}

func partsMIME(parts []types.ContentPart) string {
	if len(parts) == 0 || parts[0].Media == nil {
		return ""
	}
	return parts[0].Media.MIMEType
}

func summaryJSON(provider, mimeType string, count int) json.RawMessage {
	out, _ := json.Marshal(map[string]any{
		"provider":  provider,
		"mime_type": mimeType,
		"count":     count,
	})
	return out
}
