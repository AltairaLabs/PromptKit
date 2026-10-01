#!/usr/bin/env bash
#
# Runs PromptKit's MCP client against real MCP servers built with the
# official SDKs, over every transport and both protocol eras, and prints what
# each tool call delivered to the model.
#
# The official conformance suite (make mcp-conformance) checks the client
# against scripted scenarios; this checks it against real implementations,
# which is how the SSE endpoint, 4xx-retry and stdio-shutdown bugs were found
# that the suite missed.
#
# Servers (pinned): TypeScript server-everything and server-filesystem, the
# Python SDK, and the Go SDK (goserver/). Python 3, Node and npm are needed;
# they are installed into .cache/ on first run.
#
# Usage: tools/mcp-interop/run.sh            (or: make mcp-interop)
#   MCP_INTEROP_PORT_BASE  first of the local ports used (default 18100)
#
# Exit status is non-zero if any server cannot be connected to or listed.
# Tool calls that fail are reported, not fatal: some fail by design (boom),
# and some document known gaps (see README.md).
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
CACHE="$DIR/.cache"
PY_SDK_VERSION="2.2.0"
NODE_SERVERS_VERSION="2026.8.31"
BASE="${MCP_INTEROP_PORT_BASE:-18100}"

mkdir -p "$CACHE/bin"
if [ ! -x "$CACHE/venv/bin/python" ]; then
  echo "Installing the Python MCP SDK $PY_SDK_VERSION into $CACHE/venv"
  python3 -m venv "$CACHE/venv"
  "$CACHE/venv/bin/pip" install -q --disable-pip-version-check "mcp==$PY_SDK_VERSION"
fi
if [ ! -x "$CACHE/node/node_modules/.bin/mcp-server-everything" ]; then
  echo "Installing the TypeScript reference servers $NODE_SERVERS_VERSION into $CACHE/node"
  mkdir -p "$CACHE/node"
  npm install --silent --prefix "$CACHE/node" \
    "@modelcontextprotocol/server-everything@$NODE_SERVERS_VERSION" \
    "@modelcontextprotocol/server-filesystem@$NODE_SERVERS_VERSION" >/dev/null
fi
go -C "$DIR" build -o "$CACHE/bin/probe" ./probe
go -C "$DIR" build -o "$CACHE/bin/goserver" ./goserver

PY="$CACHE/venv/bin/python"
TS_EVERYTHING="$CACHE/node/node_modules/.bin/mcp-server-everything"
TS_FILESYSTEM="$CACHE/node/node_modules/.bin/mcp-server-filesystem"
PROBE="$CACHE/bin/probe"
GOSERVER="$CACHE/bin/goserver"

PIDS=()
cleanup() { for p in "${PIDS[@]}"; do kill "$p" 2>/dev/null || true; done; }
trap cleanup EXIT

# serve <name> <command...> starts a server in the background, logging to .cache.
serve() {
  local name="$1"; shift
  "$@" >"$CACHE/server-$name.log" 2>&1 &
  PIDS+=($!)
}
serve py-http "$PY" "$DIR/servers/python_server.py" http $((BASE + 1))
serve py-stateless "$PY" "$DIR/servers/python_server.py" stateless $((BASE + 2))
serve go-http "$GOSERVER" http $((BASE + 3))
serve go-stateless "$GOSERVER" stateless $((BASE + 4))
serve go-sse "$GOSERVER" sse $((BASE + 5))
serve ts-http env PORT=$((BASE + 6)) "$TS_EVERYTHING" streamableHttp
serve ts-sse env PORT=$((BASE + 7)) "$TS_EVERYTHING" sse

for port in $(seq $((BASE + 1)) $((BASE + 7))); do
  for _ in $(seq 1 100); do
    (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null && break
    sleep 0.1
  done
done

FSROOT="$CACHE/fsroot"
mkdir -p "$FSROOT"
echo "hello fs" >"$FSROOT/a.txt"

GO_TOOLS='echo={"text":"hi"};add={"a":2,"b":3};image;ask;ask2;boom'
TS_TOOLS='echo={"message":"hi"};get-structured-content={"location":"Chicago"};get-tiny-image;get-resource-reference={"resourceType":"Blob","resourceId":2};trigger-elicitation-request;trigger-long-running-operation={"duration":1,"steps":2};simulate-research-query={"topic":"x"}'
FS_TOOLS='read_text_file={"path":"'"$FSROOT"'/a.txt"};read_text_file={"path":"/etc/hostname"}'

failed=()
probe() {
  local tools="$1"; shift
  if ! TOOLS="$tools" "$PROBE" "$@" 2>/dev/null; then
    failed+=("$1")
  fi
  echo
}

probe "$GO_TOOLS" py-stdio stdio "$PY" "$DIR/servers/python_server.py" stdio
probe "$GO_TOOLS" py-http streamable "http://127.0.0.1:$((BASE + 1))/mcp"
probe "$GO_TOOLS" py-stateless streamable "http://127.0.0.1:$((BASE + 2))/mcp"
probe "$GO_TOOLS" go-stdio stdio "$GOSERVER" stdio
probe "$GO_TOOLS" go-http streamable "http://127.0.0.1:$((BASE + 3))/"
probe "$GO_TOOLS" go-stateless streamable "http://127.0.0.1:$((BASE + 4))/"
probe "$GO_TOOLS" go-sse sse "http://127.0.0.1:$((BASE + 5))/"
probe "$GO_TOOLS" go-urlonly auto "http://127.0.0.1:$((BASE + 5))/"
probe "$TS_TOOLS" ts-stdio stdio "$TS_EVERYTHING" stdio
probe "$TS_TOOLS" ts-http streamable "http://127.0.0.1:$((BASE + 6))/mcp"
probe "$TS_TOOLS" ts-sse sse "http://127.0.0.1:$((BASE + 7))/sse"
probe "$TS_TOOLS" ts-urlonly auto "http://127.0.0.1:$((BASE + 7))/sse"
probe "$FS_TOOLS" ts-fs stdio "$TS_FILESYSTEM" "$FSROOT"
probe "" silent-stdio stdio python3 "$DIR/servers/silent_stdio.py"
LEGACY=1 probe "$GO_TOOLS" py-stdio-handshake stdio "$PY" "$DIR/servers/python_server.py" stdio
LEGACY=1 probe "$GO_TOOLS" go-stdio-handshake stdio "$GOSERVER" stdio

if [ ${#failed[@]} -gt 0 ]; then
  echo "✗ could not connect to or list tools on: ${failed[*]}" >&2
  exit 1
fi
echo "✓ every server connected and listed its tools"
