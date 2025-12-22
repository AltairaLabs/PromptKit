package openai

import (
	"encoding/json"
	"testing"
)

func TestParseServerEvent(t *testing.T) {
	tests := []struct {
		name      string
		data      string
		wantType  string
		wantError bool
	}{
		{
			name: "session.created event",
			data: `{
				"event_id": "evt_001",
				"type": "session.created",
				"session": {
					"id": "sess_001",
					"object": "realtime.session",
					"model": "gpt-4o-realtime-preview",
					"modalities": ["text", "audio"],
					"voice": "alloy"
				}
			}`,
			wantType: "session.created",
		},
		{
			name: "session.updated event",
			data: `{
				"event_id": "evt_002",
				"type": "session.updated",
				"session": {
					"id": "sess_001",
					"model": "gpt-4o-realtime-preview"
				}
			}`,
			wantType: "session.updated",
		},
		{
			name: "error event",
			data: `{
				"event_id": "evt_003",
				"type": "error",
				"error": {
					"type": "invalid_request_error",
					"code": "invalid_model",
					"message": "The model specified does not exist."
				}
			}`,
			wantType: "error",
		},
		{
			name: "response.text.delta event",
			data: `{
				"event_id": "evt_004",
				"type": "response.text.delta",
				"response_id": "resp_001",
				"item_id": "item_001",
				"output_index": 0,
				"content_index": 0,
				"delta": "Hello"
			}`,
			wantType: "response.text.delta",
		},
		{
			name: "response.text.done event",
			data: `{
				"event_id": "evt_004b",
				"type": "response.text.done",
				"response_id": "resp_001",
				"item_id": "item_001",
				"output_index": 0,
				"content_index": 0,
				"text": "Hello world"
			}`,
			wantType: "response.text.done",
		},
		{
			name: "response.audio.delta event",
			data: `{
				"event_id": "evt_005",
				"type": "response.audio.delta",
				"response_id": "resp_001",
				"item_id": "item_001",
				"output_index": 0,
				"content_index": 0,
				"delta": "SGVsbG8gV29ybGQ="
			}`,
			wantType: "response.audio.delta",
		},
		{
			name: "response.audio.done event",
			data: `{
				"event_id": "evt_005b",
				"type": "response.audio.done",
				"response_id": "resp_001",
				"item_id": "item_001",
				"output_index": 0,
				"content_index": 0
			}`,
			wantType: "response.audio.done",
		},
		{
			name: "response.audio_transcript.delta event",
			data: `{
				"event_id": "evt_005c",
				"type": "response.audio_transcript.delta",
				"response_id": "resp_001",
				"item_id": "item_001",
				"output_index": 0,
				"content_index": 0,
				"delta": "Hello"
			}`,
			wantType: "response.audio_transcript.delta",
		},
		{
			name: "response.audio_transcript.done event",
			data: `{
				"event_id": "evt_005d",
				"type": "response.audio_transcript.done",
				"response_id": "resp_001",
				"item_id": "item_001",
				"output_index": 0,
				"content_index": 0,
				"transcript": "Hello world"
			}`,
			wantType: "response.audio_transcript.done",
		},
		{
			name: "response.function_call_arguments.delta event",
			data: `{
				"event_id": "evt_006a",
				"type": "response.function_call_arguments.delta",
				"response_id": "resp_001",
				"item_id": "item_001",
				"output_index": 0,
				"call_id": "call_001",
				"delta": "{\"loc"
			}`,
			wantType: "response.function_call_arguments.delta",
		},
		{
			name: "response.function_call_arguments.done event",
			data: `{
				"event_id": "evt_006",
				"type": "response.function_call_arguments.done",
				"response_id": "resp_001",
				"item_id": "item_001",
				"output_index": 0,
				"call_id": "call_001",
				"name": "get_weather",
				"arguments": "{\"location\": \"New York\"}"
			}`,
			wantType: "response.function_call_arguments.done",
		},
		{
			name: "response.created event",
			data: `{
				"event_id": "evt_006b",
				"type": "response.created",
				"response": {
					"id": "resp_001",
					"object": "realtime.response",
					"status": "in_progress"
				}
			}`,
			wantType: "response.created",
		},
		{
			name: "response.done event",
			data: `{
				"event_id": "evt_007",
				"type": "response.done",
				"response": {
					"id": "resp_001",
					"object": "realtime.response",
					"status": "completed",
					"usage": {
						"total_tokens": 100,
						"input_tokens": 50,
						"output_tokens": 50
					}
				}
			}`,
			wantType: "response.done",
		},
		{
			name: "response.output_item.added event",
			data: `{
				"event_id": "evt_007a",
				"type": "response.output_item.added",
				"response_id": "resp_001",
				"output_index": 0,
				"item": {
					"id": "item_001",
					"type": "message",
					"role": "assistant"
				}
			}`,
			wantType: "response.output_item.added",
		},
		{
			name: "response.output_item.done event",
			data: `{
				"event_id": "evt_007b",
				"type": "response.output_item.done",
				"response_id": "resp_001",
				"output_index": 0,
				"item": {
					"id": "item_001",
					"type": "message",
					"role": "assistant"
				}
			}`,
			wantType: "response.output_item.done",
		},
		{
			name: "response.content_part.added event",
			data: `{
				"event_id": "evt_007c",
				"type": "response.content_part.added",
				"response_id": "resp_001",
				"item_id": "item_001",
				"output_index": 0,
				"content_index": 0,
				"part": {
					"type": "text",
					"text": ""
				}
			}`,
			wantType: "response.content_part.added",
		},
		{
			name: "response.content_part.done event",
			data: `{
				"event_id": "evt_007d",
				"type": "response.content_part.done",
				"response_id": "resp_001",
				"item_id": "item_001",
				"output_index": 0,
				"content_index": 0,
				"part": {
					"type": "text",
					"text": "Hello"
				}
			}`,
			wantType: "response.content_part.done",
		},
		{
			name: "input_audio_buffer.committed event",
			data: `{
				"event_id": "evt_008a",
				"type": "input_audio_buffer.committed",
				"previous_item_id": "item_000",
				"item_id": "item_001"
			}`,
			wantType: "input_audio_buffer.committed",
		},
		{
			name: "input_audio_buffer.cleared event",
			data: `{
				"event_id": "evt_008b",
				"type": "input_audio_buffer.cleared"
			}`,
			wantType: "input_audio_buffer.cleared",
		},
		{
			name: "conversation.item.created event",
			data: `{
				"event_id": "evt_008c",
				"type": "conversation.item.created",
				"previous_item_id": "item_000",
				"item": {
					"id": "item_001",
					"type": "message",
					"role": "user"
				}
			}`,
			wantType: "conversation.item.created",
		},
		{
			name: "rate_limits.updated event",
			data: `{
				"event_id": "evt_008",
				"type": "rate_limits.updated",
				"rate_limits": [
					{
						"name": "requests",
						"limit": 100,
						"remaining": 99,
						"reset_seconds": 60.0
					}
				]
			}`,
			wantType: "rate_limits.updated",
		},
		{
			name: "unknown event type",
			data: `{
				"event_id": "evt_009",
				"type": "some.unknown.event"
			}`,
			wantType: "some.unknown.event",
		},
		{
			name:      "invalid JSON",
			data:      `{invalid json}`,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, err := ParseServerEvent([]byte(tt.data))
			if tt.wantError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Check event type
			switch e := event.(type) {
			case *SessionCreatedEvent:
				if tt.wantType != "session.created" {
					t.Errorf("got SessionCreatedEvent, want %s", tt.wantType)
				}
				if e.Session.ID == "" {
					t.Error("expected session ID")
				}
			case *SessionUpdatedEvent:
				if tt.wantType != "session.updated" {
					t.Errorf("got SessionUpdatedEvent, want %s", tt.wantType)
				}
			case *ErrorEvent:
				if tt.wantType != "error" {
					t.Errorf("got ErrorEvent, want %s", tt.wantType)
				}
				if e.Error.Message == "" {
					t.Error("expected error message")
				}
			case *ResponseTextDeltaEvent:
				if tt.wantType != "response.text.delta" {
					t.Errorf("got ResponseTextDeltaEvent, want %s", tt.wantType)
				}
				if e.Delta == "" {
					t.Error("expected delta text")
				}
			case *ResponseTextDoneEvent:
				if tt.wantType != "response.text.done" {
					t.Errorf("got ResponseTextDoneEvent, want %s", tt.wantType)
				}
			case *ResponseAudioDeltaEvent:
				if tt.wantType != "response.audio.delta" {
					t.Errorf("got ResponseAudioDeltaEvent, want %s", tt.wantType)
				}
			case *ResponseAudioDoneEvent:
				if tt.wantType != "response.audio.done" {
					t.Errorf("got ResponseAudioDoneEvent, want %s", tt.wantType)
				}
			case *ResponseAudioTranscriptDeltaEvent:
				if tt.wantType != "response.audio_transcript.delta" {
					t.Errorf("got ResponseAudioTranscriptDeltaEvent, want %s", tt.wantType)
				}
			case *ResponseAudioTranscriptDoneEvent:
				if tt.wantType != "response.audio_transcript.done" {
					t.Errorf("got ResponseAudioTranscriptDoneEvent, want %s", tt.wantType)
				}
			case *ResponseFunctionCallArgumentsDeltaEvent:
				if tt.wantType != "response.function_call_arguments.delta" {
					t.Errorf("got ResponseFunctionCallArgumentsDeltaEvent, want %s", tt.wantType)
				}
			case *ResponseFunctionCallArgumentsDoneEvent:
				if tt.wantType != "response.function_call_arguments.done" {
					t.Errorf("got ResponseFunctionCallArgumentsDoneEvent, want %s", tt.wantType)
				}
				if e.Name == "" {
					t.Error("expected function name")
				}
			case *ResponseCreatedEvent:
				if tt.wantType != "response.created" {
					t.Errorf("got ResponseCreatedEvent, want %s", tt.wantType)
				}
			case *ResponseDoneEvent:
				if tt.wantType != "response.done" {
					t.Errorf("got ResponseDoneEvent, want %s", tt.wantType)
				}
			case *ResponseOutputItemAddedEvent:
				if tt.wantType != "response.output_item.added" {
					t.Errorf("got ResponseOutputItemAddedEvent, want %s", tt.wantType)
				}
			case *ResponseOutputItemDoneEvent:
				if tt.wantType != "response.output_item.done" {
					t.Errorf("got ResponseOutputItemDoneEvent, want %s", tt.wantType)
				}
			case *ResponseContentPartAddedEvent:
				if tt.wantType != "response.content_part.added" {
					t.Errorf("got ResponseContentPartAddedEvent, want %s", tt.wantType)
				}
			case *ResponseContentPartDoneEvent:
				if tt.wantType != "response.content_part.done" {
					t.Errorf("got ResponseContentPartDoneEvent, want %s", tt.wantType)
				}
			case *InputAudioBufferCommittedEvent:
				if tt.wantType != "input_audio_buffer.committed" {
					t.Errorf("got InputAudioBufferCommittedEvent, want %s", tt.wantType)
				}
			case *InputAudioBufferClearedEvent:
				if tt.wantType != "input_audio_buffer.cleared" {
					t.Errorf("got InputAudioBufferClearedEvent, want %s", tt.wantType)
				}
			case *ConversationItemCreatedEvent:
				if tt.wantType != "conversation.item.created" {
					t.Errorf("got ConversationItemCreatedEvent, want %s", tt.wantType)
				}
			case *RateLimitsUpdatedEvent:
				if tt.wantType != "rate_limits.updated" {
					t.Errorf("got RateLimitsUpdatedEvent, want %s", tt.wantType)
				}
				if len(e.RateLimits) == 0 {
					t.Error("expected rate limits")
				}
			case *ServerEvent:
				if e.Type != tt.wantType {
					t.Errorf("got type %s, want %s", e.Type, tt.wantType)
				}
			default:
				t.Errorf("unexpected event type: %T", event)
			}
		})
	}
}

