# Gemini Live API - Implementation Notes

**Date**: 7 November 2025  
**Purpose**: Document Gemini Live API protocol for bidirectional streaming implementation

## Overview

The Gemini Live API (also known as Gemini 2.0 Multimodal Live API) provides real-time bidirectional streaming for audio and video input/output with low latency.

## Connection Details

### WebSocket Endpoint

```
wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1alpha.GenerativeService.BidiGenerateContent
```

**Query Parameters**:
- `key` - API key for authentication

**Example**:
```
wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1alpha.GenerativeService.BidiGenerateContent?key=YOUR_API_KEY
```

### Authentication

- API key passed as query parameter in WebSocket URL
- No additional headers required for basic authentication
- Connection upgrades from HTTP to WebSocket using standard protocol

## Message Protocol

### Message Format

All messages are JSON-encoded and sent as text frames over WebSocket.

#### Client → Server Messages

**1. Setup Message** (sent first):
```json
{
  "setup": {
    "model": "models/gemini-2.0-flash-exp",
    "generation_config": {
      "response_modalities": ["AUDIO"],
      "speech_config": {
        "voice_config": {
          "prebuilt_voice_config": {
            "voice_name": "Puck"
          }
        }
      }
    },
    "system_instruction": {
      "parts": [
        {"text": "You are a helpful assistant."}
      ]
    }
  }
}
```

**2. Client Content Message** (send audio/text):
```json
{
  "client_content": {
    "turns": [
      {
        "role": "user",
        "parts": [
          {
            "inline_data": {
              "mime_type": "audio/pcm",
              "data": "<base64-encoded-audio>"
            }
          }
        ]
      }
    ],
    "turn_complete": true
  }
}
```

**3. Realtime Input Message** (continuous audio streaming):
```json
{
  "realtime_input": {
    "media_chunks": [
      {
        "mime_type": "audio/pcm",
        "data": "<base64-encoded-audio-chunk>"
      }
    ]
  }
}
```

**4. Tool Response Message** (if tools are used):
```json
{
  "tool_response": {
    "function_responses": [
      {
        "id": "tool-call-id",
        "name": "function_name",
        "response": {
          "result": "function result"
        }
      }
    ]
  }
}
```

#### Server → Client Messages

**1. Setup Complete**:
```json
{
  "setupComplete": {}
}
```

**2. Server Content** (text/audio response):
```json
{
  "serverContent": {
    "modelTurn": {
      "parts": [
        {
          "text": "Hello! How can I help you?"
        }
      ]
    },
    "turnComplete": true
  }
}
```

**3. Server Content with Audio**:
```json
{
  "serverContent": {
    "modelTurn": {
      "parts": [
        {
          "inlineData": {
            "mimeType": "audio/pcm",
            "data": "<base64-encoded-audio>"
          }
        }
      ]
    },
    "turnComplete": false
  }
}
```

**4. Tool Call** (if model wants to call tools):
```json
{
  "toolCall": {
    "functionCalls": [
      {
        "id": "tool-call-id",
        "name": "get_weather",
        "args": {
          "location": "San Francisco"
        }
      }
    ]
  }
}
```

## Audio Configuration

### Supported Audio Formats

**Input**:
- PCM (Linear16) - 16-bit signed little-endian
- Sample rates: 16000 Hz (recommended), 24000 Hz, 48000 Hz
- Channels: 1 (mono)
- Encoding: Linear PCM, no compression

**Output**:
- PCM (Linear16) - 16-bit signed little-endian
- Sample rate: 24000 Hz (typical)
- Channels: 1 (mono)

### Audio Chunking

**Recommended chunk size**: 
- ~100ms of audio per chunk
- At 16kHz, 16-bit mono: 3200 bytes per chunk (16000 samples/sec * 0.1 sec * 2 bytes/sample)
- At 24kHz: 4800 bytes per chunk

**Base64 encoding**:
- Audio bytes must be base64-encoded before sending
- Increases size by ~33% (4/3 ratio)

## Connection Lifecycle

### 1. Connection Establishment
```
Client → Server: HTTP Upgrade request
Server → Client: 101 Switching Protocols
// WebSocket connection established
```

### 2. Setup Phase
```
Client → Server: setup message (with model config)
Server → Client: setupComplete message
```

### 3. Streaming Phase
```
Client → Server: realtime_input (continuous audio chunks)
Server → Client: serverContent (streaming responses)
// Bidirectional, interruptible
```

### 4. Interruption Handling
- Client can send new input at any time
- Server will stop current response and start processing new input
- No explicit interrupt message needed

### 5. Turn Completion
```
Client → Server: client_content with turn_complete: true
Server → Client: serverContent with turnComplete: true
```

