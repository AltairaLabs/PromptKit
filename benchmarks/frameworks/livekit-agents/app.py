"""LiveKit Agents voice pipeline benchmark server.

Uses the real livekit-agents framework with AgentSession and fake I/O
to bypass the LiveKit server entirely, per the pattern used in
livekit/agents tests (tests/fake_io.py, tests/fake_session.py).

Each WebSocket connection gets its own AgentSession:
  FakeAudioInput -> Silero VAD -> OpenAI STT -> OpenAI LLM -> OpenAI TTS -> CollectingAudioOutput

Wire protocol (matches benchmark harness in harness/voice.go):
  - Incoming binary WS frames: raw 16-bit PCM, 16kHz mono, 20ms (640 bytes)
  - Incoming text frame {"type": "end_audio"}: signals end of user speech input
  - Outgoing binary WS frames: raw 16-bit PCM audio from TTS
  - Outgoing text frame {"type": "done"}: signals pipeline completion

All AI services are pointed at mock upstream URLs via environment variables.
"""

from __future__ import annotations

import asyncio
import json
import logging
import os
import time
from typing import AsyncIterator

import uvicorn
from fastapi import FastAPI, WebSocket, WebSocketDisconnect
from livekit import rtc
from livekit.agents import Agent, AgentSession, utils
from livekit.agents.llm import ChatContext, ChatMessage
from livekit.agents.voice.io import (
    AudioInput,
    AudioOutput,
    AudioOutputCapabilities,
)
from livekit.plugins import openai as openai_plugin
from livekit.plugins import silero

logger = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------

PORT = int(os.environ.get("PORT", "8094"))
OPENAI_BASE_URL = os.environ.get("OPENAI_BASE_URL", "http://localhost:8081/v1")
# STT_URL and TTS_URL are used as base_url for their respective OpenAI plugin clients.
# The OpenAI STT plugin uses /audio/transcriptions; TTS uses /audio/speech.
STT_URL = os.environ.get("STT_URL", OPENAI_BASE_URL)
TTS_URL = os.environ.get("TTS_URL", OPENAI_BASE_URL)

SAMPLE_RATE = 16000
NUM_CHANNELS = 1
SAMPLES_PER_FRAME = 640  # 20ms at 16kHz


# ---------------------------------------------------------------------------
# Fake Audio I/O (adapted from livekit/agents tests/fake_io.py)
# ---------------------------------------------------------------------------


class PushableAudioInput(AudioInput):
    """AudioInput that accepts frames pushed from outside (e.g., WebSocket)."""

    def __init__(self) -> None:
        super().__init__(label="WebSocketInput")
        self._ch: utils.aio.Chan[rtc.AudioFrame] = utils.aio.Chan()

    async def __anext__(self) -> rtc.AudioFrame:
        return await self._ch.__anext__()

    def push(self, frame: rtc.AudioFrame) -> None:
        self._ch.send_nowait(frame)

    def close(self) -> None:
        self._ch.close()


class CollectingAudioOutput(AudioOutput):
    """AudioOutput that collects frames and exposes them as an async iterator.

    The session writes TTS audio here via capture_frame(). We forward each
    frame to the WebSocket as a binary message in real time.
    """

    def __init__(self) -> None:
        super().__init__(
            label="WebSocketOutput",
            capabilities=AudioOutputCapabilities(pause=False),
            sample_rate=None,  # accept any sample rate
        )
        self._ch: utils.aio.Chan[rtc.AudioFrame] = utils.aio.Chan()
        self._pushed_duration = 0.0
        self._start_time = 0.0
        self._flush_handle: asyncio.TimerHandle | None = None

    async def capture_frame(self, frame: rtc.AudioFrame) -> None:
        await super().capture_frame(frame)
        if not self._pushed_duration:
            self._start_time = time.time()
            self.on_playback_started(created_at=self._start_time)
        self._pushed_duration += frame.duration
        self._ch.send_nowait(frame)

    def flush(self) -> None:
        super().flush()
        if not self._pushed_duration:
            return

        def _done() -> None:
            self.on_playback_finished(
                playback_position=self._pushed_duration,
                interrupted=False,
                synchronized_transcript=None,
            )
            self._pushed_duration = 0.0

        delay = max(0.0, self._pushed_duration - (time.time() - self._start_time))
        if self._flush_handle:
            self._flush_handle.cancel()
        self._flush_handle = asyncio.get_event_loop().call_later(delay, _done)

    def clear_buffer(self) -> None:
        if not self._pushed_duration:
            return
        if self._flush_handle:
            self._flush_handle.cancel()
        self._flush_handle = None
        self.on_playback_finished(
            playback_position=min(self._pushed_duration, time.time() - self._start_time),
            interrupted=True,
            synchronized_transcript=None,
        )
        self._pushed_duration = 0.0

    async def frames(self) -> AsyncIterator[rtc.AudioFrame]:
        """Async iterator over collected output frames."""
        async for frame in self._ch:
            yield frame

    def close(self) -> None:
        self._ch.close()


