#!/usr/bin/env python3
"""Response quality eval — exec eval protocol.

Checks that the assistant response meets basic quality criteria:
- Word count within configured range
- Contains complete sentences (ends with punctuation)
- No excessive repetition

Returns a score of 0.0-1.0 with detailed breakdown.
"""
import json
import re
import sys
from collections import Counter


def check_quality(content: str, min_words: int, max_words: int) -> dict:
    words = content.split()
    word_count = len(words)
    issues = []
    score = 1.0

    # Word count check
    if word_count < min_words:
        score -= 0.3
        issues.append(f"too short ({word_count} words, minimum {min_words})")
    elif word_count > max_words:
        score -= 0.2
        issues.append(f"too long ({word_count} words, maximum {max_words})")

    # Sentence completeness check
    sentences = re.split(r'[.!?]+', content.strip())
    sentences = [s.strip() for s in sentences if s.strip()]
    if sentences and not re.search(r'[.!?]$', content.strip()):
        score -= 0.1
        issues.append("response does not end with punctuation")

    # Repetition check (trigram repetition)
    if len(words) >= 6:
        trigrams = [" ".join(words[i:i+3]).lower() for i in range(len(words) - 2)]
        counts = Counter(trigrams)
        repeated = {t: c for t, c in counts.items() if c > 2}
        if repeated:
            score -= 0.2
            issues.append(f"excessive repetition: {list(repeated.keys())[:3]}")

    score = round(max(0.0, min(1.0, score)), 2)
    detail = f"Quality score: {score:.2f}"
    if issues:
        detail += f" — issues: {'; '.join(issues)}"
    else:
        detail += " — no issues found"

    return {
        "score": score,
        "detail": detail,
        "data": {
            "word_count": word_count,
            "sentence_count": len(sentences),
            "issues": issues,
        },
    }


def main():
    request = json.loads(sys.stdin.read())
    content = request.get("content", "")
    params = request.get("params", {})
    min_words = params.get("min_words", 5)
    max_words = params.get("max_words", 500)

    result = check_quality(content, min_words, max_words)
    print(json.dumps(result))


if __name__ == "__main__":
    main()
