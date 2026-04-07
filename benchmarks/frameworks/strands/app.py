"""Strands Agents streaming benchmark endpoint.

Minimal FastAPI app wrapping Strands Agent with OpenAI-compatible model.
Follows the documented stream_async pattern from Strands docs.
"""

import json
import os

from fastapi import FastAPI, Request
from fastapi.responses import StreamingResponse
from strands import Agent
from strands.models.openai import OpenAIModel

app = FastAPI()

OPENAI_BASE_URL = os.environ.get("OPENAI_BASE_URL", "http://localhost:8081/v1")
OPENAI_API_KEY = os.environ.get("OPENAI_API_KEY", "sk-bench-fake")


def make_model():
    return OpenAIModel(
        client_args={
            "api_key": OPENAI_API_KEY,
            "base_url": OPENAI_BASE_URL,
        },
        model_id="gpt-4o",
        params={
            "max_tokens": 256,
            "temperature": 0.7,
        },
    )


@app.post("/v1/chat/completions")
async def chat_completions(request: Request):
    body = await request.json()
    messages = body.get("messages", [])
    stream = body.get("stream", False)

    if not messages:
        return {"error": "no messages"}

    user_content = messages[-1]["content"]

    if not stream:
        agent = Agent(model=make_model(), callback_handler=None)
        result = agent(user_content)
        return {
            "choices": [{
                "message": {"role": "assistant", "content": str(result)},
                "finish_reason": "stop",
            }]
        }

    agent = Agent(model=make_model(), callback_handler=None)

    async def generate():
        async for event in agent.stream_async(user_content):
            if "data" in event:
                data = {
                    "choices": [{
                        "delta": {"content": event["data"]},
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
    port = int(os.environ.get("PORT", "8093"))
    uvicorn.run(app, host="0.0.0.0", port=port)
