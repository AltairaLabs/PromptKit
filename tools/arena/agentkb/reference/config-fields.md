# Config Fields

Compact cheat-sheet generated from the embedded schemas. Run `promptarena schema <type>` for the full schema; `promptarena validate` enforces the binary-embedded copy.

## arena

Fields under `spec`:

| field | type | required | description |
|-------|------|----------|-------------|
| `a2a_agents` | array |  | — |
| `agents` | — |  | — |
| `compositions` | — |  | — |
| `defaults` | object | ✓ | — |
| `deploy` | object |  | — |
| `embedding_providers` | array |  | — |
| `eval_specs` | object |  | — |
| `evals` | array |  | — |
| `globals` | object |  | — |
| `image_providers` | array |  | — |
| `judge_defaults` | object |  | — |
| `judge_specs` | object |  | — |
| `judges` | array |  | — |
| `mcp_servers` | array |  | — |
| `memory` | — |  | — |
| `pack_evals` | array |  | — |
| `pack_file` | string |  | — |
| `prompt_configs` | array |  | — |
| `prompt_specs` | object |  | — |
| `provider_specs` | object |  | — |
| `providers` | array | ✓ | — |
| `runtime` | object |  | — |
| `scenario_specs` | object |  | — |
| `scenarios` | array |  | — |
| `self_play` | object |  | — |
| `skills` | array |  | — |
| `state_store` | object |  | — |
| `stt_providers` | array |  | — |
| `tool_specs` | object |  | — |
| `tools` | array |  | — |
| `tts_providers` | array |  | — |
| `voices` | array |  | — |
| `workflow` | — |  | — |

## eval

Fields under `spec`:

