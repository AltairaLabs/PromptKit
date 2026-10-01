"""Shared helpers for docs provenance, staleness and coverage (scripts/docs-*.py).

A reviewed page records `verified.commit` and `verified.sources` in its
frontmatter, and a sidecar docs/provenance/<page>.json maps each claim to its
evidence as `path` or `path:line` strings. Line numbers are relative to the
sidecar's commit.
"""
import glob
import json
import os
import re
import subprocess

DOCS = "docs/src/content/docs"
EVIDENCE_RE = re.compile(
    r"((?:[A-Za-z0-9_.\[\]-]+/)*[A-Za-z0-9_.\[\]-]+\.(?:go|tsx|ts|jsx|js|mjs|yaml|yml|proto|json|tpl|sh|sql|py)\b"
    r"|(?:[A-Za-z0-9_.\[\]-]+/)*(?:Makefile|Dockerfile(?:\.[A-Za-z0-9_-]+)?|Tiltfile))(?::(\d+)(?:-(\d+))?)?")
SURFACE_FILE = "scripts/docs-surface.txt"
IGNORE_FILE = "scripts/docs-coverage-ignore.txt"
WINDOW = 3  # lines of slack around a cited line


def git(root, *args, check=True):
    return subprocess.run(["git", "-C", root, *args], capture_output=True, text=True,
                          check=check).stdout


def has_commit(root, sha):
    return subprocess.run(["git", "-C", root, "cat-file", "-e", sha + "^{commit}"],
                          capture_output=True).returncode == 0


def sidecar_path(page):
    return "docs/provenance/" + page[len(DOCS) + 1:].rsplit(".", 1)[0] + ".json"


def frontmatter(path):
    """Return (commit, [sources]) from a page's `verified` block."""
    commit, sources, inv, insrc = "", [], False, False
    with open(path) as f:
        lines = f.read().split("\n")
    if not lines or lines[0].strip() != "---":
        return "", []
    for line in lines[1:]:
        if line.strip() == "---":
            break
        if line.startswith("verified:"):
            inv, insrc = True, False
            continue
        if inv and line and not line.startswith(" "):
            inv = insrc = False
        if inv and line.startswith("  commit:"):
            commit = line.split(":", 1)[1].strip().strip("\"'")
        elif inv and line.startswith("  sources:"):
            insrc = True
        elif inv and insrc and line.startswith("    - "):
            s = line[6:].split("#", 1)[0].strip().strip("\"'")
            if s:
                sources.append(s)
        elif inv and insrc and line.startswith("  ") and not line.startswith("    "):
            insrc = False
    return commit, sources


def parse_evidence(text, exists=os.path.isfile):
    """Return sorted unique evidence strings (`path` or `path:line[-end]`)."""
    out = set()
    for m in EVIDENCE_RE.finditer(text or ""):
        p = m.group(1)
        p = p[2:] if p.startswith("./") else p
        if p.startswith("docs/") or not exists(p):
            continue
        if m.group(2):
            out.add(f"{p}:{m.group(2)}" + (f"-{m.group(3)}" if m.group(3) else ""))
        else:
            out.add(p)
    return sorted(out)


def split_ev(ev):
    """'path:10-12' -> ('path', 10, 12); 'path' -> ('path', None, None)."""
    m = re.match(r"^(.*?):(\d+)(?:-(\d+))?$", ev)
    if not m:
        return ev, None, None
    a = int(m.group(2))
    return m.group(1), a, int(m.group(3) or a)


def hunks(root, old, new, path):
    """Hunks of `git diff -U0 old [new] -- path` as (old_start, old_count, new_start, new_count).
    new=None diffs against the working tree."""
    args = ["diff", "-U0", old] + ([new] if new else []) + ["--", path]
    out = git(root, *args, check=False)
    res = []
    for m in re.finditer(r"^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@", out, re.M):
        res.append((int(m.group(1)), int(m.group(2) or 1), int(m.group(3)), int(m.group(4) or 1)))
    return res


def touches(hs, a, b):
    """Does any hunk's old side overlap lines a..b (with WINDOW slack)?"""
    for os_, oc, _, _ in hs:
        lo, hi = os_, os_ + max(oc, 1) - 1
        if oc == 0:  # pure insertion after line os_
            lo, hi = os_, os_ + 1
        if lo <= b + WINDOW and hi >= a - WINDOW:
            return True
    return False


def translate(hs, line):
    """Map an old line number through hunks; None if the line was changed."""
    delta = 0
    for os_, oc, ns, nc in hs:
        if oc and os_ <= line < os_ + oc:
            return None
        if (oc and line >= os_ + oc) or (not oc and line > os_):
            delta += nc - oc
    return line + delta


def _patterns(path):
    if not os.path.exists(path):
        return []
    with open(path) as f:
        return [ln.strip() for ln in f if ln.strip() and not ln.lstrip().startswith("#")]


def surface(root):
    """Repo-relative public-surface files: the globs in scripts/docs-surface.txt,
    minus test files and scripts/docs-coverage-ignore.txt. Sorted."""
    out = set()
    for pat in _patterns(os.path.join(root, SURFACE_FILE)):
        for p in glob.glob(pat, root_dir=root, recursive=True):
            if os.path.isfile(os.path.join(root, p)) and not p.endswith("_test.go"):
                out.add(p)
    return sorted(out - set(_patterns(os.path.join(root, IGNORE_FILE))))


def pages(root):
    """Repo-relative docs pages (*.md, *.mdx), sorted."""
    res = []
    for d, _, files in os.walk(os.path.join(root, DOCS)):
        for f in files:
            if f.endswith((".md", ".mdx")):
                res.append(os.path.relpath(os.path.join(d, f), root))
    return sorted(res)


def load_sidecar(root, page):
    p = os.path.join(root, sidecar_path(page))
    if not os.path.exists(p):
        return None
    with open(p) as f:
        return json.load(f)
