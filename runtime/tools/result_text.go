package tools

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// FormatResultText is a JSON tool result as the model is given it: a string
// unquoted, an object or array indented, any other value as written (numbers
// keep their exact digits, null reads null). Text that is not JSON is returned
// as it is.
func FormatResultText(raw json.RawMessage) string {
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
		return v
	case map[string]any, []any:
		if indented, err := json.MarshalIndent(v, "", "  "); err == nil {
			return string(indented)
		}
	}
	return fmt.Sprint(value)
}

// TruncateResultText cuts content to maxSize bytes and appends a note saying so
// when it is longer. A maxSize of zero or less leaves content as it is.
func TruncateResultText(content string, maxSize int) string {
	if maxSize <= 0 || len(content) <= maxSize {
		return content
	}
	return fmt.Sprintf(
		"%s\n... [truncated, %d bytes exceeded limit of %d bytes]",
		content[:maxSize], len(content), maxSize,
	)
}