| field | type | required | description |
|-------|------|----------|-------------|
| `conversation_assertions` | array |  | Conversation-level assertions |
| `description` | string | ✓ | Human-readable description |
| `id` | string |  | Unique identifier for this evaluation |
| `mode` | string |  | Replay timing mode (instant |
| `recording` | object | ✓ | Recording source |
| `speed` | number |  | Playback speed (default 1.0) |
| `tags` | array |  | Tags for categorization |
| `turns` | array |  | Turn-level assertions |

## logging

Fields under `spec`:

| field | type | required | description |
|-------|------|----------|-------------|
| `commonFields` | object |  | Key-value pairs added to every log entry |
| `defaultLevel` | string |  | Default log level for all modules |
| `format` | string |  | Log output format |
| `modules` | array |  | Per-module logging configuration |

## persona

Fields under `spec`:

| field | type | required | description |
|-------|------|----------|-------------|
| `constraints` | array | ✓ | — |
| `defaults` | object |  | — |
| `description` | string | ✓ | — |
| `fragments` | array |  | — |
| `goals` | array | ✓ | — |
| `id` | string |  | — |
| `optional_vars` | object |  | — |
| `prompt_activity` | string |  | — |
| `required_vars` | array |  | — |
| `style` | object |  | — |
| `system_prompt` | string |  | — |
| `system_template` | string |  | — |
| `voice` | string |  | — |

## promptconfig

Fields under `spec`:

| field | type | required | description |
|-------|------|----------|-------------|
| `allowed_tools` | array |  | — |
| `compilation` | object |  | — |
| `description` | string | ✓ | — |
| `evals` | array |  | — |
| `fragments` | array |  | — |
| `media` | object |  | — |
| `metadata` | object |  | — |
| `model_overrides` | object |  | — |
| `parameters` | object |  | — |
| `system_template` | string | ✓ | — |
| `task_type` | string | ✓ | — |
| `template_engine` | object |  | — |
| `tested_models` | array |  | — |
| `tool_policy` | object |  | — |
| `validators` | array |  | — |
| `variables` | array |  | — |
| `version` | string | ✓ | — |

## provider

Fields under `spec`:

| field | type | required | description |
|-------|------|----------|-------------|
| `additional_config` | object |  | — |
| `audio_files` | array |  | — |
| `base_url` | string |  | — |
| `capabilities` | array |  | — |
| `credential` | object |  | — |
| `defaults` | object |  | — |
| `headers` | object |  | — |
| `http_transport` | object |  | — |
| `id` | string |  | — |
| `include_raw_output` | boolean |  | — |
| `model` | string |  | — |
| `platform` | object |  | — |
| `pricing` | object |  | — |
| `pricing_correct_at` | string |  | — |
| `rate_limit` | object |  | — |
| `request_timeout` | string |  | — |
| `role` | string |  | — |
| `sample_rate` | integer |  | — |
| `stream_idle_timeout` | string |  | — |
| `stream_max_concurrent` | integer |  | — |
| `stream_retry` | object |  | — |
| `type` | string | ✓ | — |
| `unsupported_params` | array |  | — |
| `voice` | string |  | — |

## runtime-config

Fields under `spec`:

| field | type | required | description |
|-------|------|----------|-------------|
| `embedding_providers` | array |  | Embedding provider configurations |
| `evals` | object |  | External eval process bindings keyed by eval type name |
| `hooks` | object |  | External hook process configurations |
| `inference_providers` | array |  | Inference (classify) provider configurations |
| `logging` | object |  | Logging configuration |
| `mcp_servers` | array |  | MCP server configurations |
| `providers` | array |  | LLM provider configurations |
| `sandboxes` | object |  | Named sandbox backends for exec-hook subprocess launch |
| `selectors` | object |  | External selector processes narrowing skill and tool candidate sets |
| `skills` | object |  | Runtime skill configuration |
| `state_store` | object |  | Conversation state persistence configuration |
| `stt_providers` | array |  | Speech-to-text provider configurations |
| `tool_selector` | string |  | Name of a selector declared under spec.selectors used to narrow the LLM-visible tool set per turn |
| `tools` | object |  | Tool implementation bindings keyed by tool name |
| `tts_providers` | array |  | Text-to-speech provider configurations |

## scenario

Fields under `spec`:

| field | type | required | description |
|-------|------|----------|-------------|
| `constraints` | object |  | — |
| `context` | object |  | — |
| `context_metadata` | object |  | — |
| `context_policy` | object |  | — |
| `conversation_assertions` | array |  | — |
| `description` | string | ✓ | — |
| `duplex` | object |  | — |
| `id` | string |  | — |
| `labels` | object |  | — |
| `mode` | string |  | — |
| `provider_group` | string |  | — |
| `providers` | array |  | — |
| `required_capabilities` | array |  | — |
| `seed_memories` | array |  | — |
| `streaming` | boolean |  | — |
| `task_type` | string |  | — |
| `tool_policy` | object |  | — |
| `trials` | integer |  | Number of times to run this scenario for statistical evaluation |
| `turns` | array |  | — |
| `variables` | object |  | Template variables to inject into the pack |
| `voice` | string |  | — |

## tool

Fields under `spec`:

| field | type | required | description |
|-------|------|----------|-------------|
| `client` | object |  | — |
| `description` | string | ✓ | — |
| `exec` | object |  | — |
| `http` | object |  | — |
| `input_schema` | — | ✓ | — |
| `mock_parts` | array |  | — |
| `mock_result` | — |  | Static mock response returned regardless of tool-call args. Use when the response does not depend on inputs. Mutually exclusive with mock_template. |
| `mock_template` | string |  | Go text/template rendered against tool-call args (parsed as a JSON map). Rendered output is parsed back as JSON. Use this instead of mock_result when the response should depend on inputs (e.g. branching on order_id with {{ if eq .order_id "X" }}...{{ end }}). Mutually exclusive with mock_result. |
| `mode` | string | ✓ | Execution mode. One of: 'mock' (use mock_result or mock_template) - 'live' (HTTP via 'http') - 'mcp' (MCP server) - 'exec' (subprocess) - 'client' (client-side handler). |
| `name` | string |  | — |
| `output_schema` | — | ✓ | — |
| `timeout_ms` | integer |  | — |
