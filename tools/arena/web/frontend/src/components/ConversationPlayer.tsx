import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Pause, Play, Square } from "lucide-react";
import { cn } from "@/lib/utils";
import type { Message } from "@/types";

// FIXED_TURN_SECONDS is how long a non-audio turn (text-only assistant
// response, tool call, tool result) sits highlighted in the player before
// advancing to the next step. Long enough to read, short enough to keep
// momentum.
const FIXED_TURN_SECONDS = 2.5;

interface ConversationPlayerProps {
  messages: Message[];
  // activeIdx is the message index currently being played. Parent uses it to
  // highlight the corresponding message bubble and scroll it into view.
  activeIdx: number | null;
  onActiveChange: (idx: number | null) => void;
}

interface PlaybackStep {
  messageIdx: number;
  role: string;
  audioURL?: string;
  // Fallback duration in seconds when there's no audio. Used to time the
  // active highlight on text-only turns.
  fallbackSeconds: number;
  // Estimated duration (audio length when known, fallback otherwise). Used
  // to scale timeline segment widths.
  estimatedSeconds: number;
}

function mediaURL(ref?: string): string | null {
  if (!ref) return null;
  return `/api/media/${ref.replace(/^\/?out\//, "")}`;
}

function pickPlayableAudioURL(msg: Message): string | null {
  for (const part of msg.parts ?? []) {
    if (part.type !== "audio") continue;
    const ref = part.media?.storage_reference ?? part.media?.file_path ?? part.media?.url;
    const lower = (ref ?? "").toLowerCase();
    // Only accept WAV/MP3/OGG/etc. — raw .pcm can't be played by <audio>.
    if (lower.endsWith(".wav") || lower.endsWith(".mp3") || lower.endsWith(".ogg") ||
        lower.endsWith(".m4a") || lower.endsWith(".webm") || lower.endsWith(".aac")) {
      return mediaURL(ref);
    }
  }
  return null;
}

// estimateAudioSeconds approximates duration from PCM file size + assumed
// 24kHz mono s16 sample rate. This is just for timeline sizing — actual
// playback uses audio.duration once loaded.
function estimateAudioSeconds(msg: Message): number | null {
  for (const part of msg.parts ?? []) {
    if (part.type !== "audio" || !part.media) continue;
    if (part.media.duration && part.media.duration > 0) return part.media.duration;
    const bytes = part.media.size_bytes ?? (part.media.size_kb ? part.media.size_kb * 1024 : 0);
    if (bytes > 0) return bytes / (2 * 24000);
  }
  return null;
}

const ROLE_COLOR: Record<string, string> = {
  user: "bg-[#2563EB]",
  assistant: "bg-[#10B981]",
  system: "bg-[#8B5CF6]",
  tool: "bg-[#F59E0B]",
};

