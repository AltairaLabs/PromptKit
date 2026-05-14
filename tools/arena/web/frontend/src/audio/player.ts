// Browser-side audio player. Connects to /api/events?audio=1 and renders
// stereo audio (user → left, agent → right) via Web AudioContext.

export interface AudioPlayerOptions {
  rate?: number;
  runId: string;
  onError?: (msg: string) => void;
}

interface AudioFrame {
  type: "audio";
  run_id: string;
  direction: "input" | "output";
  seq: number;
  rate: number;
  samples: string; // base64 s16le
}

export class AudioPlayer {
  private readonly ctx: AudioContext;
  private readonly leftPanner: StereoPannerNode;
  private readonly rightPanner: StereoPannerNode;
  private nextStartTime: Record<"input" | "output", number> = { input: 0, output: 0 };
  private source: EventSource | null = null;
  private readonly opts: AudioPlayerOptions;

  constructor(opts: AudioPlayerOptions) {
    this.opts = opts;
    const rate = opts.rate ?? 24000;
    this.ctx = new AudioContext({ sampleRate: rate });

    this.leftPanner = this.ctx.createStereoPanner();
    this.leftPanner.pan.value = -1;
    this.leftPanner.connect(this.ctx.destination);

    this.rightPanner = this.ctx.createStereoPanner();
    this.rightPanner.pan.value = 1;
    this.rightPanner.connect(this.ctx.destination);
  }

  /** Open the audio EventSource without resuming the AudioContext.
   * Use this to start receiving frames during an in-progress run before the
   * user has clicked anything; frames arrive but stay silent until play(). */
  connect(eventsUrl: string): void {
    if (this.source) return;
    // Reset scheduling clocks each session.
    this.nextStartTime = { input: 0, output: 0 };
    const url = `${eventsUrl}?audio=1`;
    this.source = new EventSource(url);
    this.source.addEventListener("audio", (ev) => this.onAudio(ev));
    this.source.addEventListener("error", () => {
      this.opts.onError?.("SSE connection error");
    });
  }

  /** Resume the AudioContext so scheduled frames become audible.
   * Browsers gate AudioContext.resume() until a user gesture; call this
   * from a click handler. */
  play(): void {
    void this.ctx.resume();
  }

  /** Connect and start playback in one call. Equivalent to connect()+play(). */
  start(eventsUrl: string): void {
    this.connect(eventsUrl);
    this.play();
  }

  /** Pause playback (keep EventSource open so we can resume mid-run). */
  pause(): void {
    void this.ctx.suspend();
  }

  /** Stop playback and release resources. */
  stop(): void {
    this.source?.close();
    this.source = null;
    void this.ctx.suspend();
  }

  /** Permanently close. Construct a new instance to play again. */
  close(): void {
    this.stop();
    void this.ctx.close();
  }

  private onAudio(ev: MessageEvent): void {
    try {
      const frame: AudioFrame = JSON.parse(ev.data);
      if (frame.run_id !== this.opts.runId) return;
      if (!frame.samples) return;

      const pcm = base64ToInt16(frame.samples);
      if (pcm.length === 0) return;

      const buffer = this.ctx.createBuffer(1, pcm.length, frame.rate);
      buffer.copyToChannel(int16ToFloat32(pcm), 0);

      const node = this.ctx.createBufferSource();
      node.buffer = buffer;
      const panner = frame.direction === "input" ? this.leftPanner : this.rightPanner;
      node.connect(panner);

      const now = this.ctx.currentTime;
      const startAt = Math.max(now, this.nextStartTime[frame.direction]);
      node.start(startAt);
      this.nextStartTime[frame.direction] = startAt + buffer.duration;
    } catch (err) {
      this.opts.onError?.(`audio decode error: ${err}`);
    }
  }
}

// Exported so unit tests can exercise the pure decode/scale helpers without
// instantiating an AudioContext.
export function base64ToInt16(b64: string): Int16Array {
  const bin = atob(b64);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.codePointAt(i) ?? 0;
  // s16le: byte 0 = LSB, byte 1 = MSB. Int16Array on a little-endian
  // ArrayBuffer interprets pairs that way directly.
  return new Int16Array(bytes.buffer, bytes.byteOffset, bytes.byteLength / 2);
}

export function int16ToFloat32(input: Int16Array): Float32Array {
  const out = new Float32Array(input.length);
  for (let i = 0; i < input.length; i++) {
    out[i] = input[i] / 32768;
  }
  return out;
}
