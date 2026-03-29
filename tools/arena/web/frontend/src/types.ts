// src/types.ts

// === SSE Events ===

export interface SSEEvent {
  type: string;
  timestamp: string;
  executionId?: string;
  conversationId?: string;
  data?: unknown;
}

export interface ArenaRunStartedData {
  scenario: string;
  provider: string;
  region: string;
}

export interface ArenaRunCompletedData {
  duration: number;
  cost: number;
}

export interface ArenaRunFailedData {
  error: string;
}

export interface ArenaTurnData {
  turn_index: number;
  role: string;
  scenario: string;
  error?: string;
}

export interface ProviderCallData {
  provider: string;
  model: string;
  duration?: number;
  cost?: number;
  error?: string;
}

export interface MessageCreatedData {
  role: string;
  content: string;
  index: number;
  toolCalls?: MessageToolCall[];
  toolResult?: MessageToolResult | null;
}

export interface MessageUpdatedData {
  index: number;
  latencyMs: number;
  inputTokens: number;
  outputTokens: number;
  totalCost: number;
}

export interface ToolCallEventData {
  toolName: string;
  callId: string;
  status: string;
}

export interface ValidationEventData {
  validatorName: string;
  validatorType: string;
  error: string;
  monitorOnly: boolean;
  score?: number;
}

// === REST API ===

export interface RunRequest {
  providers?: string[];
  scenarios?: string[];
  regions?: string[];
}

export interface RunStartedResponse {
  combinations: number;
  status: "started";
}

// === Run Results ===

export interface RunResult {
  RunID: string;
  PromptPack: string;
  Region: string;
  ScenarioID: string;
  ProviderID: string;
  Params: Record<string, unknown>;
  Messages: Message[];
  Commit: Record<string, unknown>;
  Cost: CostInfo;
  ToolStats?: ToolStats;
  Violations: ValidationError[];
  StartTime: string;
  EndTime: string;
  Duration: number;
  Error: string;
  SelfPlay: boolean;
  PersonaID: string;
  MediaOutputs: MediaOutput[];
  ConversationAssertions?: AssertionsSummary;
  A2AAgents: A2AAgentInfo[];
  TrialResults?: TrialResults;
}

export interface Message {
  role: string;
  content: string;
  parts?: ContentPart[];
  tool_calls?: MessageToolCall[];
  tool_result?: MessageToolResult;
  timestamp?: string;
  latency_ms?: number;
  cost_info?: CostInfo;
  meta?: Record<string, unknown>;
  validations?: ValidationResult[];
}

export interface ContentPart {
  type: "text" | "image" | "audio" | "video" | "document" | "thinking";
  text?: string;
  media?: MediaContent;
}

export interface MediaContent {
  mime_type: string;
  file_path?: string;
  url?: string;
  format?: string;
  size_kb?: number;
  duration?: number;
  width?: number;
  height?: number;
}

export interface MessageToolCall {
  id: string;
  name: string;
  args: Record<string, unknown>;
}

export interface MessageToolResult {
  id: string;
  name: string;
  error?: string;
  error_type?: string;
  parts: ContentPart[];
  latency_ms: number;
}

export interface CostInfo {
  input_tokens: number;
  output_tokens: number;
  cached_tokens?: number;
  input_cost_usd: number;
  output_cost_usd: number;
  cached_cost_usd?: number;
  total_cost_usd: number;
}

export interface ToolStats {
  total_calls: number;
  by_tool: Record<string, number>;
}

export interface ValidationError {
  type: string;
  tool: string;
  detail: string;
}

export interface ValidationResult {
  validator_type: string;
  passed: boolean;
  details?: Record<string, unknown>;
  timestamp?: string;
}

export interface AssertionsSummary {
  failed: number;
  passed: boolean;
  results: ConversationValidationResult[];
  total: number;
}

export interface ConversationValidationResult {
  name: string;
  type: string;
  passed: boolean;
  score?: number;
  message?: string;
  details?: Record<string, unknown>;
}

export interface TrialResults {
  trial_count: number;
  pass_rate: number;
  flakiness_score: number;
  per_assertion_stats?: Record<string, AssertionTrialStats>;
}

export interface AssertionTrialStats {
  pass_rate: number;
  pass_count: number;
  fail_count: number;
  flakiness_score: number;
}

export interface MediaOutput {
  Type: string;
  MIMEType: string;
  SizeBytes: number;
  Duration?: number;
  Width?: number;
  Height?: number;
  FilePath: string;
  Thumbnail: string;
  MessageIdx: number;
  PartIdx: number;
}

export interface A2AAgentInfo {
  name: string;
  description: string;
  skills?: A2ASkillInfo[];
}

export interface A2ASkillInfo {
  id: string;
  name: string;
  description?: string;
  tags?: string[];
}

export interface SelfPlayRoleInfo {
  provider?: string;
  model?: string;
  region?: string;
}

// === App State ===

export interface ActiveRun {
  runId: string;
  scenario: string;
  provider: string;
  region: string;
  startTime: string;
  turnIndex: number;
  messages: MessageCreatedData[];
  costs: { inputTokens: number; outputTokens: number; totalCost: number };
  status: "running" | "completed" | "failed";
  duration?: number;
  error?: string;
}

export interface ArenaState {
  connected: boolean;
  runs: Record<string, ActiveRun>;
  completedRunIds: string[];
  totalCost: number;
  totalTokens: number;
  logs: LogEntry[];
}

export interface LogEntry {
  timestamp: string;
  level: "info" | "error";
  message: string;
  runId?: string;
}