export function ConversationPlayer({ messages, activeIdx, onActiveChange }: ConversationPlayerProps) {
  const audioRef = useRef<HTMLAudioElement | null>(null);
  const stepIdxRef = useRef<number>(-1);
  const fallbackTimerRef = useRef<number | null>(null);
  const [playing, setPlaying] = useState(false);
  // currentStepIdx mirrors stepIdxRef but as state so the turn counter
  // re-renders on advance. Ref still drives playback to avoid extra
  // dependency churn.
  const [currentStepIdx, setCurrentStepIdx] = useState<number>(-1);

  const steps: PlaybackStep[] = useMemo(() => {
    return messages.map((msg, idx) => {
      const audioURL = pickPlayableAudioURL(msg);
      const audioSeconds = estimateAudioSeconds(msg);
      const estimated = audioSeconds ?? FIXED_TURN_SECONDS;
      return {
        messageIdx: idx,
        role: msg.role,
        audioURL: audioURL ?? undefined,
        fallbackSeconds: FIXED_TURN_SECONDS,
        estimatedSeconds: estimated,
      };
    });
  }, [messages]);

  const totalSeconds = useMemo(() => steps.reduce((sum, s) => sum + s.estimatedSeconds, 0), [steps]);

  const clearFallback = () => {
    if (fallbackTimerRef.current != null) {
      window.clearTimeout(fallbackTimerRef.current);
      fallbackTimerRef.current = null;
    }
  };

  const stop = useCallback(() => {
    clearFallback();
    if (audioRef.current) {
      audioRef.current.pause();
      audioRef.current.removeAttribute("src");
      audioRef.current.load();
    }
    stepIdxRef.current = -1;
    setCurrentStepIdx(-1);
    setPlaying(false);
    onActiveChange(null);
  }, [onActiveChange]);

  // playStep advances to the given step index and engages the right transport.
  // Audio steps load+play the file and rely on the <audio onEnded> handler to
  // advance. Text-only steps schedule a setTimeout that calls advance().
  const playStep = useCallback(
    (i: number) => {
      clearFallback();
      if (i < 0 || i >= steps.length) {
        stop();
        return;
      }
      const step = steps[i];
      stepIdxRef.current = i;
      setCurrentStepIdx(i);
      onActiveChange(step.messageIdx);

      if (step.audioURL && audioRef.current) {
        audioRef.current.src = step.audioURL;
        audioRef.current.play().catch((err) => {
          console.warn("conversation player: audio play failed", err);
          // Fall through to fixed-time advance so the timeline still progresses.
          fallbackTimerRef.current = window.setTimeout(() => playStep(i + 1), step.fallbackSeconds * 1000);
        });
      } else {
        // Text-only / unplayable audio — hold the highlight for fallbackSeconds, then advance.
        fallbackTimerRef.current = window.setTimeout(() => playStep(i + 1), step.fallbackSeconds * 1000);
      }
    },
    [steps, onActiveChange, stop],
  );

  const togglePlay = useCallback(() => {
    if (playing) {
      // Pause — keep stepIdxRef so resume continues at the same step.
      clearFallback();
      audioRef.current?.pause();
      setPlaying(false);
      return;
    }
    setPlaying(true);
    if (stepIdxRef.current < 0) {
      playStep(0);
    } else if (audioRef.current && audioRef.current.src && !audioRef.current.ended) {
      // Resume the in-flight audio.
      audioRef.current.play().catch(() => {
        // If browser blocks the resume for some reason, restart from current step.
        playStep(stepIdxRef.current);
      });
    } else {
      playStep(stepIdxRef.current);
    }
  }, [playing, playStep]);

  const jumpToStep = useCallback(
    (i: number) => {
      setPlaying(true);
      playStep(i);
    },
    [playStep],
  );

  const onAudioEnded = useCallback(() => {
    if (!playing) return;
    playStep(stepIdxRef.current + 1);
  }, [playing, playStep]);

  // Stop on unmount so we don't leave audio playing when the user navigates away.
  useEffect(() => {
    return () => {
      clearFallback();
      audioRef.current?.pause();
    };
  }, []);

  if (steps.length === 0) return null;

  // Sum estimated seconds up through the current step so we can render a
  // "0:14 / 0:42" elapsed-style label even when most turns are text-only.
  const elapsedSeconds = useMemo(() => {
    if (currentStepIdx < 0) return 0;
    return steps.slice(0, currentStepIdx).reduce((sum, s) => sum + s.estimatedSeconds, 0);
  }, [currentStepIdx, steps]);

  const fmt = (n: number) => {
    const m = Math.floor(n / 60);
    const s = Math.max(0, Math.floor(n - m * 60));
    return `${m}:${s.toString().padStart(2, "0")}`;
  };

  const currentTurn = currentStepIdx >= 0 ? currentStepIdx + 1 : null;

  return (
    <div className="rounded-xl border border-mist bg-surfaceshadow-sm p-3 space-y-2">
      <div className="flex items-center gap-3">
        <button
          onClick={togglePlay}
          className="rounded-full bg-[#2563EB] hover:bg-[#1D4ED8] text-white h-9 w-9 flex items-center justify-center transition-colors"
          title={playing ? "Pause" : "Play conversation"}
        >
          {playing ? <Pause className="h-4 w-4" /> : <Play className="h-4 w-4 ml-0.5" />}
        </button>
        <button
          onClick={stop}
          disabled={!playing && stepIdxRef.current < 0}
          className="rounded-full border border-mist hover:bg-[var(--c-surface-2)] h-9 w-9 flex items-center justify-center disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
          title="Stop"
        >
          <Square className="h-3.5 w-3.5 fill-current" />
        </button>
        <div className="text-[11px] text-fg-muted font-mono flex items-baseline gap-2">
          <span className="text-fg font-semibold">
            Turn {currentTurn ?? "—"} / {steps.length}
          </span>
          <span>·</span>
          <span>{fmt(elapsedSeconds)} / {fmt(totalSeconds)}</span>
        </div>
      </div>

      {/* Timeline: each segment is a turn, width ∝ estimated duration.
          Click any segment to jump to that turn. */}
      <div className="flex items-stretch gap-[2px] h-6 rounded overflow-hidden bg-mist/30">
        {steps.map((step, i) => {
          const widthPct = totalSeconds > 0 ? (step.estimatedSeconds / totalSeconds) * 100 : 100 / steps.length;
          const isActive = activeIdx === step.messageIdx;
          const color = ROLE_COLOR[step.role] ?? "bg-slate-400";
          return (
            <button
              key={i}
              onClick={() => jumpToStep(i)}
              style={{ width: `${widthPct}%` }}
              className={cn(
                "relative transition-opacity hover:opacity-80",
                color,
                isActive ? "opacity-100" : "opacity-50",
              )}
              title={`Turn ${i + 1} (${step.role}, ~${step.estimatedSeconds.toFixed(1)}s)${step.audioURL ? " — has audio" : ""}`}
            >
              {step.audioURL && (
                <span className="absolute inset-0 flex items-center justify-center text-white text-[9px]">🔊</span>
              )}
            </button>
          );
        })}
      </div>

      {/* Hidden audio element drives playback. Listening to onEnded advances
          the sequence; onError falls back so a single broken file doesn't
          stall the whole replay. */}
      <audio
        ref={audioRef}
        onEnded={onAudioEnded}
        onError={() => {
          if (playing) playStep(stepIdxRef.current + 1);
        }}
      />
    </div>
  );
}
