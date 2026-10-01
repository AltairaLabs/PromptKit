#!/usr/bin/env python3
"""Report docs pages whose verified facts may have changed. No model involved.

A page is stale only when a change touches what it depends on:
  - a changed hunk overlaps a line one of its claims cites (+/- a few lines), or
  - a claim cites the file with no line (the whole file counts), or
  - the page owns a public-surface file (scripts/docs-surface.txt; the page
    has the most claims citing it) and the change adds lines to it (possible
    new field, option, flag or value).
Pages without a sidecar fall back to file level: any change is stale.

  stale <page>: <file> (<why>)[; <file> (<why>)...]
  missing <page>: <file>
  unknown-commit <page>: <sha>
  unverified <page>

Usage: python3 scripts/docs-stale.py [--root <repo>] [--base <ref>]
  default   compare each page's verified commit with the working tree
  --base    PR mode: only changes between <ref> and HEAD count (CI uses HEAD^1)
Always exits 0.
"""
import os
import sys
from collections import Counter

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "lib"))
import docs_freshness as df  # noqa: E402


def main():
    args, root, base = sys.argv[1:], os.path.dirname(os.path.dirname(os.path.abspath(__file__))), None
    while args:
        a = args.pop(0)
        if a == "--root":
            root = args.pop(0)
        elif a == "--base":
            base = args.pop(0)
        else:
            sys.exit(f"unknown argument: {a}")
    root = os.path.abspath(root)

    pages = df.pages(root)
    surface = set(df.surface(root))

    records, owners = {}, {}
    counts = {}
    for page in pages:
        commit, sources = df.frontmatter(os.path.join(root, page))
        side = df.load_sidecar(root, page)
        records[page] = (commit, sources, side)
        for c in (side or {}).get("claims", []):
            for ev in c.get("evidence", []):
                p = df.split_ev(ev)[0]
                if p in surface:
                    counts.setdefault(p, Counter())[page] += 1
    for p, cnt in counts.items():
        owners[p] = cnt.most_common(1)[0][0]

    changed_in_pr = None
    if base:
        changed_in_pr = set(df.git(root, "diff", "--name-only", base, "HEAD", check=False).split())

    for page in pages:
        commit, sources, side = records[page]
        if not commit or not sources:
            print(f"unverified {page}")
            continue
        if not df.has_commit(root, commit):
            print(f"unknown-commit {page}: {commit}")
            continue
        evidence = {}
        for c in (side or {}).get("claims", []):
            for ev in c.get("evidence", []):
                p, a, b = df.split_ev(ev)
                evidence.setdefault(p, []).append((a, b))
        hits = []
        for src in sources:
            if base and src not in changed_in_pr:
                continue
            if not os.path.exists(os.path.join(root, src)):
                print(f"missing {page}: {src}")
                continue
            if base:
                hs = df.hunks(root, base, "HEAD", src)
                shift = df.hunks(root, commit, base, src)  # verified commit -> PR base
            else:
                hs, shift = df.hunks(root, commit, None, src), []
            if not hs:
                continue
            why = []
            cites = evidence.get(src)
            if side is None or cites is None:
                why.append("file changed")
            else:
                lines = []
                for a, b in cites:
                    if a is None:
                        why.append("cited without a line")
                        break
                    a2 = df.translate(shift, a) if shift else a
                    b2 = df.translate(shift, b) if shift else b
                    if a2 is None or b2 is None:
                        # Changed before this PR: older drift, reported weekly.
                        continue
                    if df.touches(hs, a2, b2):
                        lines.append(a)
                if lines:
                    why.append("cited lines " + ",".join(str(x) for x in sorted(set(lines))[:8]))
            if not why and owners.get(src) == page and any(nc > 0 for _, _, _, nc in hs):
                why.append("surface added")
            if why:
                hits.append(f"{src} ({why[0]})")
        if hits:
            print(f"stale {page}: " + "; ".join(hits))


if __name__ == "__main__":
    main()