### 6. Connection Termination
```
Client → Server: Close frame (code 1000 = normal closure)
Server → Client: Close frame
```

## Error Handling

### WebSocket Error Codes

- `1000` - Normal closure
- `1001` - Going away (endpoint unavailable)
- `1002` - Protocol error
- `1003` - Unsupported data
- `1006` - Abnormal closure (connection lost)
- `1008` - Policy violation
- `1011` - Server error

### Application-Level Errors

Errors are sent as JSON messages:
```json
{
  "error": {
    "code": 400,
    "message": "Invalid audio format",
    "status": "INVALID_ARGUMENT"
  }
}
```

**Common error codes**:
- `400` - Invalid request (bad format, unsupported config)
- `401` - Authentication failed (invalid API key)
- `429` - Rate limit exceeded
- `500` - Internal server error
- `503` - Service unavailable

### Reconnection Strategy

**Exponential backoff**:
- Initial delay: 1 second
- Max delay: 60 seconds
- Multiplier: 2x
- Add jitter: ±25%

**Retry conditions**:
- Connection errors (network issues)
- Codes 1001, 1006, 1011 (temporary failures)
- HTTP 503 (service unavailable)

**Do NOT retry**:
- Code 1008 (policy violation)
- HTTP 401 (authentication failure)
- HTTP 400 (invalid request)

## Rate Limits

### Per-Minute Limits
- Connections: 60 per minute
- Messages: 1000 per minute per connection
- Audio data: ~60 minutes of audio per minute (real-time)

### Per-Day Limits
- Connections: depends on tier
- Total audio: depends on tier

### Backpressure Handling
- If sending too fast, server may drop chunks
- Monitor for warnings in server messages
- Implement adaptive rate limiting based on RTT

## Latency Considerations

### Target Latencies
- First token: <500ms
- Subsequent tokens: 50-100ms
- End-to-end (audio in → audio out): <1000ms

### Optimization Tips
1. Use 16kHz audio (lower bandwidth than 48kHz)
2. Send ~100ms chunks (balance latency vs overhead)
3. Pipeline: start sending next chunk before previous response completes
4. Use `turn_complete: false` for continuous mode
5. Minimize base64 encoding overhead (do it efficiently)

## Voice Configuration

### Available Voices
- `Puck` - Friendly, energetic (default)
- `Charon` - Calm, professional
- `Kore` - Warm, conversational
- `Fenrir` - Deep, authoritative
- `Aoede` - Musical, expressive

### Voice Settings
```json
{
  "voice_config": {
    "prebuilt_voice_config": {
      "voice_name": "Puck"
    }
  }
}
```

## Implementation Checklist

- [ ] WebSocket connection with API key authentication
- [ ] Send setup message with model and config
- [ ] Wait for setupComplete before sending audio
- [ ] Base64 encode audio chunks before sending
- [ ] Send realtime_input messages with audio chunks
- [ ] Receive and parse serverContent messages
- [ ] Base64 decode audio from server responses
- [ ] Handle turn completion (turnComplete flag)
- [ ] Implement connection error handling
- [ ] Implement exponential backoff retry logic
- [ ] Add heartbeat/ping for connection keepalive
- [ ] Handle graceful shutdown (close frame)
- [ ] Respect rate limits
- [ ] Monitor and log latency metrics

## Security Considerations

1. **API Key Protection**
   - Never commit API keys to source control
   - Use environment variables
   - Rotate keys periodically

2. **Data Privacy**
   - Audio is transmitted to Google servers
   - Consider user consent for voice data
   - No local storage of audio by default

3. **Connection Security**
   - Always use WSS (secure WebSocket)
   - Validate server certificates
   - No fallback to unencrypted connections

## Testing Strategy

### Unit Tests
- Message encoding/decoding
- Audio chunk preparation (base64)
- Error parsing and handling

### Integration Tests (with real API)
- Connection establishment
- Setup phase completion
- Send single audio chunk, receive response
- Send continuous audio stream
- Handle interruption
- Graceful shutdown
- Error recovery (disconnect and reconnect)

### Performance Tests
- Measure latency (first token, subsequent tokens)
- Test sustained streaming (5+ minutes)
- Verify no memory leaks
- Check goroutine cleanup

## References

- [Gemini API Documentation](https://ai.google.dev/gemini-api/docs)
- [Multimodal Live API Guide](https://ai.google.dev/gemini-api/docs/live-api)
- [WebSocket Protocol RFC 6455](https://tools.ietf.org/html/rfc6455)

---

**Last Updated**: 7 November 2025  
**Status**: Ready for implementation
