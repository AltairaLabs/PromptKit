#!/usr/bin/env python3
"""Sentiment analysis tool — exec protocol (one-shot).

Receives: {"args": {"text": "..."}}
Returns:  {"result": {"sentiment": "positive|negative|neutral", "score": 0.0-1.0, "keywords": [...]}}
"""
import json
import sys

# Simple keyword-based sentiment (replace with a real model in production)
POSITIVE = {"happy", "great", "love", "thanks", "excellent", "good", "pleased", "wonderful", "amazing", "helpful"}
NEGATIVE = {"angry", "terrible", "hate", "awful", "bad", "frustrated", "disappointing", "broken", "worst", "useless"}


def analyze(text: str) -> dict:
    words = set(text.lower().split())
    pos = words & POSITIVE
    neg = words & NEGATIVE

    if len(pos) > len(neg):
        sentiment = "positive"
        score = min(1.0, 0.5 + len(pos) * 0.1)
    elif len(neg) > len(pos):
        sentiment = "negative"
        score = max(0.0, 0.5 - len(neg) * 0.1)
    else:
        sentiment = "neutral"
        score = 0.5

    return {
        "sentiment": sentiment,
        "score": round(score, 2),
        "keywords": sorted(pos | neg),
    }


def main():
    request = json.loads(sys.stdin.read())
    text = request["args"].get("text", "")
    result = analyze(text)
    print(json.dumps({"result": result}))


if __name__ == "__main__":
    main()
