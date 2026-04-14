#!/usr/bin/env python3
"""
Reference exec selector that calls a remote rerank service.

Wire protocol (matches PromptKit's selection.ExecClient):

  stdin:  {"query": {"text": "...", "kind": "skill", "k": 5}, "candidates": [...]}
  stdout: {"selected": ["id1", "id2"], "reason": "optional"}

This script forwards the candidates to a hosted reranker (Cohere, Voyage,
Jina — pick your favorite) and returns the top-K IDs. It's a starting
point: swap the API call for whatever ranker you actually use.

Usage from RuntimeConfig:

  spec:
    selectors:
      rerank:
        command: python
        args: [/selectors/rerank.py]
        env: [RERANK_API_KEY, RERANK_URL]
        timeout_ms: 3000
    skills:
      selector: rerank

Requires: Python 3.9+, `requests` (or swap for httpx / urllib).
"""

from __future__ import annotations

import json
import os
import sys
from typing import Any

# Replace this block with your provider-of-choice. Kept inline so the
# script is self-contained and copy-pasteable.
import urllib.request


DEFAULT_K = 10


def call_reranker(query: str, docs: list[str], k: int) -> list[int]:
    """Return the indices of the top-k documents, ranked best first.

    Stub implementation: ranks by the count of overlapping whitespace
    tokens between the query and each document. Replace with a real
    rerank API call (Cohere /v1/rerank, Voyage /v1/rerank, etc.).
    """
    api_url = os.environ.get("RERANK_URL")
    api_key = os.environ.get("RERANK_API_KEY")
    if api_url and api_key:
        body = json.dumps({"query": query, "documents": docs, "top_k": k}).encode()
        req = urllib.request.Request(
            api_url,
            data=body,
            headers={
                "Authorization": f"Bearer {api_key}",
                "Content-Type": "application/json",
            },
        )
        with urllib.request.urlopen(req, timeout=2.5) as r:  # noqa: S310
            data = json.load(r)
        # Expect provider response like {"results": [{"index": int, "score": float}, ...]}
        return [int(item["index"]) for item in data.get("results", [])][:k]

    # Fallback for local testing without a configured backend.
    q_tokens = set(query.lower().split())
    scored = [
        (i, sum(1 for t in d.lower().split() if t in q_tokens))
        for i, d in enumerate(docs)
    ]
    scored.sort(key=lambda x: x[1], reverse=True)
    return [i for i, _ in scored[:k]]


def main() -> None:
    payload: dict[str, Any] = json.load(sys.stdin)
    query = payload.get("query", {})
    candidates = payload.get("candidates", [])

    text = query.get("text", "")
    k = query.get("k") or DEFAULT_K

    docs = [
        f"{c.get('name','')}: {c.get('description','')}"
        for c in candidates
    ]

    if not text or not docs:
        json.dump({"selected": []}, sys.stdout)
        return

    indices = call_reranker(text, docs, k)
    selected = [candidates[i]["id"] for i in indices if 0 <= i < len(candidates)]
    json.dump({"selected": selected}, sys.stdout)


if __name__ == "__main__":
    main()
