"""An MCP server built with the official Python SDK (mcp 2.2.0), for the
interop matrix. ask uses ctx.elicit, which this SDK supports only on
handshake-era sessions; ask2 elicits through a resolver, which works in both
eras.

    python_server.py stdio | http <port> | stateless <port>
"""
import sys, base64
from typing import Annotated
from pydantic import BaseModel
from mcp.server.mcpserver import MCPServer, Context, Resolve, Elicit
from mcp.server.mcpserver.utilities.types import Image

app = MCPServer("py-real")

@app.tool()
def echo(text: str) -> str:
    """Echo text back."""
    return text

class Sum(BaseModel):
    total: int

@app.tool()
def add(a: int, b: int) -> Sum:
    """Add two ints (structured output)."""
    return Sum(total=a + b)

@app.tool()
def image() -> Image:
    """Return a tiny PNG."""
    return Image(data=base64.b64decode("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="), format="png")

class Pref(BaseModel):
    color: str

@app.tool()
async def ask(ctx: Context) -> str:
    """Elicit a color (handshake-era API)."""
    r = await ctx.elicit("favourite color?", Pref)
    return f"{r.action}:{getattr(getattr(r,'data',None),'color',None)}"

def _pick(ctx: Context):
    return Elicit("favourite color?", Pref)

@app.tool()
def ask2(pref: Annotated[Pref, Resolve(_pick)]) -> str:
    """Elicit a color via a resolver (both eras)."""
    return f"resolved:{pref.color}"

@app.tool()
def boom() -> str:
    """Always fails."""
    raise ValueError("kaboom")

if __name__ == "__main__":
    mode = sys.argv[1] if len(sys.argv) > 1 else "stdio"
    if mode == "stdio":
        app.run(transport="stdio")
    else:
        app.run(transport="streamable-http", host="127.0.0.1", port=int(sys.argv[2]), stateless_http=(mode == "stateless"))