# ---------------------------------------------------------------------------
# Agent definition
# ---------------------------------------------------------------------------


class BenchmarkAgent(Agent):
    """Minimal voice agent for benchmarking."""

    def __init__(self) -> None:
        super().__init__(
            instructions="You are a helpful assistant. Keep responses brief.",
        )


# ---------------------------------------------------------------------------
# FastAPI app + WebSocket handler
# ---------------------------------------------------------------------------

app = FastAPI()


@app.get("/health")
async def health() -> dict:
    return {"status": "ok"}


@app.websocket("/ws")
async def websocket_endpoint(websocket: WebSocket) -> None:
    await websocket.accept()

    audio_input = PushableAudioInput()
    audio_output = CollectingAudioOutput()

    # Build the AgentSession with real plugins pointed at mock upstreams.
    # Silero VAD runs locally (ONNX model, no network calls).
    vad = silero.VAD.load(
        min_speech_duration=0.05,
        min_silence_duration=0.3,
        sample_rate=SAMPLE_RATE,
    )

    stt = openai_plugin.STT(
        base_url=STT_URL,
        api_key="fake-key",
        # TODO: verify exact kwarg name; in some plugin versions it may be
        #       `base_url` or passed via a pre-constructed `openai.AsyncClient`.
    )

    llm = openai_plugin.LLM(
        model="gpt-4o",
        base_url=OPENAI_BASE_URL,
        api_key="fake-key",
    )

    tts = openai_plugin.TTS(
        model="tts-1",
        voice="alloy",
        base_url=TTS_URL,
        api_key="fake-key",
        response_format="pcm",  # raw 16-bit PCM, no container overhead
    )

    session = AgentSession(
        vad=vad,
        stt=stt,
        llm=llm,
        tts=tts,
    )

    # Wire fake I/O BEFORE calling session.start() so RoomIO is never created.
    session.input.audio = audio_input
    session.output.audio = audio_output

    # Task: forward output audio frames to the WebSocket as binary messages.
    async def forward_output() -> None:
        try:
            async for frame in audio_output.frames():
                await websocket.send_bytes(bytes(frame.data))
        except Exception:
            pass

    output_task = asyncio.create_task(forward_output())

    # Start the session (no room= argument → uses the pre-set fake I/O).
    agent = BenchmarkAgent()
    session_task = asyncio.create_task(session.start(agent))

    try:
        # Receive loop: binary = PCM frames, text = control messages.
        while True:
            try:
                msg = await websocket.receive()
            except WebSocketDisconnect:
                break

            if "bytes" in msg and msg["bytes"] is not None:
                data = msg["bytes"]
                num_samples = len(data) // 2  # 16-bit samples
                frame = rtc.AudioFrame(
                    data=data,
                    sample_rate=SAMPLE_RATE,
                    num_channels=NUM_CHANNELS,
                    samples_per_channel=num_samples,
                )
                audio_input.push(frame)

            elif "text" in msg and msg["text"] is not None:
                try:
                    ctrl = json.loads(msg["text"])
                except json.JSONDecodeError:
                    continue
                if ctrl.get("type") == "end_audio":
                    # Signal that no more input is coming; close the input channel.
                    audio_input.close()
                    break

    finally:
        # Drain and shut down the session.
        try:
            await asyncio.wait_for(session.drain(), timeout=10.0)
        except (asyncio.TimeoutError, RuntimeError):
            pass

        await session.aclose()

        # Close the output channel so forward_output() terminates.
        audio_output.close()
        output_task.cancel()
        try:
            await output_task
        except asyncio.CancelledError:
            pass

        session_task.cancel()
        try:
            await asyncio.wait_for(session_task, timeout=2.0)
        except (asyncio.CancelledError, asyncio.TimeoutError):
            pass

        # Send the done signal to the harness.
        try:
            await websocket.send_text(json.dumps({"type": "done"}))
        except Exception:
            pass


if __name__ == "__main__":
    logging.basicConfig(level=logging.INFO)
    print(f"LiveKit Agents benchmark server listening on 0.0.0.0:{PORT}", flush=True)
    uvicorn.run(app, host="0.0.0.0", port=PORT, log_level="warning")
