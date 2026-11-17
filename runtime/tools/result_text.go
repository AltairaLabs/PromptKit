package tools

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// FormatResultText is a JSON tool result as the model is given it: a string
// unquoted, an object or array indented, any other value as written (numbers
// keep their exact digits, null reads null). The empty string reads "" (two
// quote characters), as does no result at all: an empty text block is
// rejected by some providers. Text
// that is not JSON is returned as it is.
func FormatResultText(raw json.RawMessage) string {
	if len(bytes.TrimSpace(raw)) == 0 {
		return `""` // no result: an empty text block is rejected by some providers
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if dec.Decode(&value) != nil {
		return string(raw)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return string(raw) // text after the JSON value: not a JSON result
	}
	switch v := value.(type) {
	case nil:
		return "null"
	case string:
		if v == "" {
			return `""`
		}
		return v
	case map[string]any, []any:
		if indented, err := json.MarshalIndent(v, "", "  "); err == nil {
			return string(indented)
		}
	}
	return fmt.Sprint(value)
}

// TruncateResultText cuts content to at most maxSize bytes, never inside a
// character, and appends a note saying so when it is longer, logging the cut
// under toolName. A maxSize of zero or less leaves content as it is.
func TruncateResultText(toolName, content string, maxSize int) string {
	size := len(content)
	if maxSize <= 0 || size <= maxSize {
		return content
	}
	logger.Warn("Tool result truncated", "tool", toolName, "size", size, "limit", maxSize)
	// Back up to a character boundary: at most a character's width, so text
	// that is not UTF-8 keeps its full prefix.
	cut := maxSize
	for back := 1; back < utf8.UTFMax && back <= maxSize; back++ {
		if utf8.RuneStart(content[maxSize]) {
			break
		}
		if utf8.RuneStart(content[maxSize-back]) {
			cut = maxSize - back
			break
		}
	}
	return fmt.Sprintf(
		"%s\n... [truncated, %d bytes exceeded limit of %d bytes]",
		content[:cut], size, maxSize,
	)
}

// ModelResultOptions says how a completed tool result is prepared for the model.
type ModelResultOptions struct {
	// ToolName names the tool in the truncation log.
	ToolName string
	// MaxSize is the size limit in bytes for each text; zero or less is no limit
	// (the registry's MaxToolResultSize).
	MaxSize int
	// Rewrite, when set, rewrites each text before the size limit applies.
	Rewrite func(string) (string, error)
}

// ModelResult is a completed tool result as the model is given it.
type ModelResult struct {
	// Text is the result's text: the formatted JSON, or the joined text parts.
	Text string
	// Parts holds a multimodal result's parts (text and media, in order); nil
	// for a text-only result.
	Parts []types.ContentPart
}

// BuildModelResult prepares a completed tool result for the model: the one
// place a result's formatting, rewriting and size limit are applied, for a call
// the pipeline ran and for one a caller resolved. A result with parts is its
// parts, each text part rewritten and capped and media parts kept in place;
// otherwise it is raw formatted as text. The error is Rewrite's.
func BuildModelResult(
	raw json.RawMessage, parts []types.ContentPart, opts ModelResultOptions,
) (ModelResult, error) {
	prepare := func(text string) (string, error) {
		if opts.Rewrite != nil {
			var err error
			if text, err = opts.Rewrite(text); err != nil {
				return "", err
			}
		}
		if text = TruncateResultText(opts.ToolName, text, opts.MaxSize); text == "" {
			return `""`, nil // an empty text block is rejected by some providers
		}
		return text, nil
	}
	if len(parts) == 0 {
		text, err := prepare(FormatResultText(raw))
		return ModelResult{Text: text}, err
	}
	out := make([]types.ContentPart, len(parts))
	for i, p := range parts {
		if p.Type != types.ContentTypeText || p.Text == nil {
			out[i] = p
			continue
		}
		text, err := prepare(*p.Text)
		if err != nil {
			return ModelResult{}, err
		}
		part := p // the part as it was, its text replaced
		part.Text = &text
		out[i] = part
	}
	joined := types.MessageToolResult{Parts: out}
	return ModelResult{Text: joined.GetTextContent(), Parts: out}, nil
}
