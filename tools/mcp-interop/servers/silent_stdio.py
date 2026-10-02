"""A handshake-era stdio MCP server that ignores requests it does not know,
server/discover among them, as some older servers do. The client must fall
back to the initialize handshake rather than wait out its init timeout.
"""
import sys, json
for line in sys.stdin:
    m = json.loads(line)
    meth, i = m.get("method"), m.get("id")
    if meth == "initialize":
        r = {"protocolVersion": "2025-11-25", "capabilities": {"tools": {}}, "serverInfo": {"name": "silent", "version": "1"}}
    elif meth == "tools/list":
        r = {"tools": [{"name": "t", "inputSchema": {"type": "object"}}]}
    else:
        continue
    sys.stdout.write(json.dumps({"jsonrpc": "2.0", "id": i, "result": r}) + "\n"); sys.stdout.flush()
