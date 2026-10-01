#!/usr/bin/env bash
#
# Checks reader-facing prose in docs/src/content/docs against the mechanical
# rules of docs/STYLE.md. The rule list is scripts/docs-voice-banned.txt; findings
# in directories listed in scripts/docs-voice-enforced.txt fail, others warn.
# scripts/docs-voice-exempt.txt drops given rules for given pages (a page whose
# purpose a rule contradicts, such as upgrade notes and the history rule).
#
# Usage: bash scripts/check-docs-voice.sh [--root <repo-root>]
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
while [ $# -gt 0 ]; do
	case "$1" in
	--root) ROOT="$2"; shift 2 ;;
	*) echo "unknown argument: $1" >&2; exit 2 ;;
	esac
done

DOCS_REL="docs/src/content/docs"
DOCS="$ROOT/$DOCS_REL"
BANNED="$ROOT/scripts/docs-voice-banned.txt"
ENFORCED="$ROOT/scripts/docs-voice-enforced.txt"
EXEMPT="$ROOT/scripts/docs-voice-exempt.txt"

[ -d "$DOCS" ] || { echo "no docs tree at $DOCS" >&2; exit 2; }

findings=$(mktemp); trap 'rm -f "$findings"' EXIT

# Prose rules, per file.
while IFS= read -r -d '' f; do
	rel="${f#"$ROOT"/}"
	awk -v banned="$BANNED" -v rel="$rel" '
	BEGIN {
		n = 0
		while ((getline l < banned) > 0) {
			if (l ~ /^#/ || l == "") continue
			tab = index(l, "\t"); if (tab == 0) continue
			n++; rule[n] = substr(l, 1, tab - 1); pat[n] = substr(l, tab + 1)
		}
		infence = 0; fm = 0; para = 0; pstart = 0
	}
	function out(r, t) { printf "%s:%d: %s: %s\n", rel, FNR, r, substr(t, 1, 120) }
	function flush() { if (para > 1) printf "%s:%d: em-dash: %d em-dashes in one paragraph\n", rel, pstart, para; para = 0 }
	function prose(t,   low, i, c) {
		gsub(/`[^`]*`/, "", t)
		low = tolower(t)
		for (i = 1; i <= n; i++) if (low ~ pat[i]) out(rule[i], $0)
		if (t ~ /┌|┐|┘|▶|◀|▼|▲/) out("ascii-diagram", $0)
		c = gsub(/—/, "—", t); if (para == 0) pstart = FNR; para += c
	}
	FNR == 1 && /^---[ \t]*$/ { fm = 1; next }
	fm && /^---[ \t]*$/ { fm = 0; next }
	fm { v = $0; sub(/^[A-Za-z_]+:[ \t]*/, "", v); if ($0 ~ /^(title|description):/) prose(v); next }
	/^[ \t]*(```|~~~)/ {
		flush()
		if (!infence) { infence = 1; fstart = FNR; lang = $0; sub(/^[ \t]*(```|~~~)[ \t]*/, "", lang) }
		else infence = 0
		next
	}
	infence { if (lang !~ /^mermaid/ && $0 ~ /┌|┐|┘|▶|◀|▼|▲/) out("ascii-diagram", $0); next }
	/^[ \t]*$/ { flush(); next }
	# A list item or table row starts its own unit for the em-dash count.
	/^[ \t]*([-*+][ \t]|[0-9]+\.[ \t]|\|)/ { flush() }
	{ prose($0) }
	END { flush(); if (infence) printf "%s:%d: unterminated-fence: fence opened here is never closed\n", rel, fstart }
	' "$f" >> "$findings" || { echo "check-docs-voice: awk failed on $rel" >&2; exit 2; }
done < <(find "$DOCS" -type f \( -name '*.md' -o -name '*.mdx' \) -print0)

# Classify each finding as error (enforced dir) or warn.
enforced=()
if [ -f "$ENFORCED" ]; then
	while IFS= read -r l; do
		case "$l" in ''|'#'*) continue ;; esac
		enforced+=("$DOCS_REL/${l%/}/")
	done < "$ENFORCED"
fi

# Exemptions: "<page under the docs root><TAB><rule>[,<rule>...]" or "*" for all rules.
exempt=()
if [ -f "$EXEMPT" ]; then
	while IFS=$'\t' read -r page rules; do
		case "$page" in ''|'#'*) continue ;; esac
		IFS=, read -ra rs <<<"$rules"
		for r in "${rs[@]}"; do exempt+=("$DOCS_REL/$page:*: $r:*"); done
		[ "$rules" = '*' ] && exempt+=("$DOCS_REL/$page:*")
	done < "$EXEMPT"
fi

status=0
while IFS= read -r line; do
	[ -n "$line" ] || continue
	skip=0
	for x in "${exempt[@]+"${exempt[@]}"}"; do
		# shellcheck disable=SC2254
		case "$line" in $x) skip=1; break ;; esac
	done
	[ "$skip" = 1 ] && continue
	level=warn
	for e in "${enforced[@]+"${enforced[@]}"}"; do
		case "$line" in "$e"*) level=error ;; esac
	done
	[ "$level" = error ] && status=1
	echo "$level $line"
done < "$findings"

exit $status
