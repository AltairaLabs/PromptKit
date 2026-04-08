"""LangChain streaming benchmark endpoint."""

import json
import os
import httpx
from fastapi import FastAPI, Request
from fastapi.responses import StreamingResponse
from langchain_openai import ChatOpenAI
from langchain_core.messages import AIMessage, HumanMessage, ToolMessage

app = FastAPI()

OPENAI_BASE_URL = os.environ.get("OPENAI_BASE_URL", "http://localhost:8081/v1")
OPENAI_API_KEY = os.environ.get("OPENAI_API_KEY", "sk-bench-fake")
TOOL_URL = os.environ.get("TOOL_URL", "http://localhost:8085")


@app.post("/v1/chat/completions")
async def chat_completions(request: Request):
    body = await request.json()
    messages = body.get("messages", [])
    stream = body.get("stream", False)

    if not messages:
        return {"error": "no messages"}

    llm = ChatOpenAI(
        model="gpt-4o",
        openai_api_base=OPENAI_BASE_URL,
        openai_api_key=OPENAI_API_KEY,
        streaming=True,
        temperature=0.7,
        max_tokens=256,
    )

    user_content = messages[-1]["content"]
    lc_messages = [HumanMessage(content=user_content)]

    if not stream:
        result = await llm.ainvoke(lc_messages)
        return {
            "choices": [{
                "message": {"role": "assistant", "content": result.content},
                "finish_reason": "stop",
            }]
        }

    async def generate():
        async for chunk in llm.astream(lc_messages):
            data = {
                "choices": [{
                    "delta": {"content": chunk.content},
                    "finish_reason": None,
                }]
            }
            yield f"data: {json.dumps(data)}\n\n"
        stop = {"choices": [{"delta": {}, "finish_reason": "stop"}]}
        yield f"data: {json.dumps(stop)}\n\n"
        yield "data: [DONE]\n\n"

    return StreamingResponse(generate(), media_type="text/event-stream")


@app.post("/v1/chat/completions/tools")
async def chat_completions_tools(request: Request):
    body = await request.json()
    messages = body.get("messages", [])

    if not messages:
        return {"error": "no messages"}

    llm = ChatOpenAI(
        model="gpt-4o",
        openai_api_base=OPENAI_BASE_URL,
        openai_api_key=OPENAI_API_KEY,
        temperature=0.7,
        max_tokens=256,
    )

    user_content = messages[-1]["content"]
    lc_messages = [HumanMessage(content=user_content)]

    # First LLM call — expects tool_calls response (non-streaming)
    response = await llm.ainvoke(lc_messages)
    lc_messages.append(response)

    # Execute tool calls if present
    if response.tool_calls:
        for tc in response.tool_calls:
            async with httpx.AsyncClient() as client:
                tool_resp = await client.post(
                    f"{TOOL_URL}/tool",
                    json=tc["args"],
                )
                tool_result = tool_resp.text
            lc_messages.append(
                ToolMessage(content=tool_result, tool_call_id=tc["id"])
            )

    # Second LLM call — streaming response
    async def generate():
        async for chunk in llm.astream(lc_messages):
            if isinstance(chunk.content, str) and chunk.content:
                data = {
                    "choices": [{
                        "delta": {"content": chunk.content},
                        "finish_reason": None,
                    }]
                }
                yield f"data: {json.dumps(data)}\n\n"
        stop = {"choices": [{"delta": {}, "finish_reason": "stop"}]}
        yield f"data: {json.dumps(stop)}\n\n"
        yield "data: [DONE]\n\n"

    return StreamingResponse(generate(), media_type="text/event-stream")


@app.get("/health")
async def health():
    return {"status": "ok"}


if __name__ == "__main__":
    import uvicorn
    port = int(os.environ.get("PORT", "8091"))
    uvicorn.run(app, host="0.0.0.0", port=port)
