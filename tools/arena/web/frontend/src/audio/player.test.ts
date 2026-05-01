import { describe, it, expect } from "vitest";
import { base64ToInt16, int16ToFloat32 } from "./player";

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
