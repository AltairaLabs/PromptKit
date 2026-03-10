#!/usr/bin/env python3
"""Audit logger hook — exec hook protocol (session/observe).

Logs session lifecycle events to stderr for demonstration.
In production, this would write to an audit log, database, or SIEM.

Observe mode: the pipeline never blocks on this hook. If it crashes
or times out, the pipeline continues unaffected.
"""
import json
import sys
from datetime import datetime, timezone


def main():
    request = json.loads(sys.stdin.read())
    phase = request.get("phase", "unknown")
    event = request.get("event", {})

    session_id = event.get("session_id", "unknown")
    turn_index = event.get("turn_index", 0)
    timestamp = datetime.now(timezone.utc).isoformat()

    # Log to stderr (captured by runtime, not parsed as protocol output)
    log_entry = {
        "timestamp": timestamp,
        "phase": phase,
        "session_id": session_id,
        "turn_index": turn_index,
    }
    print(json.dumps(log_entry), file=sys.stderr)

    # Acknowledge (observe hooks always succeed)
    print(json.dumps({"ack": True}))


if __name__ == "__main__":
    main()
