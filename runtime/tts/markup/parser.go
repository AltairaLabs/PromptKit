// Package markup is the canonical scanner for TTS characterization tags.
//
// Personas and scripts emit bracket-tagged text like
//
//	"[whispers]Come here[/]Did you hear that?"
//	"[excited][pause:500ms]Surprise!"
//
// and each TTS provider adapter lowers those tags into its native dialect:
// ElevenLabs v3 receives them verbatim, OpenAI gpt-4o-mini-tts splits them
// into a free-form `instructions` field, Cartesia maps known names into its
// `emotion` array, SSML providers translate them to <prosody>/<break>.
//
// This package owns the parsing and the canonical taxonomy so all providers
// agree on what the LLM is allowed to emit. Each provider remains free to
// drop tags it doesn't understand — see "Unknown tags" below.
//
// # Tag form
//
//   - "[name]" — a flag tag (e.g. [whispers]).
//   - "[name:value]" — a tag with a value (e.g. [pause:500ms]).
//   - "[/]" — explicit end of the current span; no value, no name.
//   - "\[" and "\]" — literal brackets, not part of a tag.
//
// Tag scope runs from the tag itself until the next tag, the end of the
// containing sentence (`.`, `?`, `!`), or an explicit "[/]" terminator.
// Spans do not overlap; a new tag implicitly closes the previous one.
//
// # Unknown tags
//
// Parsing accepts any name. Whether a name is meaningful is decided by the
// downstream consumer ([ToSSML] knows a fixed set; provider adapters know
// their own set). Unknown names degrade gracefully — they are dropped from
// SSML output and ignored by [ExtractInstructions]'s textual phrasing.
package markup

import (
	"strings"
)

// Canonical tag names. The taxonomy is intentionally small; adding a name
// here is a commitment that every consumer (SSML, instructions, provider
// adapters) should map it sensibly. Plural and singular forms are accepted
// where natural.
const (
	tagWhispers = "whispers"
	tagWhisper  = "whisper"
	tagShouts   = "shouts"
	tagShout    = "shout"
	tagLaughs   = "laughs"
	tagLaugh    = "laugh"
	tagSighs    = "sighs"
	tagSigh     = "sigh"
	tagExcited  = "excited"
	tagSad      = "sad"
	tagSmiles   = "smiles"
	tagSmile    = "smile"
	tagCalm     = "calm"
	tagPause    = "pause"
)

// Tag is a parsed bracket-tagged directive.
type Tag struct {
	// Name is the tag name, lower-cased and trimmed. "/" indicates the
	// explicit end-of-span marker "[/]".
	Name string

	// Value is the optional value portion ("[pause:500ms]" → "500ms").
	// Empty for flag tags.
	Value string

	// Start is the byte offset in the input where "[" begins.
	Start int

	// End is the byte offset immediately after the closing "]".
	End int
}

// IsClose reports whether t is the explicit end-of-span marker "[/]".
func (t Tag) IsClose() bool { return t.Name == "/" }

// ParseTags returns every bracket tag in text, in textual order. Malformed
// tags (unbalanced brackets, empty body) are skipped silently — the source
// LLM occasionally emits typos and the safer behavior is to keep going.
func ParseTags(text string) []Tag {
	var tags []Tag
	for i := 0; i < len(text); i++ {
		ch := text[i]
		// "\[" — literal bracket, skip the escape and the bracket.
		if ch == '\\' && i+1 < len(text) && (text[i+1] == '[' || text[i+1] == ']') {
			i++
			continue
		}
		if ch != '[' {
			continue
		}
		end := indexCloseBracket(text, i+1)
		if end < 0 {
			// Unbalanced "[" — stop scanning; the rest is plain text.
			break
		}
		body := text[i+1 : end]
		tag, ok := parseTagBody(body, i, end+1)
		if ok {
			tags = append(tags, tag)
		}
		i = end // outer loop's i++ then advances past "]"
	}
	return tags
}

// parseTagBody parses the content between "[" and "]" into a Tag.
// Returns ok=false for empty bodies.
func parseTagBody(body string, start, end int) (Tag, bool) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Tag{}, false
	}
	if body == "/" {
		return Tag{Name: "/", Start: start, End: end}, true
	}
	name, value, _ := strings.Cut(body, ":")
	return Tag{
		Name:  strings.ToLower(strings.TrimSpace(name)),
		Value: strings.TrimSpace(value),
		Start: start,
		End:   end,
	}, true
}

// indexCloseBracket returns the byte offset of the next unescaped "]" at or
// after start, or -1 if there is none.
func indexCloseBracket(text string, start int) int {
	for i := start; i < len(text); i++ {
		if text[i] == '\\' && i+1 < len(text) {
			i++ // skip the escaped char
			continue
		}
		if text[i] == ']' {
			return i
		}
	}
	return -1
}

// StripTags returns text with every bracket tag removed. Escaped brackets
// ("\[" and "\]") are unescaped to their literal form. Whitespace
// collapsing is intentionally minimal — providers should treat the output
// as the verbatim spoken text and decide their own normalization.
func StripTags(text string) string {
	tags := ParseTags(text)
	if len(tags) == 0 {
		return unescapeBrackets(text)
	}
	var b strings.Builder
	b.Grow(len(text))
	cursor := 0
	for _, t := range tags {
		b.WriteString(text[cursor:t.Start])
		cursor = t.End
	}
	b.WriteString(text[cursor:])
	return unescapeBrackets(b.String())
}

