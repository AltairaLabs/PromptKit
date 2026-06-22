import { describe, it, expect } from "vitest";
import { __reducer, __mapSSEToAction, __initialState } from "./useArenaEvents";
import type { SSEEvent } from "@/types";

const fakeRunStarted = (runId: string): SSEEvent => ({
  type: "arena.run.started",
  timestamp: "2026-05-06T12:00:00Z",
  executionId: runId,
  conversationId: runId,
  data: { provider: "mock-duplex", region: "default", scenario: "aggressive-refund" },
});

const fakeMessageCreated = (
  runId: string,
  index: number,
  role: string,
  content: string,
): SSEEvent => ({
  type: "message.created",
  timestamp: "2026-05-06T12:00:00Z",
  executionId: runId,
  conversationId: runId,
  data: { role, content, index, toolCalls: null, toolResult: null },
});

describe("mapSSEToAction", () => {
  it("returns null when SSE event has no data field — protects the reducer from publisher gaps", () => {
    const event: SSEEvent = {
      type: "message.created",
      timestamp: "2026-05-06T12:00:00Z",
      executionId: "run-1",
      conversationId: "run-1",
      // data deliberately omitted — mirrors the bug where the SSE adapter
      // dropped Data on pointer-typed runtime events.
    };
    expect(__mapSSEToAction(event)).toBeNull();
  });

  it("maps arena.run.started → RUN_STARTED with runId from executionId", () => {
    const action = __mapSSEToAction(fakeRunStarted("run-1"));
    expect(action).toMatchObject({
      type: "RUN_STARTED",
      runId: "run-1",
      data: { provider: "mock-duplex", scenario: "aggressive-refund" },
    });
  });

  it("maps message.created → MESSAGE_CREATED with the data payload", () => {
    const action = __mapSSEToAction(fakeMessageCreated("run-1", 0, "user", "Hi"));
    expect(action).toMatchObject({
      type: "MESSAGE_CREATED",
      runId: "run-1",
      data: { role: "user", content: "Hi", index: 0 },
    });
  });

  it("aliases arena.duplex.turn.started to arena.turn.started so duplex runs update the UI", () => {
    const event: SSEEvent = {
      type: "arena.duplex.turn.started",
      timestamp: "2026-05-06T12:00:00Z",
      executionId: "run-1",
      conversationId: "run-1",
      data: { turn_index: 2, role: "user", scenario: "aggressive-refund" },
    };
    const action = __mapSSEToAction(event);
    expect(action).toMatchObject({ type: "TURN_STARTED" });
  });
});

