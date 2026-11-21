package a2a

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// octetStream names bytes of no particular type. Content sniffing reports it
// for bytes it does not recognize.
const octetStream = "application/octet-stream"

// Media types named more than once below that runtime/types has no constant for.
const (
	mimeAudioM4A  = "audio/m4a"
	mimeAudioFLAC = "audio/flac"
	mimeAudioAIFF = "audio/aiff"
	mimeVideoAVI  = "video/avi"
	mimeVideoMOV  = "video/mov"
	mimeVideoMPEG = "video/mpeg"
	mimeTextHTML  = "text/html"
)

// errOctetStream refuses content typed application/octet-stream, which no
// provider can read: it says nothing about what the bytes are.
var errOctetStream = fmt.Errorf("%w: %s names no type of content", ErrContentTypeNotSupported, octetStream)

// extensionMediaTypes maps the file extensions a part's type is inferred from
// to the names providers list (runtime/providers' format lists). It is fixed
// here rather than read from the host's MIME tables, which differ from host
// to host.
var extensionMediaTypes = map[string]string{
	".png":  types.MIMETypeImagePNG,
	".jpg":  types.MIMETypeImageJPEG,
	".jpeg": types.MIMETypeImageJPEG,
	".gif":  types.MIMETypeImageGIF,
	".webp": types.MIMETypeImageWebP,
	".heic": "image/heic",
	".heif": "image/heif",

	".wav":  types.MIMETypeAudioWAV,
	".mp3":  types.MIMETypeAudioMP3,
	".m4a":  mimeAudioM4A,
	".aac":  "audio/aac",
	".ogg":  types.MIMETypeAudioOgg,
	".oga":  types.MIMETypeAudioOgg,
	".opus": "audio/opus",
	".flac": mimeAudioFLAC,
	".aif":  mimeAudioAIFF,
	".aiff": mimeAudioAIFF,
	".weba": types.MIMETypeAudioWebM,

	".mp4":  types.MIMETypeVideoMP4,
	".m4v":  types.MIMETypeVideoMP4,
	".mpeg": mimeVideoMPEG,
	".mpg":  mimeVideoMPEG,
	".mov":  mimeVideoMOV,
	".avi":  mimeVideoAVI,
	".webm": types.MIMETypeVideoWebM,
	".wmv":  "video/wmv",
	".flv":  "video/x-flv",
	".3gp":  "video/3gpp",

	".pdf":  types.MIMETypePDF,
	".txt":  types.MIMETypePlainText,
	".md":   types.MIMETypeMarkdown,
	".csv":  types.MIMETypeCSV,
	".html": mimeTextHTML,
	".htm":  mimeTextHTML,
	".json": types.MIMETypeJSON,
	".xml":  types.MIMETypeXML,
	".rtf":  "application/rtf",
	".doc":  types.MIMETypeDoc,
	".docx": types.MIMETypeDocx,
}

// mediaTypeAliases maps other names for a type, from senders and from
// http.DetectContentType, to the name providers list.
var mediaTypeAliases = map[string]string{
	"audio/wave":        types.MIMETypeAudioWAV,
	"audio/x-wav":       types.MIMETypeAudioWAV,
	"audio/vnd.wave":    types.MIMETypeAudioWAV,
	"audio/x-pn-wav":    types.MIMETypeAudioWAV,
	"audio/mp3":         types.MIMETypeAudioMP3,
	"audio/x-mp3":       types.MIMETypeAudioMP3,
	"audio/x-mpeg":      types.MIMETypeAudioMP3,
	"audio/x-aiff":      mimeAudioAIFF,
	"audio/x-flac":      mimeAudioFLAC,
	"audio/mp4":         mimeAudioM4A,
	"audio/x-m4a":       mimeAudioM4A,
	"video/quicktime":   mimeVideoMOV,
	"video/x-msvideo":   mimeVideoAVI,
	"video/msvideo":     mimeVideoAVI,
	"image/jpg":         types.MIMETypeImageJPEG,
	"image/pjpeg":       types.MIMETypeImageJPEG,
	"image/x-png":       types.MIMETypeImagePNG,
	"application/x-pdf": types.MIMETypePDF,
}

