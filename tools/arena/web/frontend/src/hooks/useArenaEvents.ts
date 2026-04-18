import { useEffect, useReducer } from "react";
import type {
  ArenaState,
  SSEEvent,
  ActiveRun,
  ArenaRunStartedData,
  ArenaRunCompletedData,
  ArenaRunFailedData,
  ArenaTurnData,
  MessageCreatedData,
  MessageUpdatedData,
  ProviderCallData,
  LogEntry,
} from "@/types";
import { toDisplayString } from "@/lib/utils";

const initialState: ArenaState = {
  connected: false,
  runs: {},
  completedRunIds: [],
  totalCost: 0,
  totalTokens: 0,
  logs: [],
};

type Action =
  | { type: "CONNECTED" }
  | { type: "DISCONNECTED" }
  | { type: "RUN_STARTED"; runId: string; data: ArenaRunStartedData; timestamp: string }
  | { type: "RUN_COMPLETED"; runId: string; data: ArenaRunCompletedData; timestamp: string }
  | { type: "RUN_FAILED"; runId: string; data: ArenaRunFailedData; timestamp: string }
  | { type: "TURN_STARTED"; runId: string; data: ArenaTurnData; timestamp: string }
  | { type: "TURN_COMPLETED"; runId: string; data: ArenaTurnData; timestamp: string }
  | { type: "MESSAGE_CREATED"; runId: string; data: MessageCreatedData; timestamp: string }
  | { type: "MESSAGE_UPDATED"; runId: string; data: MessageUpdatedData; timestamp: string }
  | { type: "PROVIDER_CALL_COMPLETED"; runId: string; data: ProviderCallData; timestamp: string }
  | { type: "LOG"; entry: LogEntry };

function reducer(state: ArenaState, action: Action): ArenaState {
  switch (action.type) {
    case "CONNECTED":
      return { ...state, connected: true };

    case "DISCONNECTED":
      return { ...state, connected: false };

    case "RUN_STARTED": {
      const d = action.data || {} as ArenaRunStartedData;
      const run: ActiveRun = {
        runId: action.runId,
        scenario: d.scenario || "unknown",
        provider: d.provider || "unknown",
        region: d.region || "default",
        startTime: action.timestamp,
        turnIndex: 0,
        messages: [],
        costs: { inputTokens: 0, outputTokens: 0, totalCost: 0 },
        status: "running",
      };
      return { ...state, runs: { ...state.runs, [action.runId]: run } };
    }

    case "RUN_COMPLETED": {
      const existing = state.runs[action.runId];
      if (!existing) return state;
      const cd = action.data || {} as ArenaRunCompletedData;
      return {
        ...state,
        runs: {
          ...state.runs,
          [action.runId]: { ...existing, status: "completed", duration: cd.duration },
        },
        completedRunIds: [...state.completedRunIds, action.runId],
        totalCost: state.totalCost + (cd.cost || 0),
      };
    }

    case "RUN_FAILED": {
      const existing = state.runs[action.runId];
      if (!existing) return state;
      const fd = action.data || {} as ArenaRunFailedData;
      return {
        ...state,
        runs: {
          ...state.runs,
          [action.runId]: { ...existing, status: "failed", error: fd.error },
        },
        completedRunIds: [...state.completedRunIds, action.runId],
      };
    }

    case "TURN_STARTED": {
      const existing = state.runs[action.runId];
      if (!existing) return state;
      return {
        ...state,
        runs: {
          ...state.runs,
          [action.runId]: { ...existing, turnIndex: action.data.turn_index },
        },
      };
    }

    case "TURN_COMPLETED":
      return state;

    case "MESSAGE_CREATED": {
      const existing = state.runs[action.runId];
      if (!existing) return state;
      return {
        ...state,
        runs: {
          ...state.runs,
          [action.runId]: {
            ...existing,
            messages: [...existing.messages, action.data],
          },
        },
      };
    }

    case "MESSAGE_UPDATED": {
      const existing = state.runs[action.runId];
      if (!existing) return state;
      return {
        ...state,
        runs: {
          ...state.runs,
          [action.runId]: {
            ...existing,
            costs: {
              inputTokens: existing.costs.inputTokens + action.data.inputTokens,
              outputTokens: existing.costs.outputTokens + action.data.outputTokens,
              totalCost: existing.costs.totalCost + action.data.totalCost,
            },
          },
        },
        totalTokens: state.totalTokens + action.data.inputTokens + action.data.outputTokens,
      };
    }

    case "PROVIDER_CALL_COMPLETED":
      return state;

    case "LOG":
      return {
        ...state,
        logs: [...state.logs.slice(-499), action.entry],
      };

    default:
      return state;
  }
}

function mapSSEToAction(event: SSEEvent): Action | null {
  const runId = event.executionId || event.conversationId || "";
  const ts = event.timestamp;
  const d = event.data as Record<string, unknown> | null | undefined;

  // Skip events with no data payload — the reducer expects populated data
  if (!d) return null;

  switch (event.type) {
    case "arena.run.started":
      return { type: "RUN_STARTED", runId, data: d as unknown as ArenaRunStartedData, timestamp: ts };
    case "arena.run.completed":
      return { type: "RUN_COMPLETED", runId, data: d as unknown as ArenaRunCompletedData, timestamp: ts };
    case "arena.run.failed":
      return { type: "RUN_FAILED", runId, data: d as unknown as ArenaRunFailedData, timestamp: ts };
    case "arena.turn.started":
      return { type: "TURN_STARTED", runId, data: d as unknown as ArenaTurnData, timestamp: ts };
    case "arena.turn.completed":
    case "arena.turn.failed":
      return { type: "TURN_COMPLETED", runId, data: d as unknown as ArenaTurnData, timestamp: ts };
    case "message.created":
      return { type: "MESSAGE_CREATED", runId, data: d as unknown as MessageCreatedData, timestamp: ts };
    case "message.updated":
      return { type: "MESSAGE_UPDATED", runId, data: d as unknown as MessageUpdatedData, timestamp: ts };
    case "provider.call.completed":
      return { type: "PROVIDER_CALL_COMPLETED", runId, data: d as unknown as ProviderCallData, timestamp: ts };
    case "provider.call.failed":
      return {
        type: "LOG",
        entry: {
          timestamp: ts,
          level: "error",
          message: `Provider call failed: ${toDisplayString(d.provider, "unknown")} — ${toDisplayString(d.error, "unknown error")}`,
          runId,
        },
      };
    default:
      return null;
  }
}

export function useArenaEvents() {
  const [state, dispatch] = useReducer(reducer, initialState);

  useEffect(() => {
    const eventSource = new EventSource("/api/events");

    eventSource.onopen = () => {
      console.debug("[SSE] Connected");
      dispatch({ type: "CONNECTED" });
    };
    eventSource.onerror = (err) => {
      console.warn("[SSE] Error/disconnected", err);
      dispatch({ type: "DISCONNECTED" });
    };

    eventSource.onmessage = (e) => {
      try {
        const event: SSEEvent = JSON.parse(e.data);
        console.debug("[SSE]", event.type, event.executionId, event.data);
        const action = mapSSEToAction(event);
        if (action) dispatch(action);
      } catch (err) {
        console.warn("[SSE] Failed to parse event:", err, e.data);
      }
    };

    return () => eventSource.close();
  }, []);

  return state;
}
