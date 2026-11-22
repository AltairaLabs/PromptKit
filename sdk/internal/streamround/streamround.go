// Package streamround is the stream plumbing the AG-UI adapter (sdk/agui)
// needs from an sdk.StreamChunk and that is not part of the public SDK API:
// the text of the reply a ChunkToolCall's call belongs to, as the turn
// stores it. A structured-output turn withholds its tool rounds' text from
// the stream, and the adapter sends it as the calls' parent message before
// the calls.
//
// Package sdk registers the lookup in its init.
package streamround

// replyText is the lookup package sdk registers.
var replyText func(chunk any) string

// Register sets the lookup ReplyText uses. Package sdk calls it in its init.
func Register(f func(chunk any) string) { replyText = f }

// ReplyText returns the stored text of the reply a ChunkToolCall chunk's
// call belongs to, or "" for another chunk or a reply without text.
func ReplyText(chunk any) string {
	if replyText == nil {
		return ""
	}
	return replyText(chunk)
}
