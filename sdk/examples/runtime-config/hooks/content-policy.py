#!/usr/bin/env python3
"""Content policy hook — exec hook protocol (provider/filter).

Runs after each LLM response. Checks for content policy violations:
- Prohibited topics (financial advice, medical diagnosis, legal counsel)
- Excessive personal information disclosure

Filter mode: returns {"allow": false, "reason": "..."} to block the response.
"""
import json
import sys

PROHIBITED_PATTERNS = [
    ("invest in", "financial advice"),
    ("buy stocks", "financial advice"),
    ("guaranteed return", "financial advice"),
    ("diagnose you with", "medical diagnosis"),
    ("you have a condition", "medical diagnosis"),
    ("legal advice", "legal counsel"),
    ("sue them", "legal counsel"),
]


def check_policy(content: str) -> dict:
    content_lower = content.lower()

    for pattern, category in PROHIBITED_PATTERNS:
        if pattern in content_lower:
            return {
                "allow": False,
                "reason": f"Content policy violation: {category} (matched: '{pattern}')",
                "metadata": {"category": category, "pattern": pattern},
            }

    return {"allow": True}


def main():
    request = json.loads(sys.stdin.read())
    phase = request.get("phase", "")

    # Only check after_call (output validation)
    if phase != "after_call":
        print(json.dumps({"allow": True}))
        return

    # Extract the response content
    response = request.get("response", {})
    message = response.get("message", {})

    # Message content is in the parts array
    content = ""
    parts = message.get("parts", [])
    for part in parts:
        if isinstance(part, dict) and part.get("type") == "text":
            content += part.get("text", "")
        elif isinstance(part, str):
            content += part

    result = check_policy(content)
    print(json.dumps(result))


if __name__ == "__main__":
    main()
