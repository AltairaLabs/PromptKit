import { useEffect, useMemo, useRef, useState } from "react";
import { cn } from "@/lib/utils";
import { ChevronRight, Copy, Check } from "lucide-react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import type { Message, MessageToolCall } from "@/types";

interface InlineAssertion {
  passed: boolean;
  type: string;
  name?: string;
}

// readInlineAssertions pulls the per-turn assertion list out of message
// meta. Defensive: meta is `Record<string, unknown>` so we shape-check
// each access. Format mirrors what arena writes via emitAssertions.
function readInlineAssertions(msg: Message): InlineAssertion[] {
  const a = msg.meta?.assertions as Record<string, unknown> | undefined;
  if (!a) return [];
  const results = a.results;
  if (!Array.isArray(results)) return [];
  return results
    .map((r): InlineAssertion | null => {
      if (typeof r !== "object" || r === null) return null;
      const o = r as Record<string, unknown>;
      if (typeof o.type !== "string") return null;
      return {
        passed: o.passed === true,
        type: o.type,
        name: typeof o.name === "string" ? o.name : undefined,
      };
    })
    .filter((x): x is InlineAssertion => x !== null);
}

interface ConversationThreadProps {
  messages: Message[];
  // activeIdx is the message currently being played back via ConversationPlayer.
  // The matching bubble gets a glow and is scrolled into view.
  activeIdx?: number | null;
  // streaming flips on the "↓ N new" jump-to-latest button when the user
  // scrolls up while messages keep arriving.
  streaming?: boolean;
  onSelectMessage?: (index: number, message: Message) => void;
}

const roleStyles: Record<string, { bg: string; border: string; label: string }> = {
  user: { bg: "bg-[var(--c-bubble-user)]", border: "border-l-[#2563EB]", label: "text-[#2563EB]" },
  assistant: { bg: "bg-[var(--c-bubble-assistant)]", border: "border-l-[#10B981]", label: "text-[#10B981]" },
  system: { bg: "bg-[var(--c-bubble-system)]", border: "border-l-[#8B5CF6]", label: "text-[#8B5CF6]" },
  tool: { bg: "bg-[var(--c-bubble-tool)]", border: "border-l-[#F59E0B]", label: "text-[#F59E0B]" },
};

// toolIcon mirrors the HTML report's tool-call grouping: agent-callable
// tools (a2a__*) get 🤖, workflow tools (workflow__*) ⚡, memory tools
// (memory__*) 🧠, everything else falls through.
function toolIcon(name: string): string {
  if (name.startsWith("a2a__")) return "🤖";
  if (name.startsWith("workflow__")) return "⚡";
  if (name.startsWith("memory__")) return "🧠";
  return "🔧";
}

// hasMedia reports whether the message carries any non-text content parts.
// Matches the HTML report's "renderToolResultMediaBadges" / per-message badges.
function mediaCounts(msg: Message): { images: number; audio: number; video: number; documents: number } {
  const c = { images: 0, audio: 0, video: 0, documents: 0 };
  const parts = msg.parts ?? [];
  for (const p of parts) {
    if (p.type === "image") c.images++;
    else if (p.type === "audio") c.audio++;
    else if (p.type === "video") c.video++;
    else if (p.type === "document") c.documents++;
  }
  return c;
}

