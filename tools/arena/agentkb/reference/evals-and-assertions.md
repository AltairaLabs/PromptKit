# Evals & Assertions

Generated from the handler registry — do not edit by hand; run the reference generator.

An **eval** emits a raw `Score` (0..1) or a boolean gate. A **`type: assertion`** wrapper applies the pass/fail **threshold** (`min_score`/`max_score`). Never put a threshold on the inner eval.

| id | level | score | description |
|----|-------|-------|-------------|
| `a2a_eval` | turn | 0..1 raw signal | Delegates turn scoring to an Agent-to-Agent evaluator. |
| `a2a_eval_session` | session | 0..1 raw signal | Delegates session scoring to an Agent-to-Agent evaluator. |
| `agent_invoked` | session | boolean gate | A sub-agent/skill was invoked. |
| `agent_not_invoked` | session | boolean gate | A sub-agent/skill was not invoked. |
| `agent_response_contains` | session | boolean gate | An agent response contains the given text. |
| `answer_relevancy` | turn | 0..1 raw signal | Degree to which the answer is relevant to the question. |
| `assertion` | turn | — | Structural wrapper — applies a pass/fail threshold to an inner eval's score. Thresholds live HERE, never on the inner eval. |
| `audio_duration` | turn | boolean gate | Audio duration falls within the expected range. |
| `audio_emotion` | turn | 0..1 raw signal | Classifier signal for the emotion conveyed in audio output. |
| `audio_format` | turn | boolean gate | Audio output is in the expected format. |
| `bias` | turn | 0..1 raw signal | Bias signal for the output. LLM judge required. |
| `composition_branch_taken` | session | boolean gate | The composition took the expected branch. |
| `composition_output` | session | boolean gate | The composition's final output matches expectations. |
| `composition_parallel_complete` | session | boolean gate | Parallel composition steps all completed. |
| `composition_step_output` | session | boolean gate | A composition step produced the expected output. |
| `contains` | turn | boolean gate | Output text contains the given substring. |
| `contains_any` | session | boolean gate | Output contains at least one of the given substrings, checked across the session. |
| `content_excludes` | session | boolean gate | Output excludes all banned substrings across the session. |
| `contextual_precision` | turn | 0..1 raw signal | Precision of the retrieved context relative to the question. |
| `contextual_recall` | turn | 0..1 raw signal | Recall of relevant facts in the retrieved context. |
| `contextual_relevancy` | turn | 0..1 raw signal | Overall relevance of the retrieved context. |
| `cosine_similarity` | turn | 0..1 raw signal | Embedding cosine similarity between output and a reference. |
| `cost_budget` | session | boolean gate | Total cost stays within the given budget. |
| `directional` | session | 0..1 raw signal | Whether output improved or degraded versus a baseline. |
| `faithfulness` | turn | 0..1 raw signal | Degree to which the answer is grounded in the provided context. |
| `field_presence` | turn | boolean gate | Required fields are present in the structured output. |
| `guardrail` | turn | — | Structural wrapper — enforces an eval primitive in production AND tests; assertions may observe its firings. |
| `guardrail_triggered` | session | boolean gate | A guardrail fired during the run. |
| `hallucination` | turn | 0..1 raw signal | Degree of hallucinated (unsupported) content in the output. |
| `image_dimensions` | turn | boolean gate | Image dimensions match the expected size. |
| `image_format` | turn | boolean gate | Image output is in the expected format. |
| `image_moderation` | turn | 0..1 raw signal | Moderation signal for image output (e.g. NSFW likelihood). |
| `invariant_fields_preserved` | turn | boolean gate | Named fields are unchanged across turns (invariants preserved). |
| `json_path` | turn | boolean gate | A JSONPath expression over the output evaluates as expected. |
| `json_schema` | turn | boolean gate | Output validates against the given JSON Schema. |
| `json_valid` | turn | boolean gate | Output is syntactically valid JSON. |
| `latency_budget` | turn | boolean gate | Response latency stays within the given budget. |
| `llm_judge` | turn | 0..1 raw signal | An LLM judges the turn output against a rubric. Requires a judge provider. |
| `llm_judge_session` | session | 0..1 raw signal | An LLM judges the whole session against a rubric. Requires a judge provider. |
| `llm_judge_tool_calls` | session | 0..1 raw signal | An LLM judges the session's tool calls against a rubric. Requires a judge provider. |
| `max_length` | turn | boolean gate | Output is at most the given length (characters/tokens). |
| `min_length` | turn | boolean gate | Output is at least the given length (characters/tokens). |
| `no_tool_errors` | turn | boolean gate | Tools executed without returning errors. |
| `outcome_equivalent` | session | 0..1 raw signal | Whether two outputs are semantically equivalent. |
| `pii_leakage` | turn | 0..1 raw signal | PII-leakage signal; regex fallback when no judge is configured. |
| `regex` | turn | boolean gate | Output matches the given regular expression. |
| `rest_eval` | turn | 0..1 raw signal | Calls an external REST endpoint to score the turn. |
| `rest_eval_session` | session | 0..1 raw signal | Calls an external REST endpoint to score the session. |
| `role_violation` | turn | 0..1 raw signal | Degree to which the output violates the expected role/persona. LLM judge required. |
| `sentence_count` | turn | boolean gate | Output sentence count falls within the given range. |
| `skill_activated` | session | boolean gate | The named skill was activated. |
| `skill_activation_order` | session | boolean gate | Skills were activated in the specified order. |
| `skill_not_activated` | session | boolean gate | The named skill was not activated. |
| `state_is` | session | boolean gate | The workflow is in the expected state. |
| `text_sentiment` | turn | 0..1 raw signal | Classifier sentiment signal for the output. |
| `text_toxicity` | turn | 0..1 raw signal | Classifier toxicity signal for the output (0 safe … 1 toxic). |
| `tool_anti_pattern` | session | boolean gate | Detects unwanted tool-use patterns across the session. |
| `tool_args` | turn | boolean gate | A tool was called with arguments matching the given spec. |
| `tool_args_excluded_session` | session | boolean gate | No tool was called with the excluded arguments across the session. |
| `tool_args_session` | session | boolean gate | A tool was called with matching arguments somewhere in the session. |
| `tool_call_chain` | session | boolean gate | Tool results chain into subsequent calls as specified. |
| `tool_call_count` | turn | boolean gate | The number of tool invocations matches an exact value or range. |
| `tool_call_sequence` | session | boolean gate | Tools were called in the specified order. |
| `tool_calls_with_args` | turn | boolean gate | Tools were called with the given argument sets. |
| `tool_efficiency` | session | 0..1 raw signal | Tool-use efficiency metric for the session. |
| `tool_exec` | turn | boolean gate | Executes a tool and gates on its success/return; used for codegen-style scoring. |
| `tool_no_repeat` | session | boolean gate | A tool was not called repeatedly with the same arguments. |
| `tool_result_has_media` | turn | boolean gate | A tool returned media (image/audio/video) content. |
| `tool_result_includes` | turn | boolean gate | A tool result contains the given text. |
| `tool_result_matches` | turn | boolean gate | A tool result matches the given regular expression. |
| `tool_result_media_type` | turn | boolean gate | A tool result's media type matches the expected type. |
| `tools_called` | turn | boolean gate | The named tool(s) were called this turn. |
| `tools_called_session` | session | boolean gate | The named tool(s) were called somewhere in the session. |
| `tools_not_called` | turn | boolean gate | The named tool(s) were not called this turn. |
| `tools_not_called_session` | session | boolean gate | The named tool(s) were never called in the session. |
| `toxicity` | turn | 0..1 raw signal | Safety toxicity signal for the output. LLM judge required. |
| `transitioned_to` | session | boolean gate | The workflow transitioned to the expected state. |
| `video_duration` | turn | boolean gate | Video duration falls within the expected range. |
| `video_resolution` | turn | boolean gate | Video resolution matches the expected size. |
| `workflow_complete` | session | boolean gate | The workflow reached a terminal/complete state. |
| `workflow_tool_access` | session | boolean gate | A tool was accessible in the expected workflow state. |
| `workflow_transition_order` | session | boolean gate | Workflow transitions occurred in the specified order. |

## Aliases

Legacy names that map to a canonical handler:

| alias | canonical |
|-------|-----------|
| `banned_words` | `content_excludes` |
| `content_includes` | `contains` |
| `content_includes_any` | `contains_any` |
| `content_matches` | `regex` |
| `content_not_includes` | `content_excludes` |
| `is_valid_json` | `json_valid` |
| `length` | `max_length` |
| `llm_judge_conversation` | `llm_judge_session` |
| `max_sentences` | `sentence_count` |
| `required_fields` | `field_presence` |
| `tool_called` | `tools_called` |
| `tools_not_called_with_args` | `tool_args_excluded_session` |
| `valid_json` | `json_valid` |
