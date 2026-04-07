FROM python:3.12-slim

WORKDIR /app

# Install system deps needed for Silero (ONNX runtime) and audio processing.
RUN apt-get update && apt-get install -y --no-install-recommends \
    libgomp1 \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Pre-download the Silero VAD model so it's baked into the image and
# the first request doesn't block on a network download.
# TODO: verify the correct download command for livekit-agents 1.4.
# In many versions `python -m livekit.plugins.silero download-files` works.
RUN python -c "from livekit.plugins import silero; silero.VAD.load()" || true

COPY app.py .

ENV PORT=8094
EXPOSE 8094

CMD ["python", "app.py"]