// mediaURL converts a storage_reference (e.g. "out/media/runs/.../foo.wav")
// into a URL the browser can load. The arena server mounts saved-run media
// at /api/media/<rel> where <rel> is the path under outputDir.
function mediaURL(ref?: string): string | null {
  if (!ref) return null;
  const stripped = ref.replace(/^\/?out\//, "");
  return `/api/media/${stripped}`;
}


export function ConversationThread({ messages, activeIdx, streaming, onSelectMessage }: ConversationThreadProps) {
  const tailRef = useRef<HTMLDivElement | null>(null);
  const [showJump, setShowJump] = useState(false);
  const lastSeenCountRef = useRef(messages.length);

  // Watch the page's scroll position; show jump-to-latest button when the
  // user has scrolled up more than ~600px and new messages have arrived.
  useEffect(() => {
    if (!streaming) {
      setShowJump(false);
      return;
    }
    const onScroll = () => {
      const distFromBottom = document.documentElement.scrollHeight
        - window.scrollY - window.innerHeight;
      setShowJump(distFromBottom > 600 && messages.length > lastSeenCountRef.current);
    };
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, [messages.length, streaming]);

  useEffect(() => {
    lastSeenCountRef.current = messages.length;
  }, [messages.length]);

  const newCount = Math.max(0, messages.length - lastSeenCountRef.current);

  // Mark messages that are tool results paired with the immediately
  // preceding assistant turn's tool call. Used by MessageBubble to render
  // a "↳ result for X" connector in the bubble header so the relationship
  // is visually obvious.
  const pairedTo = useMemo(() => {
    const map = new Map<number, string>();
    for (let i = 1; i < messages.length; i++) {
      const m = messages[i];
      if (m.role !== "tool" || !m.tool_result) continue;
      const prev = messages[i - 1];
      if (prev?.role === "assistant" && prev.tool_calls?.some((tc) => tc.id === m.tool_result?.id)) {
        map.set(i, m.tool_result.name);
      }
    }
    return map;
  }, [messages]);

  return (
    <div className="space-y-3 relative">
      {messages.map((msg, i) => (
        <MessageBubble
          key={i}
          message={msg}
          index={i}
          isActive={activeIdx === i}
          onSelect={onSelectMessage}
          pairedToToolName={pairedTo.get(i)}
        />
      ))}
      <div ref={tailRef} />
      {showJump && (
        <button
          type="button"
          onClick={() => {
            tailRef.current?.scrollIntoView({ behavior: "smooth", block: "end" });
          }}
          className="fixed bottom-6 left-1/2 -translate-x-1/2 z-30 rounded-full bg-[#2563EB] hover:bg-[#1D4ED8] text-white text-xs font-semibold px-3 py-1.5 shadow-lg flex items-center gap-1.5"
        >
          ↓ {newCount > 0 ? `${newCount} new` : "Latest"}
        </button>
      )}
    </div>
  );
}

function MessageBubble({
  message,
  index,
  isActive,
  onSelect,
  pairedToToolName,
}: {
  message: Message;
  index: number;
  isActive?: boolean;
  onSelect?: (i: number, msg: Message) => void;
  pairedToToolName?: string;
}) {
  const style = roleStyles[message.role] ?? roleStyles.system;
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [copied, setCopied] = useState(false);

  // Scroll the active (playback) bubble into view when the player advances.
  // Use 'nearest' so we don't yank the page if the bubble is already visible.
  useEffect(() => {
    if (isActive && containerRef.current) {
      containerRef.current.scrollIntoView({ behavior: "smooth", block: "nearest" });
    }
  }, [isActive]);
  const persona = (message.meta?.persona as string | undefined) ?? "";
  const isSelfplay = (message.meta?.self_play as boolean | undefined) === true;
  const media = mediaCounts(message);
  const mediaTotal = media.images + media.audio + media.video + media.documents;
  const inlineAssertions = readInlineAssertions(message);

  const handleCopy = (e: React.MouseEvent) => {
    e.stopPropagation();
    const text = message.content ?? "";
    navigator.clipboard.writeText(text).then(
      () => {
        setCopied(true);
        window.setTimeout(() => setCopied(false), 1200);
      },
      () => undefined,
    );
  };

  return (
    <div
      ref={containerRef}
      className={cn(
        "group rounded-lg border-l-[3px] px-4 py-3 cursor-pointer transition-all hover:brightness-95",
        style.bg,
        style.border,
        isActive && "ring-2 ring-[#2563EB]/60 ring-offset-2 shadow-lg",
        pairedToToolName && "ml-6",
      )}
      onClick={() => onSelect?.(index, message)}
    >
      <div className="flex items-center justify-between mb-1.5 gap-3">
        <div className="flex items-center gap-2 min-w-0 flex-wrap">
          <span className={cn("text-[10px] font-bold uppercase tracking-widest", style.label)}>
            {message.role}
          </span>
          <span className="text-[10px] font-mono text-fg-muted">#{index}</span>
          {pairedToToolName && (
            <span className="inline-flex items-center gap-1 text-[10px] text-fg-muted" title="Tool result paired with prior assistant tool call">
              ↳ <span className="font-mono">{pairedToToolName}</span>
            </span>
          )}
          {isSelfplay && persona && (
            <span
              className="inline-flex items-center gap-1 rounded-full bg-violet-100 text-[#8B5CF6] px-2 py-0.5 text-[10px] font-semibold"
              title={`Self-play persona: ${persona}`}
            >
              <span>🤖</span>
              <span className="font-mono">{persona}</span>
            </span>
          )}
          {mediaTotal > 0 && (
            <span className="inline-flex items-center gap-1 text-[10px] text-fg-muted">
              {media.images > 0 && <span title={`${media.images} image part${media.images > 1 ? "s" : ""}`}>🖼️ {media.images}</span>}
              {media.audio > 0 && <span title={`${media.audio} audio part${media.audio > 1 ? "s" : ""}`}>🔊 {media.audio}</span>}
              {media.video > 0 && <span title={`${media.video} video part${media.video > 1 ? "s" : ""}`}>🎬 {media.video}</span>}
              {media.documents > 0 && <span title={`${media.documents} document part${media.documents > 1 ? "s" : ""}`}>📄 {media.documents}</span>}
            </span>
          )}
        </div>
        <div className="flex items-center gap-2 shrink-0">
          {message.cost_info && (
            <span className="text-[11px] font-mono text-[#F59E0B]" title="Total cost (USD)">
              ${message.cost_info.total_cost_usd.toFixed(4)}
            </span>
          )}
          {message.latency_ms != null && (
            <span className="text-[11px] text-fg-muted" title="Latency (ms)">{message.latency_ms}ms</span>
          )}
          {message.validations && message.validations.length > 0 && (
            <span
              className={cn(
                "text-[11px] font-bold",
                message.validations.every((v) => v.passed) ? "text-[#10B981]" : "text-[#EF4444]"
              )}
              title={`${message.validations.length} validation${message.validations.length > 1 ? "s" : ""}`}
            >
              {message.validations.every((v) => v.passed) ? "✓" : "✗"}
            </span>
          )}
          {message.content && (
            <button
              type="button"
              onClick={handleCopy}
              className="opacity-0 group-hover:opacity-100 transition-opacity rounded p-1 text-fg-muted hover:bg-white/40 hover:text-fg"
              title={copied ? "Copied!" : "Copy message text"}
              aria-label="Copy message text"
            >
              {copied ? <Check className="h-3 w-3 text-[#10B981]" /> : <Copy className="h-3 w-3" />}
            </button>
          )}
        </div>
      </div>

      {message.content && (
        <div className="markdown-message text-sm text-deep-space leading-relaxed">
          <ReactMarkdown remarkPlugins={[remarkGfm]}>{message.content}</ReactMarkdown>
        </div>
      )}

      {/* Multimodal parts inline. Audio is intentionally NOT rendered here
          — the top-of-page ConversationPlayer is the canonical playback
          surface for the saved run; per-message <audio> widgets would
          duplicate it (and risk two streams playing at once). The header
          badge counts (🔊 N) still flag turns that have audio. */}
      {message.parts && message.parts.length > 0 && (
        <div className="mt-2 space-y-2">
          {message.parts.map((part, j) => {
            if (part.type === "text") return null;
            if (part.type === "audio") return null;
            if (part.type === "image" && part.media) {
              const url = mediaURL(part.media.storage_reference ?? part.media.file_path ?? part.media.url);
              if (!url) return null;
              return (
                <img
                  key={j}
                  src={url}
                  alt="message image"
                  className="rounded border border-mist max-w-md max-h-64 object-contain"
                  onClick={(e) => e.stopPropagation()}
                />
              );
            }
            if (part.type === "video" && part.media) {
              const url = mediaURL(part.media.storage_reference ?? part.media.file_path ?? part.media.url);
              if (!url) return null;
              return (
                <video
                  key={j}
                  controls
                  preload="metadata"
                  src={url}
                  className="w-full max-w-md rounded border border-mist"
                  onClick={(e) => e.stopPropagation()}
                />
              );
            }
            if (part.type === "document" && part.media) {
              const url = mediaURL(part.media.storage_reference ?? part.media.file_path ?? part.media.url);
              if (!url) return null;
              return (
                <a
                  key={j}
                  href={url}
                  target="_blank"
                  rel="noopener"
                  onClick={(e) => e.stopPropagation()}
                  className="inline-flex items-center gap-1 text-xs text-[#2563EB] hover:underline"
                >
                  📄 {part.media.mime_type ?? "document"} — open
                </a>
              );
            }
            return null;
          })}
        </div>
      )}

      {message.tool_calls && message.tool_calls.length > 0 && (
        <div className="mt-3 space-y-2">
          {message.tool_calls.map((tc, idx) => (
            <ToolCallCard key={tc.id || `${tc.name}-${idx}`} call={tc} />
          ))}
        </div>
      )}

      {message.tool_result && (
        <div className="mt-3 rounded-lg bg-surface p-3 border border-mist">
          <div className="flex items-center gap-2 mb-1">
            <span className="rounded-full bg-amber-100 text-[#F59E0B] px-2 py-0.5 text-[10px] font-semibold">
              <span className="mr-1">{toolIcon(message.tool_result.name)}</span>
              {message.tool_result.name}
            </span>
            {message.tool_result.error && (
              <span className="rounded-full bg-red-100 text-[#EF4444] px-2 py-0.5 text-[10px] font-semibold">Error</span>
            )}
            {message.tool_result.latency_ms != null && (
              <span className="text-[11px] text-slate-muted">{message.tool_result.latency_ms}ms</span>
            )}
          </div>
          {message.tool_result.parts?.map((part, j) => (
            <div key={j} className="text-xs text-deep-space/70 mt-1">
              {part.type === "text" ? (
                <pre className="whitespace-pre-wrap font-mono bg-mist/30 px-2 py-1 rounded">{part.text}</pre>
              ) : (
                <span className="inline-flex items-center gap-1 rounded bg-mist/40 px-2 py-0.5">
                  {part.type === "image" && "🖼️"}
                  {part.type === "audio" && "🔊"}
                  {part.type === "video" && "🎬"}
                  {part.type === "document" && "📄"}
                  {part.type} ({part.media?.mime_type ?? "unknown"})
                </span>
              )}
            </div>
          ))}
          {message.tool_result.error && (
            <div className="text-xs text-[#EF4444] mt-1">{message.tool_result.error}</div>
          )}
        </div>
      )}

      {(message.validations?.length || inlineAssertions.length > 0) && (
        <div className="mt-3 flex flex-wrap gap-1">
          {message.validations?.map((v, j) => (
            <span
              key={`v-${j}`}
              className={cn(
                "rounded-full px-2 py-0.5 text-[10px] font-semibold",
                v.passed ? "bg-emerald-100 text-[#10B981]" : "bg-red-100 text-[#EF4444]"
              )}
              title={`Validator: ${v.validator_type}`}
            >
              {v.passed ? "✓" : "✗"} {v.validator_type}
            </span>
          ))}
          {inlineAssertions.map((a, j) => (
            <span
              key={`a-${j}`}
              className={cn(
                "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-semibold",
                a.passed
                  ? "bg-emerald-100 text-[#10B981]"
                  : "bg-red-100 text-[#EF4444]",
              )}
              title={`Assertion: ${a.name ?? a.type}`}
            >
              <span>🛡️</span>
              {a.passed ? "✓" : "✗"} {a.name ?? a.type}
            </span>
          ))}
        </div>
      )}
    </div>
  );
}

// ToolCallCard mirrors the HTML report's per-tool-call element: header always
// visible (icon + name + id + ▶ toggle), args panel collapses on click. One
// state per card, so multiple calls in a turn open/close independently.
function ToolCallCard({ call }: { call: MessageToolCall }) {
  const [expanded, setExpanded] = useState(false);
  const argsText = typeof call.args === "string" ? call.args : JSON.stringify(call.args, null, 2);
  const hasArgs = argsText && argsText.trim() !== "" && argsText !== "{}" && argsText !== "null";
  return (
    <div className="rounded-lg bg-surface border border-mist overflow-hidden">
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          if (hasArgs) setExpanded((x) => !x);
        }}
        className={cn(
          "w-full flex items-center gap-2 px-3 py-2 text-left",
          hasArgs ? "cursor-pointer hover:bg-[var(--c-surface-2)]" : "cursor-default",
        )}
      >
        <span className="rounded-full bg-violet-100 text-[#8B5CF6] px-2 py-0.5 text-[10px] font-semibold inline-flex items-center gap-1">
          <span>{toolIcon(call.name)}</span>
          {call.name}
        </span>
        {call.id && <span className="text-[10px] text-slate-muted font-mono truncate">{call.id}</span>}
        {hasArgs && (
          <ChevronRight
            className={cn(
              "ml-auto h-3 w-3 text-slate-muted transition-transform",
              expanded && "rotate-90",
            )}
          />
        )}
      </button>
      {hasArgs && expanded && (
        <div className="border-t border-mist px-3 py-2 bg-[var(--c-surface-2)]">
          <div className="text-[10px] uppercase tracking-wider text-slate-muted mb-1">Arguments</div>
          <pre className="text-xs font-mono text-deep-space/80 overflow-x-auto whitespace-pre-wrap">
            {argsText}
          </pre>
        </div>
      )}
    </div>
  );
}
