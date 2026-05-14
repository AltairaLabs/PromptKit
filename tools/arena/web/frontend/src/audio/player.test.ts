import { describe, it, expect } from "vitest";
import {
  base64ToInt16,
  int16ToFloat32,
  newPlaybackTimeline,
  scheduleFrame,
} from "./player";

describe("base64ToInt16", () => {
  it("decodes empty string to empty Int16Array", () => {
    expect(base64ToInt16("").length).toBe(0);
  });

  it("decodes s16le little-endian pairs (LSB-first)", () => {
    // Byte sequence 0x01 0x00 0x02 0x00 0xff 0xff in little-endian s16:
    //   sample 0 = 0x0001 = 1
    //   sample 1 = 0x0002 = 2
    //   sample 2 = 0xffff = -1 (signed)
    const bytes = new Uint8Array([0x01, 0x00, 0x02, 0x00, 0xff, 0xff]);
    const b64 = btoa(String.fromCharCode(...bytes));
    const samples = base64ToInt16(b64);
    expect(Array.from(samples)).toEqual([1, 2, -1]);
  });

  it("round-trips a known PCM sample", () => {
    const bytes = new Uint8Array([0x10, 0x27, 0xf0, 0xd8]); // 10000, -10000
    const b64 = btoa(String.fromCharCode(...bytes));
    expect(Array.from(base64ToInt16(b64))).toEqual([10000, -10000]);
  });
});

describe("int16ToFloat32", () => {
  it("scales by 1/32768", () => {
    const input = new Int16Array([0, 16384, -16384, 32767, -32768]);
    const out = int16ToFloat32(input);
    expect(out[0]).toBeCloseTo(0, 6);
    expect(out[1]).toBeCloseTo(0.5, 6);
    expect(out[2]).toBeCloseTo(-0.5, 6);
    expect(out[3]).toBeCloseTo(0.99997, 4);
    expect(out[4]).toBeCloseTo(-1.0, 6);
  });

  it("preserves length", () => {
    expect(int16ToFloat32(new Int16Array([1, 2, 3])).length).toBe(3);
    expect(int16ToFloat32(new Int16Array(0)).length).toBe(0);
  });

  it("end-to-end: base64 → int16 → float32 round-trips amplitude", () => {
    const bytes = new Uint8Array([0x00, 0x40]); // 0x4000 = 16384 → 0.5
    const b64 = btoa(String.fromCharCode(...bytes));
    const float = int16ToFloat32(base64ToInt16(b64));
    expect(float[0]).toBeCloseTo(0.5, 6);
  });
});

describe("scheduleFrame", () => {
  it("schedules the first frame at now when timeline is empty", () => {
    const r = scheduleFrame(newPlaybackTimeline(), "output", 0.5, 1.0);
    expect(r.startAt).toBe(1.0);
    expect(r.timeline.nextStart).toBe(1.5);
  });

  it("serializes back-to-back frames in the same direction", () => {
    let t = newPlaybackTimeline();
    let r = scheduleFrame(t, "output", 1.0, 0);
    t = r.timeline;
    r = scheduleFrame(t, "output", 0.5, 0.1);
    expect(r.startAt).toBe(1.0); // not 0.1 — must follow first frame's tail
    expect(r.timeline.nextStart).toBe(1.5);
  });

  // This is the demo overlap bug. With the old per-direction logic, a fresh
  // input frame would start immediately even though output is still playing.
  // The fix is to share one nextStart across directions so scripted turns
  // serialize cleanly.
  it("serializes frames regardless of direction (no left-right overlap)", () => {
    let t = newPlaybackTimeline();
    // 2s of agent output
    let r = scheduleFrame(t, "output", 2.0, 0);
    t = r.timeline;
    // user input frame arrives at now=0.5 — must wait for output to finish
    r = scheduleFrame(t, "input", 1.0, 0.5);
    expect(r.startAt).toBe(2.0);
    expect(r.timeline.nextStart).toBe(3.0);
  });

  it("falls back to now when the timeline has fallen into the past", () => {
    const r = scheduleFrame({ nextStart: 1.0 }, "input", 0.5, 5.0);
    expect(r.startAt).toBe(5.0);
    expect(r.timeline.nextStart).toBe(5.5);
  });
});
