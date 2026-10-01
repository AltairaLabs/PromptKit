#!/usr/bin/env bash
# Tests for scripts/check-docs-voice.sh
set -u
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$REPO_ROOT/scripts/check-docs-voice.sh"
FAILURES=0

# fixture <enforced-dirs...>: a root with the real banned list and the given enforced dirs.
fixture() {
	local d; d=$(mktemp -d)
	mkdir -p "$d/scripts" "$d/docs/src/content/docs/reference" "$d/docs/src/content/docs/how-to"
	cp "$REPO_ROOT/scripts/docs-voice-banned.txt" "$d/scripts/"
	: > "$d/scripts/docs-voice-enforced.txt"
	for e in "$@"; do echo "$e" >> "$d/scripts/docs-voice-enforced.txt"; done
	echo "$d"
}

# run_case name want-exit dir [grep-pattern-that-must-appear]
run_case() {
	local name="$1" want="$2" dir="$3" pat="${4:-}" out got=0
	out=$(bash "$SCRIPT" --root "$dir" 2>&1) || got=$?
	if [ "$got" != "$want" ]; then
		echo "FAIL - $name (want exit $want, got $got)"; echo "$out" | sed 's/^/       /'; FAILURES=1; return
	fi
	if [ -n "$pat" ] && ! grep -qE -- "$pat" <<<"$out"; then
		echo "FAIL - $name (output lacks /$pat/)"; echo "$out" | sed 's/^/       /'; FAILURES=1; return
	fi
	echo "ok   - $name"
}

P=docs/src/content/docs

d=$(fixture reference/)
printf -- '---\ntitle: X\n---\n\nThe controller deliberately skips it.\n' > "$d/$P/reference/a.md"
run_case "faux-candour in enforced dir fails" 1 "$d" '^error docs/src/content/docs/reference/a.md:5: faux-candour'

d=$(fixture)
printf 'The controller deliberately skips it.\n' > "$d/$P/reference/a.md"
run_case "same finding outside enforced dirs only warns" 0 "$d" '^warn .*faux-candour'

d=$(fixture reference/)
printf 'Run it:\n\n```bash\npromptarena simply --deliberately\n```\n\nUse `just` here.\n' > "$d/$P/reference/a.md"
run_case "code blocks and inline code are exempt" 0 "$d"

d=$(fixture reference/)
printf -- '---\ntitle: X\ndescription: Simply the best CRD.\n---\n\nFine.\n' > "$d/$P/reference/a.md"
run_case "frontmatter prose is checked" 1 "$d" 'a.md:3: filler'

d=$(fixture reference/)
printf 'One — two — three.\n\nOne — ok.\n' > "$d/$P/reference/a.md"
run_case "two em-dashes in one paragraph flagged" 1 "$d" 'a.md:1: em-dash'

d=$(fixture reference/)
printf 'A — b.\n\nC — d.\n' > "$d/$P/reference/a.md"
run_case "one em-dash per paragraph is fine" 0 "$d"

d=$(fixture reference/)
printf '```\n┌──────┐\n│ box  │──▶ x\n└──────┘\n```\n' > "$d/$P/reference/a.md"
run_case "ascii box diagram flagged" 1 "$d" 'ascii-diagram'

d=$(fixture reference/)
printf '```\ndocs/\n├── a.md\n└── b.md\n```\n' > "$d/$P/reference/a.md"
run_case "directory tree is not a diagram" 0 "$d"

d=$(fixture reference/)
printf '```mermaid\ngraph TD\n  A --> B\n```\n' > "$d/$P/reference/a.md"
run_case "mermaid block is fine" 0 "$d"

d=$(fixture reference/)
printf '```bash\necho hi\n\nText deliberately after.\n' > "$d/$P/reference/a.md"
run_case "unterminated fence reported" 1 "$d" 'unterminated-fence'

d=$(fixture reference/)
printf 'Fixed in #2752.\n' > "$d/$P/reference/a.md"
run_case "issue reference flagged" 1 "$d" 'issue-ref'

d=$(fixture reference/)
printf 'Tracked in [the issue](https://github.com/AltairaLabs/PromptKit/issues/42).\n' > "$d/$P/reference/a.md"
run_case "issue link flagged" 1 "$d" 'issue-ref'

d=$(fixture reference/)
printf 'Use `&#8212;` or a heading anchor like [x](#field-spec).\n' > "$d/$P/reference/a.md"
run_case "anchors and entities are not issue refs" 0 "$d"

d=$(fixture reference/)
printf 'bad\t(unclosed\n' >> "$d/scripts/docs-voice-banned.txt"
printf 'Fine.\n' > "$d/$P/reference/a.md"
run_case "a rule awk cannot compile fails loudly" 2 "$d" 'awk failed'

d=$(fixture reference/)
printf -- '- **a** — one\n- **b** — two\n\n| x — y |\n| z — w |\n\n1. a — b\n2. c — d\n' > "$d/$P/reference/a.md"
run_case "one em-dash per list item or table row is fine" 0 "$d"

d=$(fixture reference/)
printf -- '- a — b — c\n' > "$d/$P/reference/a.md"
run_case "two em-dashes in one list item flagged" 1 "$d" 'a.md:1: em-dash'

d=$(fixture reference/)
printf 'A seamless, best-in-class runtime.\n' > "$d/$P/reference/a.md"
run_case "positioning flagged" 1 "$d" 'a.md:1: positioning'

d=$(fixture reference/)
printf 'See docs/local-backlog/voice.md for the plan.\n' > "$d/$P/reference/a.md"
run_case "internal reference flagged" 1 "$d" 'a.md:1: internal'

exit $FAILURES