// canonicalMediaType gives mediaType under the name providers list:
// lowercase, without parameters, aliases resolved.
func canonicalMediaType(mediaType string) string {
	bare, _, _ := strings.Cut(mediaType, ";")
	bare = strings.ToLower(strings.TrimSpace(bare))
	if alias, ok := mediaTypeAliases[bare]; ok {
		return alias
	}
	return bare
}

// extensionMediaType is the media type name's extension stands for, matched
// without regard to case, or "" for an extension not in extensionMediaTypes.
func extensionMediaType(name string) string {
	return extensionMediaTypes[strings.ToLower(path.Ext(name))]
}

// sniffRawMediaType is the type raw's bytes are recognized as, or octetStream.
// An MP4 container whose major brand is M4A is audio, though
// http.DetectContentType reports every MP4 container as video/mp4.
func sniffRawMediaType(raw []byte) string {
	// An MP4 file opens with a 4-byte box size, then the ftyp box and its major brand.
	const boxSizeLen = 4
	detected := canonicalMediaType(http.DetectContentType(raw))
	if detected == types.MIMETypeVideoMP4 && bytes.HasPrefix(raw[min(boxSizeLen, len(raw)):], []byte("ftypM4A ")) {
		return mimeAudioM4A
	}
	return detected
}

// rawMediaType names a raw part's media type: MediaType, else Filename's
// extension, else the type its bytes sniff as. Text whose type was inferred
// must be UTF-8, the only encoding providers read text in.
func rawMediaType(part *Part) (string, error) {
	if part.MediaType != "" {
		return declaredMediaType(part.MediaType)
	}
	mediaType := extensionMediaType(part.Filename)
	if mediaType == "" {
		mediaType = sniffRawMediaType(part.Raw)
	}
	if mediaType == octetStream {
		return "", fmt.Errorf(
			"%w: raw part has no mediaType and its content is not a recognized type", ErrContentTypeNotSupported)
	}
	if strings.HasPrefix(mediaType, "text/") && !utf8.Valid(part.Raw) {
		return "", fmt.Errorf("%w: raw part has no mediaType and its text is not UTF-8", ErrContentTypeNotSupported)
	}
	return mediaType, nil
}

// urlMediaType names a URL part's media type: MediaType, else Filename's
// extension, else a data URL's declared type or the URL path's extension.
func urlMediaType(part *Part) (string, error) {
	if part.MediaType != "" {
		return declaredMediaType(part.MediaType)
	}
	if t := extensionMediaType(part.Filename); t != "" {
		return t, nil
	}
	if rest, isData := cutDataScheme(*part.URL); isData {
		return dataURLMediaType(rest)
	}
	mediaType := ""
	if u, err := url.Parse(*part.URL); err == nil {
		mediaType = extensionMediaType(u.Path)
	}
	if mediaType == "" {
		return "", fmt.Errorf("%w: url part has no mediaType and neither its filename nor its URL names a type",
			ErrContentTypeNotSupported)
	}
	return mediaType, nil
}

// declaredMediaType is a sender's mediaType under its canonical name, refused
// when it is application/octet-stream.
func declaredMediaType(declared string) (string, error) {
	mediaType := canonicalMediaType(declared)
	if mediaType == octetStream {
		return "", errOctetStream
	}
	return mediaType, nil
}

// cutDataScheme returns what follows a "data:" scheme, matched without regard
// to case, and whether u is a data URL.
func cutDataScheme(u string) (string, bool) {
	const scheme = "data:"
	if !strings.EqualFold(u[:min(len(u), len(scheme))], scheme) {
		return "", false
	}
	return u[len(scheme):], true
}

// dataURLMediaType reads the media type from a data URL's header (what
// follows "data:"): text/plain when the header names none (RFC 2397).
func dataURLMediaType(rest string) (string, error) {
	header, _, hasComma := strings.Cut(rest, ",")
	if !hasComma {
		return "", fmt.Errorf("a2a: url part is a malformed data URL: it has no comma")
	}
	mediaType := canonicalMediaType(header)
	if mediaType == "" {
		return types.MIMETypePlainText, nil
	}
	if mediaType == octetStream {
		return "", errOctetStream
	}
	return mediaType, nil
}
