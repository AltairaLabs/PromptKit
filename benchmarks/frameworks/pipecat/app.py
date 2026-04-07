"""Pipecat voice pipeline benchmark server.

Uses the real Pipecat framework with FastAPIWebsocketTransport for
multi-client support. Each WebSocket connection gets its own pipeline:
DeepgramSTTService -> OpenAILLMService -> CartesiaTTSService.

All services pointed at mock upstream servers. The benchmark harness
connects using Pipecat's native protobuf wire format (AudioRawFrame).
"""

import asyncio
import os

from fastapi import FastAPI, WebSocket

from pipecat.pipeline.pipeline import Pipeline
from pipecat.pipeline.runner import PipelineRunner
from pipecat.pipeline.task import PipelineParams, PipelineTask
from pipecat.processors.aggregators.openai_llm_context import OpenAILLMContext
from pipecat.services.cartesia.tts import CartesiaTTSService
from pipecat.services.deepgram.stt import DeepgramSTTService
from pipecat.services.openai.llm import OpenAILLMService
from pipecat.serializers.protobuf import ProtobufFrameSerializer
from pipecat.transports.websocket.fastapi import (
    FastAPIWebsocketParams,
    FastAPIWebsocketTransport,
)

STT_URL = os.environ.get("STT_URL", "ws://localhost:8082/v1/listen")
LLM_URL = os.environ.get("OPENAI_BASE_URL", "http://localhost:8081/v1")
TTS_URL = os.environ.get("TTS_URL", "ws://localhost:8083/tts/ws")
PORT = int(os.environ.get("PORT", "8092"))

app = FastAPI()


async def run_pipeline_for_client(websocket: WebSocket):
    """Create and run a full Pipecat pipeline for a single client connection."""
    transport = FastAPIWebsocketTransport(
        websocket=websocket,
        params=FastAPIWebsocketParams(
            audio_in_enabled=True,
            audio_out_enabled=True,
            audio_in_sample_rate=16000,
            audio_out_sample_rate=24000,
            serializer=ProtobufFrameSerializer(),
        ),
    )

    # Deepgram SDK appends /v1/listen internally, so base_url should be just the host
    stt_base = STT_URL.replace("/v1/listen", "")
    stt = DeepgramSTTService(
        api_key="fake-key",
        base_url=stt_base,
    )

    llm = OpenAILLMService(
        api_key="fake-key",
        base_url=LLM_URL,
        settings=OpenAILLMService.Settings(model="gpt-4o"),
    )

    tts = CartesiaTTSService(
        api_key="fake-key",
        url=TTS_URL,
        settings=CartesiaTTSService.Settings(voice="benchmark-voice"),
    )

    context = OpenAILLMContext(
        messages=[{"role": "system", "content": "You are a helpful assistant."}],
    )
    context_aggregator = llm.create_context_aggregator(context)

    pipeline = Pipeline(
        [
            transport.input(),
            stt,
            context_aggregator.user(),
            llm,
            tts,
            transport.output(),
            context_aggregator.assistant(),
        ]
    )

    task = PipelineTask(
        pipeline,
        params=PipelineParams(allow_interruptions=True),
    )
    runner = PipelineRunner()

    await task.queue_frames([context_aggregator.user().get_context_frame()])
    await runner.run(task)


@app.websocket("/ws")
async def websocket_endpoint(websocket: WebSocket):
    await websocket.accept()
    await run_pipeline_for_client(websocket)


@app.get("/health")
async def health():
    return {"status": "ok"}


if __name__ == "__main__":
    import uvicorn

    print(f"Pipecat server listening on 0.0.0.0:{PORT}", flush=True)
    uvicorn.run(app, host="0.0.0.0", port=PORT)