describe("reducer", () => {
  it("RUN_STARTED creates an entry under state.runs[runId] with empty messages", () => {
    const next = __reducer(__initialState, {
      type: "RUN_STARTED",
      runId: "run-1",
      data: { provider: "mock-duplex", region: "default", scenario: "aggressive-refund" },
      timestamp: "2026-05-06T12:00:00Z",
    });
    expect(next.runs["run-1"]).toBeDefined();
    expect(next.runs["run-1"].status).toBe("running");
    expect(next.runs["run-1"].messages).toEqual([]);
  });

  it("MESSAGE_CREATED appends to state.runs[runId].messages — the bug regression test", () => {
    // This is the contract that was silently broken: the SSE adapter dropped
    // pointer-typed runtime events to nil data, mapSSEToAction returned null,
    // and this dispatch never fired. Without the dispatch, no live message
    // streaming. Lock it down here.
    let state = __reducer(__initialState, {
      type: "RUN_STARTED",
      runId: "run-1",
      data: { provider: "mock-duplex", region: "default", scenario: "agg" },
      timestamp: "t0",
    });
    state = __reducer(state, {
      type: "MESSAGE_CREATED",
      runId: "run-1",
      data: { role: "user", content: "hi", index: 0 },
      timestamp: "t1",
    });
    state = __reducer(state, {
      type: "MESSAGE_CREATED",
      runId: "run-1",
      data: { role: "assistant", content: "hello", index: 1 },
      timestamp: "t2",
    });
    expect(state.runs["run-1"].messages).toHaveLength(2);
    expect(state.runs["run-1"].messages[0]).toMatchObject({ role: "user", content: "hi" });
    expect(state.runs["run-1"].messages[1]).toMatchObject({ role: "assistant", content: "hello" });
  });

  it("MESSAGE_CREATED for an unknown runId is silently dropped (no implicit run creation)", () => {
    const next = __reducer(__initialState, {
      type: "MESSAGE_CREATED",
      runId: "unknown-run",
      data: { role: "user", content: "stray", index: 0 },
      timestamp: "t",
    });
    expect(next.runs["unknown-run"]).toBeUndefined();
  });

  it("RUN_COMPLETED flips status and adds to completedRunIds", () => {
    let state = __reducer(__initialState, {
      type: "RUN_STARTED",
      runId: "run-1",
      data: { provider: "mock-duplex", region: "default", scenario: "agg" },
      timestamp: "t0",
    });
    state = __reducer(state, {
      type: "RUN_COMPLETED",
      runId: "run-1",
      data: { duration: 1.5, cost: 0.005 },
      timestamp: "t1",
    });
    expect(state.runs["run-1"].status).toBe("completed");
    expect(state.completedRunIds).toContain("run-1");
    expect(state.totalCost).toBeCloseTo(0.005);
  });

  it("end-to-end: SSE event → mapSSEToAction → reducer integrates messages by runId", () => {
    // Mirrors the actual wire flow: a JSON SSE arrives, gets mapped, dispatched.
    let state = __initialState;
    const events: SSEEvent[] = [
      fakeRunStarted("run-1"),
      fakeMessageCreated("run-1", 0, "user", "first"),
      fakeMessageCreated("run-1", 1, "assistant", "second"),
    ];
    for (const ev of events) {
      const action = __mapSSEToAction(ev);
      if (action) state = __reducer(state, action);
    }
    expect(state.runs["run-1"].messages).toHaveLength(2);
    expect(state.runs["run-1"].messages.map((m) => m.role)).toEqual(["user", "assistant"]);
  });

  it("MESSAGE_CREATED upserts by index — duplicate index replaces, not appends", () => {
    let state = __reducer(__initialState, {
      type: "RUN_STARTED",
      runId: "run-upsert",
      data: { provider: "mock", region: "default", scenario: "test" },
      timestamp: "t0",
    });
    // First message at index 0
    state = __reducer(state, {
      type: "MESSAGE_CREATED",
      runId: "run-upsert",
      data: { role: "user", content: "hello", index: 0 },
      timestamp: "t1",
    });
    // Second message at same index 0 — should replace, not append
    state = __reducer(state, {
      type: "MESSAGE_CREATED",
      runId: "run-upsert",
      data: { role: "user", content: "hello updated", index: 0 },
      timestamp: "t2",
    });
    expect(state.runs["run-upsert"].messages).toHaveLength(1);
    expect(state.runs["run-upsert"].messages[0].content).toBe("hello updated");
  });

  it("MESSAGE_CREATED with different indices inserts in index order", () => {
    let state = __reducer(__initialState, {
      type: "RUN_STARTED",
      runId: "run-order",
      data: { provider: "mock", region: "default", scenario: "test" },
      timestamp: "t0",
    });
    // Insert index 1 first, then index 0 (out of order arrival)
    state = __reducer(state, {
      type: "MESSAGE_CREATED",
      runId: "run-order",
      data: { role: "assistant", content: "reply", index: 1 },
      timestamp: "t1",
    });
    state = __reducer(state, {
      type: "MESSAGE_CREATED",
      runId: "run-order",
      data: { role: "user", content: "question", index: 0 },
      timestamp: "t2",
    });
    expect(state.runs["run-order"].messages).toHaveLength(2);
    expect(state.runs["run-order"].messages[0].index).toBe(0);
    expect(state.runs["run-order"].messages[1].index).toBe(1);
  });
});
