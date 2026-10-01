#!/usr/bin/env python3
"""Record a docs page's verification provenance from /docs-review output.

Writes the page's `verified` frontmatter (commit + the repo files its claims
were verified against) and the claim-level sidecar
docs/provenance/<quadrant>/<page>.json that `/docs-review --stale` uses to
re-verify only the claims whose evidence changed. scripts/docs-stale.sh reads
the frontmatter.

Usage:
  python3 scripts/docs-provenance.py [--merge] <page.md> <stage2.json> <stage3.json> <commit>

<page.md> is repo-relative; the script runs from the repo root whatever the
caller's working directory. <commit> may be any ref; it is resolved to a sha.
A missing <stage2.json> is fine (claim text is then kept from the sidecar).

--merge  update an existing sidecar: claims whose ids appear in stage3 are
         replaced, every other claim is kept, and the page's sources are
         rebuilt from all of them. Stale mode uses this after re-verifying
         only the claims whose evidence changed.
"""
import json
import os
import re
import subprocess
import sys

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "lib"))
import docs_freshness as df  # noqa: E402

DOCS = df.DOCS


def resolve(ref):
    try:
        return subprocess.run(["git", "rev-parse", "--verify", ref + "^{commit}"],
                              check=True, capture_output=True, text=True).stdout.strip()
    except subprocess.CalledProcessError:
        sys.exit(f"cannot resolve commit {ref!r}")


def main():
    args = sys.argv[1:]
    merge = "--merge" in args
    args = [a for a in args if a != "--merge"]
    if len(args) != 4:
        sys.exit(__doc__)
    cwd = os.getcwd()
    page, stage2, stage3, commit = args
    stage2, stage3 = (os.path.join(cwd, p) for p in (stage2, stage3))
    root = subprocess.run(["git", "rev-parse", "--show-toplevel"], capture_output=True,
                          text=True, check=True).stdout.strip()
    os.chdir(root)
    if not page.startswith(DOCS + "/"):
        sys.exit(f"page must be repo-relative under {DOCS}/")
    commit = resolve(commit)

    results = json.load(open(stage3)).get("results", [])
    texts = {}
    if os.path.exists(stage2):
        texts = {c.get("id"): c.get("text", "") for c in json.load(open(stage2)).get("claims", [])}

    sidecar = df.sidecar_path(page)
    claims = {}
    if merge and os.path.exists(sidecar):
        old = json.load(open(sidecar))
        claims = {c["id"]: c for c in old.get("claims", [])}
        # Kept claims' line numbers are relative to the old commit; move them
        # to the new one so the stale check compares like with like.
        old_commit = old.get("commit")
        if old_commit and old_commit != commit:
            cache = {}
            for c in claims.values():
                moved = []
                for ev in c.get("evidence", []):
                    p, a, b = df.split_ev(ev)
                    if a is None:
                        moved.append(p)
                        continue
                    if p not in cache:
                        cache[p] = df.hunks(".", old_commit, commit, p)
                    a2, b2 = df.translate(cache[p], a), df.translate(cache[p], b)
                    if a2 is None or b2 is None:
                        moved.append(p)  # the cited lines changed: fall back to file level
                    else:
                        moved.append(f"{p}:{a2}" + (f"-{b2}" if b2 != a2 else ""))
                c["evidence"] = sorted(set(moved))

    for r in results:
        ev = r.get("evidence", "")
        ev = " ".join(ev) if isinstance(ev, list) else str(ev)
        cid = r.get("id")
        old = claims.get(cid, {})
        claims[cid] = {
            "id": cid,
            "claim": texts.get(cid) or old.get("claim", ""),
            "verdict": r.get("verdict"),
            "evidence": df.parse_evidence(ev),
        }

    side = [claims[k] for k in sorted(claims, key=lambda x: (str(type(x)), x))]
    sources = sorted({df.split_ev(e)[0] for c in side for e in c["evidence"]})
    if not sources:
        sys.exit(f"{page}: no repo evidence paths; nothing recorded")

    os.makedirs(os.path.dirname(sidecar), exist_ok=True)
    with open(sidecar, "w") as f:
        json.dump({"page": page, "commit": commit, "claims": side}, f, indent=1)
        f.write("\n")

    text = open(page).read()
    if not text.startswith("---\n"):
        sys.exit(f"{page}: no frontmatter")
    end = text.index("\n---", 3)
    fm = re.sub(r"\nverified:\n(?:  .*\n?)*", "\n", text[:end]).rstrip("\n")
    block = "\nverified:\n  commit: " + commit + "\n  sources:\n" + "".join(
        f"    - {p}\n" for p in sources)
    with open(page, "w") as f:
        f.write(fm + block.rstrip("\n") + text[end:])
    print(f"{page}: {len(sources)} sources, {len(side)} claims")


if __name__ == "__main__":
    main()
