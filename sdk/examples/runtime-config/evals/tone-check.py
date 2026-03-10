#!/usr/bin/env python3
"""Tone check eval — exec eval protocol.

Receives on stdin:
{
  "type": "tone_check",
  "params": {"expected_tone": "professional"},
  "content": "...",
  "context": {"messages": [...], "turn_index": N, ...}
}

Returns on stdout:
{
  "score": 0.0-1.0,
  "detail": "human-readable explanation",
  "data": {"tone": "...", "indicators": [...]}
}

Score of 1.0 = content matches the expected tone perfectly.
Score below threshold = eval fails (threshold set by assertions/guardrails).
"""
import json
import sys

# Tone indicator keywords (simplified — use an NLP model in production)
PROFESSIONAL = {"assist", "help", "understand", "apologize", "resolve", "pleased", "happy", "thank"}
CASUAL = {"hey", "gonna", "wanna", "cool", "awesome", "lol", "haha", "sup"}
AGGRESSIVE = {"stupid", "idiot", "shut", "dumb", "hate", "ridiculous"}


def check_tone(content: str, expected: str) -> dict:
    words = set(content.lower().split())

    professional_hits = words & PROFESSIONAL
    casual_hits = words & CASUAL
    aggressive_hits = words & AGGRESSIVE

    # Score based on expected tone
    if expected == "professional":
        bonus = len(professional_hits) * 0.1
        penalty = len(casual_hits) * 0.05 + len(aggressive_hits) * 0.3
        score = min(1.0, max(0.0, 0.7 + bonus - penalty))
        tone = "professional" if score >= 0.7 else ("aggressive" if aggressive_hits else "casual")
    else:
        score = 0.5
        tone = "unknown"

    indicators = sorted(professional_hits | casual_hits | aggressive_hits)

    return {
        "score": round(score, 2),
        "detail": f"Detected tone: {tone} (expected: {expected}, score: {score:.2f})",
        "data": {
            "tone": tone,
            "expected": expected,
            "indicators": indicators,
            "professional_count": len(professional_hits),
            "casual_count": len(casual_hits),
            "aggressive_count": len(aggressive_hits),
        },
    }


def main():
    request = json.loads(sys.stdin.read())
    content = request.get("content", "")
    params = request.get("params", {})
    expected = params.get("expected_tone", "professional")

    result = check_tone(content, expected)
    print(json.dumps(result))


if __name__ == "__main__":
    main()
