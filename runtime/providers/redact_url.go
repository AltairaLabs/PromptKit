package providers

import "regexp"

// redactedValue replaces a credential in a URL query string.
const redactedValue = "REDACTED"

// urlSecretParams matches a query parameter whose VALUE is a credential,
// capturing the separator and name so both survive the substitution.
//
// The name is matched on TOKEN boundaries, not as a substring. An earlier
// version tested for "key" anywhere in the name and so redacted ?keyword=,
// ?monkey=, ?assignee= and ?design= — destroying exactly the diagnostic context
// this function promises to preserve — while still missing ?pwd=, ?auth=,
// ?code= and ?sas=. A parameter name is treated as `-`/`_`-separated segments
// and matches only when one whole segment is a credential word.
//
// The value is terminated by anything that cannot appear inside one: another
// parameter, whitespace, or the quote/bracket characters error formatters wrap
// URLs in. Go's *url.Error renders as `Post "https://…?key=X": read tcp …`, so
// the closing quote matters.
var urlSecretParams = regexp.MustCompile(
	`(?i)([?&](?:[a-z0-9]+[-_])*` +
		`(?:key|token|secret|password|passwd|pwd|credential|credentials|` +
		`signature|sig|sas|auth|authorization|code)` +
		`(?:[-_][a-z0-9]+)*=)[^&\s"'` + "`" + `<>\\]+`,
)

// RedactURLSecrets masks credential-bearing query parameters in any URLs found
// in s, leaving the rest of the string untouched.
//
// It exists because credentials in URLs reach logs through error strings, not
// through the logger. Gemini carries its API key as `?key=`, Go's *url.Error
// embeds the full URL, and every layer that wraps that error reformats the same
// text — so one transport failure wrote a live key to the log several times
// over. Redacting where the error is FORMATTED covers every wrapping layer at
// once, and covers any provider, not just the one that was noticed.
//
// This is a backstop, not the primary defense. A credential is better kept out
// of the URL entirely — see the Gemini provider's x-goog-api-key header.
//
// The host, path and non-secret parameters are preserved: an error that cannot
// say which endpoint failed is not much use.
func RedactURLSecrets(s string) string {
	if s == "" {
		return s
	}
	return urlSecretParams.ReplaceAllString(s, "${1}"+redactedValue)
}
