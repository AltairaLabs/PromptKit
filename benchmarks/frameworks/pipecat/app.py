"""Python asyncio voice pipeline benchmark endpoint.

Coordinates STT -> LLM -> TTS using Python asyncio, matching the same
WebSocket protocol as the PromptKit round2 server. This measures Python's
async runtime overhead for voice pipeline coordination — the same work
Pipecat does internally, without framework-specific transport overhead.

Same protocol, same pipeline logic, different language runtime.
"""

import asyncio
import json
import os

import aiohttp
import websockets.client
from fastapi import FastAPI, WebSocket

app = FastAPI()

LLM_URL = os.environ.get("OPENAI_BASE_URL", "http://localhost:8081/v1")
STT_URL = os.environ.get("STT_URL", "ws://localhost:8082/v1/listen")
TTS_URL = os.environ.get("TTS_URL", "ws://localhost:8083/tts/ws")


@app.websocket("/")
async def voice_session(ws: WebSocket):
    await ws.accept()

    # Phase 1: Collect audio frames from client
    audio_frames = []
    while True:
        msg = await ws.receive()
        if msg.get("bytes"):
            audio_frames.append(msg["bytes"])
        elif msg.get("text"):
            data = json.loads(msg["text"])
            if data.get("type") == "end_audio":
                break

    # Phase 2: Send audio to STT, get transcript
    transcript = await call_stt(audio_frames)

    # Phase 3: Send transcript to LLM, get streamed response
    llm_text = await call_llm(transcript)

    # Phase 4: Send text to TTS, relay audio back to client
    await call_tts_and_relay(ws, llm_text)

    # Signal done
    await ws.send_json({"type": "done"})


async def call_stt(audio_frames: list[bytes]) -> str:
    async with websockets.client.connect(STT_URL) as conn:
        for frame in audio_frames:
            await conn.send(frame)
        await conn.send(json.dumps({"type": "CloseStream"}))

        async for msg in conn:
            evt = json.loads(msg)
            if (
                evt.get("type") == "Results"
                and evt.get("is_final")
                and evt.get("channel", {}).get("alternatives")
            ):
                return evt["channel"]["alternatives"][0]["transcript"]

    return ""


async def call_llm(prompt: str) -> str:
    body = {
        "model": "gpt-4o",
        "messages": [{"role": "user", "content": prompt}],
        "stream": True,
    }
    chunks = []
    async with aiohttp.ClientSession() as session:
        async with session.post(
            f"{LLM_URL}/chat/completions",
            json=body,
            headers={"Content-Type": "application/json"},
        ) as resp:
            async for line in resp.content:
                line = line.decode().strip()
                if not line.startswith("data: "):
                    continue
                payload = line[6:]
                if payload == "[DONE]":
                    break
                chunk = json.loads(payload)
                if chunk.get("choices") and chunk["choices"][0].get("delta", {}).get(
                    "content"
                ):
                    chunks.append(chunk["choices"][0]["delta"]["content"])

    return "".join(chunks)


async def call_tts_and_relay(ws: WebSocket, text: str):
    async with websockets.client.connect(TTS_URL) as conn:
        await conn.send(json.dumps({"text": text, "voice_id": "benchmark"}))

        async for msg in conn:
            if isinstance(msg, bytes):
                await ws.send_bytes(msg)
            else:
                evt = json.loads(msg)
                if evt.get("type") == "done":
                    break


@app.get("/health")
async def health():
    return {"status": "ok"}


if __name__ == "__main__":
    import uvicorn

    port = int(os.environ.get("PORT", "8092"))
    uvicorn.run(app, host="0.0.0.0", port=port)
