"""Pipecat voice pipeline benchmark endpoint.

Minimal Pipecat pipeline: Deepgram STT -> OpenAI LLM -> Cartesia TTS.
WebSocket transport for audio I/O. Follows Pipecat's simple-chatbot pattern.

NOTE: The exact Pipecat API may differ based on the pinned version.
This follows the simple-chatbot example structure. Service constructor
parameter names (e.g., base_url vs url) should be verified against
Pipecat docs at implementation time and adjusted as needed.
"""

import asyncio
import os

from pipecat.pipeline.pipeline import Pipeline
from pipecat.pipeline.runner import PipelineRunner
from pipecat.pipeline.task import PipelineParams, PipelineTask
from pipecat.processors.aggregators.openai_llm_context import OpenAILLMContext
from pipecat.services.deepgram import DeepgramSTTService
from pipecat.services.openai import OpenAILLMService
from pipecat.services.cartesia import CartesiaTTSService
from pipecat.transports.websocket_server import (
    WebsocketServerParams,
    WebsocketServerTransport,
)

STT_URL = os.environ.get("STT_URL", "ws://localhost:8082/v1/listen")
LLM_URL = os.environ.get("OPENAI_BASE_URL", "http://localhost:8081/v1")
TTS_URL = os.environ.get("TTS_URL", "ws://localhost:8083/tts/ws")
PORT = int(os.environ.get("PORT", "8092"))


async def run_pipeline():
    transport = WebsocketServerTransport(
        params=WebsocketServerParams(
            host="0.0.0.0",
            port=PORT,
            audio_in_enabled=True,
            audio_out_enabled=True,
        )
    )

    stt = DeepgramSTTService(api_key="fake-key", url=STT_URL)
    llm = OpenAILLMService(api_key="fake-key", base_url=LLM_URL, model="gpt-4o")
    tts = CartesiaTTSService(api_key="fake-key", url=TTS_URL, voice_id="benchmark-voice")

    context = OpenAILLMContext(
        messages=[{"role": "system", "content": "You are a helpful assistant."}],
    )
    context_aggregator = llm.create_context_aggregator(context)

    pipeline = Pipeline([
        transport.input(),
        stt,
        context_aggregator.user(),
        llm,
        tts,
        transport.output(),
        context_aggregator.assistant(),
    ])

    task = PipelineTask(pipeline, PipelineParams(allow_interruptions=True))
    runner = PipelineRunner()

    @transport.event_handler("on_client_connected")
    async def on_client_connected(transport, client):
        await task.queue_frames([context_aggregator.user().get_context_frame()])

    await runner.run(task)


if __name__ == "__main__":
    asyncio.run(run_pipeline())
