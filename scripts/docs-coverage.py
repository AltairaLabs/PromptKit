#!/usr/bin/env python3
"""Report public-surface files that no docs page cites in its `verified`
sources, so new functionality with no page shows up as soon as it lands.

The surface is the list of globs in scripts/docs-surface.txt (test files
excluded). scripts/docs-coverage-ignore.txt lists files that deliberately have
no page. No model involved; always exits 0.

  uncovered <file>

Usage: python3 scripts/docs-coverage.py [--root <repo>]
"""
import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "lib"))
import docs_freshness as df  # noqa: E402


def main():
    args, root = sys.argv[1:], os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    while args:
        a = args.pop(0)
        if a == "--root" and args:
            root = args.pop(0)
        else:
            sys.exit(f"unknown argument: {a}")
    root = os.path.abspath(root)
    cited = set()
    for page in df.pages(root):
        cited.update(df.frontmatter(os.path.join(root, page))[1])
    for f in df.surface(root):
        if f not in cited:
            print(f"uncovered {f}")


if __name__ == "__main__":
    main()