func unescapeBrackets(s string) string {
	if !strings.Contains(s, `\[`) && !strings.Contains(s, `\]`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && (s[i+1] == '[' || s[i+1] == ']') {
			b.WriteByte(s[i+1])
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// ExtractInstructions splits text into a free-form instructions phrase
// (suitable for the OpenAI gpt-4o-mini-tts `instructions` field) and the
// stripped spoken text. Tags are phrased with [tagPhrasing]; unknown tag
// names appear verbatim. Close tags ("[/]") are filtered out — they have
// no instruction value.
//
// Example:
//
//	"[whispers]Come here[/]Did you hear that?"
//	→ ("whisper", "Come here Did you hear that?")
//
//	"[excited][pause:500ms]Surprise!"
//	→ ("excited; pause for 500ms", "Surprise!")
func ExtractInstructions(text string) (instructions, stripped string) {
	stripped = StripTags(text)
	tags := ParseTags(text)
	if len(tags) == 0 {
		return "", stripped
	}
	parts := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		if t.IsClose() {
			continue
		}
		phrase := tagPhrasing(t)
		if phrase == "" {
			continue
		}
		// De-duplicate so "[excited][excited]" doesn't produce a repeat.
		if _, dup := seen[phrase]; dup {
			continue
		}
		seen[phrase] = struct{}{}
		parts = append(parts, phrase)
	}
	return strings.Join(parts, "; "), stripped
}

// tagPhrasing converts a tag to a short instruction phrase. Unknown names
// fall through and are emitted verbatim (with their value, if any).
func tagPhrasing(t Tag) string {
	switch t.Name {
	case tagWhispers, tagWhisper:
		return tagWhisper
	case tagShouts, tagShout:
		return tagShout
	case tagLaughs, tagLaugh:
		return "laugh briefly"
	case tagSighs, tagSigh:
		return tagSigh
	case tagExcited:
		return tagExcited
	case tagSad:
		return tagSad
	case tagSmile, tagSmiles:
		return "smile while speaking"
	case tagCalm:
		return tagCalm
	case tagPause:
		if t.Value == "" {
			return tagPause
		}
		return "pause for " + t.Value
	default:
		// Unknown name — preserve the value if present, drop otherwise.
		if t.Value != "" {
			return t.Name + " " + t.Value
		}
		return t.Name
	}
}

// ToSSML converts text into an SSML fragment for providers that consume
// SSML natively (Google, Azure). Tags wrap the span they govern:
//
//   - whisper/shout/calm/excited/sad/smile → <prosody> with rate/volume/pitch
//   - pause → <break time="…"/>
//   - unknown names → dropped (the text segment is rendered without a wrapper)
//
// The returned string is *not* wrapped in <speak>; callers add that boundary
// when they assemble the request body. Output text is XML-escaped.
func ToSSML(text string) string {
	var b strings.Builder
	tags := ParseTags(text)
	stripped := StripTags(text)
	if len(tags) == 0 {
		return xmlEscape(stripped)
	}

	// Re-walk text, splitting at tag boundaries. Each non-close tag opens
	// a span; the span ends at the next tag, "[/]", or a sentence-ender.
	cursor := 0
	var open *Tag // currently-open prosody-like tag, if any
	flushOpen := func(upTo int) {
		segment := xmlEscape(StripTags(text[cursor:upTo]))
		if open != nil {
			wrapped := wrapSSML(*open, segment)
			b.WriteString(wrapped)
		} else {
			b.WriteString(segment)
		}
		cursor = upTo
	}

	for i, t := range tags {
		// Plain text up to the start of this tag belongs to the previous
		// open tag (or to no tag).
		flushOpen(t.Start)
		// Consume the tag itself (cursor jumps over it).
		cursor = t.End
		if t.IsClose() {
			open = nil
			continue
		}
		if t.Name == tagPause {
			// <break/> is self-closing and does not govern a span.
			b.WriteString(breakSSML(t.Value))
			continue
		}
		// Open the new span. Implicitly close any prior open span first
		// (no overlapping spans, per the design).
		tag := tags[i]
		open = &tag
	}
	// Flush trailing content past the last tag.
	// Spans also implicitly close at sentence enders; if the final span
	// hasn't been explicitly closed we still emit it as a single wrapper.
	flushOpen(len(text))
	_ = stripped // referenced for symmetry; flushOpen already strips.
	return b.String()
}

// wrapSSML wraps segment with the SSML element implied by t. Returns the
// bare segment when t has no SSML mapping.
func wrapSSML(t Tag, segment string) string {
	if segment == "" {
		return ""
	}
	const closeProsody = `</prosody>`
	switch t.Name {
	case tagWhispers, tagWhisper:
		return `<prosody volume="x-soft" rate="slow">` + segment + closeProsody
	case tagShouts, tagShout:
		return `<prosody volume="x-loud">` + segment + closeProsody
	case tagLaughs, tagLaugh, tagExcited:
		return `<prosody pitch="+2st" rate="fast">` + segment + closeProsody
	case tagSighs, tagSigh, tagSad:
		return `<prosody pitch="-2st" rate="slow">` + segment + closeProsody
	case tagSmile, tagSmiles:
		return `<prosody pitch="+1st">` + segment + closeProsody
	case tagCalm:
		return `<prosody rate="slow">` + segment + closeProsody
	default:
		return segment
	}
}

// breakSSML emits a <break/> element with the given time value, defaulting
// to a short pause when value is empty.
func breakSSML(value string) string {
	if value == "" {
		return `<break time="500ms"/>`
	}
	return `<break time="` + xmlEscapeAttr(value) + `"/>`
}

// xmlEscape escapes characters that are unsafe in SSML text content.
func xmlEscape(s string) string {
	if !strings.ContainsAny(s, `<>&"`) {
		return s
	}
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

// xmlEscapeAttr escapes characters that are unsafe in SSML attribute values.
// Attribute values use double quotes here, so " must be encoded.
func xmlEscapeAttr(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", `"`, "&quot;")
	return r.Replace(s)
}