func TestClientEventSerialization(t *testing.T) {
	tests := []struct {
		name  string
		event interface{}
		check func(t *testing.T, data []byte)
	}{
		{
			name: "SessionUpdateEvent",
			event: SessionUpdateEvent{
				ClientEvent: ClientEvent{
					EventID: "evt_001",
					Type:    "session.update",
				},
				Session: SessionConfig{
					Modalities:   []string{"text", "audio"},
					Instructions: "You are a helpful assistant.",
					Voice:        "alloy",
				},
			},
			check: func(t *testing.T, data []byte) {
				var parsed map[string]interface{}
				if err := json.Unmarshal(data, &parsed); err != nil {
					t.Fatalf("failed to unmarshal: %v", err)
				}
				if parsed["type"] != "session.update" {
					t.Errorf("expected type session.update, got %v", parsed["type"])
				}
				session := parsed["session"].(map[string]interface{})
				if session["voice"] != "alloy" {
					t.Errorf("expected voice alloy, got %v", session["voice"])
				}
			},
		},
		{
			name: "InputAudioBufferAppendEvent",
			event: InputAudioBufferAppendEvent{
				ClientEvent: ClientEvent{
					EventID: "evt_002",
					Type:    "input_audio_buffer.append",
				},
				Audio: "SGVsbG8gV29ybGQ=",
			},
			check: func(t *testing.T, data []byte) {
				var parsed map[string]interface{}
				if err := json.Unmarshal(data, &parsed); err != nil {
					t.Fatalf("failed to unmarshal: %v", err)
				}
				if parsed["audio"] != "SGVsbG8gV29ybGQ=" {
					t.Errorf("expected audio data, got %v", parsed["audio"])
				}
			},
		},
		{
			name: "ResponseCreateEvent",
			event: ResponseCreateEvent{
				ClientEvent: ClientEvent{
					EventID: "evt_003",
					Type:    "response.create",
				},
				Response: &ResponseConfig{
					Modalities: []string{"text"},
				},
			},
			check: func(t *testing.T, data []byte) {
				var parsed map[string]interface{}
				if err := json.Unmarshal(data, &parsed); err != nil {
					t.Fatalf("failed to unmarshal: %v", err)
				}
				if parsed["type"] != "response.create" {
					t.Errorf("expected type response.create, got %v", parsed["type"])
				}
			},
		},
		{
			name: "ConversationItemCreateEvent with function output",
			event: ConversationItemCreateEvent{
				ClientEvent: ClientEvent{
					EventID: "evt_004",
					Type:    "conversation.item.create",
				},
				Item: ConversationItem{
					Type:   "function_call_output",
					CallID: "call_001",
					Output: `{"temperature": 72}`,
				},
			},
			check: func(t *testing.T, data []byte) {
				var parsed map[string]interface{}
				if err := json.Unmarshal(data, &parsed); err != nil {
					t.Fatalf("failed to unmarshal: %v", err)
				}
				item := parsed["item"].(map[string]interface{})
				if item["type"] != "function_call_output" {
					t.Errorf("expected type function_call_output, got %v", item["type"])
				}
				if item["call_id"] != "call_001" {
					t.Errorf("expected call_id call_001, got %v", item["call_id"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.event)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}
			tt.check(t, data)
		})
	}
}

func TestInputAudioBufferEvents(t *testing.T) {
	t.Run("commit event", func(t *testing.T) {
		event := InputAudioBufferCommitEvent{
			ClientEvent: ClientEvent{
				EventID: "evt_001",
				Type:    "input_audio_buffer.commit",
			},
		}
		data, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if parsed["type"] != "input_audio_buffer.commit" {
			t.Errorf("expected type input_audio_buffer.commit, got %v", parsed["type"])
		}
	})

	t.Run("clear event", func(t *testing.T) {
		event := InputAudioBufferClearEvent{
			ClientEvent: ClientEvent{
				EventID: "evt_002",
				Type:    "input_audio_buffer.clear",
			},
		}
		data, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if parsed["type"] != "input_audio_buffer.clear" {
			t.Errorf("expected type input_audio_buffer.clear, got %v", parsed["type"])
		}
	})
}

func TestParseServerEvent_SpeechEvents(t *testing.T) {
	t.Run("speech_started event", func(t *testing.T) {
		data := `{
			"event_id": "evt_001",
			"type": "input_audio_buffer.speech_started",
			"audio_start_ms": 1500,
			"item_id": "item_001"
		}`

		event, err := ParseServerEvent([]byte(data))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		e, ok := event.(*InputAudioBufferSpeechStartedEvent)
		if !ok {
			t.Fatalf("expected InputAudioBufferSpeechStartedEvent, got %T", event)
		}
		if e.AudioStartMs != 1500 {
			t.Errorf("expected audio_start_ms 1500, got %d", e.AudioStartMs)
		}
	})

	t.Run("speech_stopped event", func(t *testing.T) {
		data := `{
			"event_id": "evt_002",
			"type": "input_audio_buffer.speech_stopped",
			"audio_end_ms": 3000,
			"item_id": "item_001"
		}`

		event, err := ParseServerEvent([]byte(data))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		e, ok := event.(*InputAudioBufferSpeechStoppedEvent)
		if !ok {
			t.Fatalf("expected InputAudioBufferSpeechStoppedEvent, got %T", event)
		}
		if e.AudioEndMs != 3000 {
			t.Errorf("expected audio_end_ms 3000, got %d", e.AudioEndMs)
		}
	})
}

func TestParseServerEvent_TranscriptionEvents(t *testing.T) {
	t.Run("transcription completed", func(t *testing.T) {
		data := `{
			"event_id": "evt_001",
			"type": "conversation.item.input_audio_transcription.completed",
			"item_id": "item_001",
			"content_index": 0,
			"transcript": "Hello, how are you?"
		}`

		event, err := ParseServerEvent([]byte(data))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		e, ok := event.(*ConversationItemInputAudioTranscriptionCompletedEvent)
		if !ok {
			t.Fatalf("expected ConversationItemInputAudioTranscriptionCompletedEvent, got %T", event)
		}
		if e.Transcript != "Hello, how are you?" {
			t.Errorf("expected transcript, got %s", e.Transcript)
		}
	})

	t.Run("transcription failed", func(t *testing.T) {
		data := `{
			"event_id": "evt_002",
			"type": "conversation.item.input_audio_transcription.failed",
			"item_id": "item_001",
			"content_index": 0,
			"error": {
				"type": "transcription_error",
				"code": "audio_too_short",
				"message": "Audio is too short to transcribe"
			}
		}`

		event, err := ParseServerEvent([]byte(data))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		e, ok := event.(*ConversationItemInputAudioTranscriptionFailedEvent)
		if !ok {
			t.Fatalf("expected ConversationItemInputAudioTranscriptionFailedEvent, got %T", event)
		}
		if e.Error.Code != "audio_too_short" {
			t.Errorf("expected error code, got %s", e.Error.Code)
		}
	})
}
