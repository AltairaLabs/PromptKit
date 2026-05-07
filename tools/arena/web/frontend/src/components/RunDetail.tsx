import { useEffect, useMemo, useState } from "react";
import { ArrowLeft } from "lucide-react";
import { ConversationThread } from "@/components/ConversationThread";
import { ConversationPlayer } from "@/components/ConversationPlayer";
import { AssertionsPanel } from "@/components/AssertionsPanel";
import { EvalsPanel } from "@/components/EvalsPanel";
import { MediaOutputsPanel } from "@/components/MediaOutputsPanel";
import { A2AAgentsPanel } from "@/components/A2AAgentsPanel";
import { TrialResultsPanel } from "@/components/TrialResultsPanel";
import { useArenaAPI } from "@/hooks/useArenaAPI";
import type { ActiveRun, MessageCreatedData, RunResult } from "@/types";

import type { Message } from "@/types";

interface RunDetailProps {
  runId: string;
  liveRun?: ActiveRun;
  listeningRunId: string | null;
  onToggleListen: (runId: string) => void;
  onBack: () => void;
  onSelectMessage?: (index: number, message?: Message, allMessages?: Message[]) => void;
}

// liveMessageToMessage maps an in-flight SSE MessageCreatedData onto the
// shape ConversationThread renders. Live messages don't yet carry parts /
// timestamp / cost_info — those land when the static result is fetched.
function liveMessageToMessage(m: MessageCreatedData): Message {
  return {
    role: m.role,
    content: m.content,
    tool_calls: m.toolCalls,
    tool_result: m.toolResult ?? undefined,
  };
}

export function RunDetail({
  runId,
  liveRun,
  listeningRunId,
  onToggleListen,
  onBack,
  onSelectMessage,
}: RunDetailProps) {
  const { getResult } = useArenaAPI();
  const [result, setResult] = useState<RunResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  // playbackIdx is the message index currently being played back through
  // ConversationPlayer; lifted here so ConversationThread can highlight and
  // scroll the bubble into view in sync with the audio.
  const [playbackIdx, setPlaybackIdx] = useState<number | null>(null);

  const isLive = liveRun?.status === "running";
  const isListening = listeningRunId === runId;

  // Initial fetch of the saved run result. May 404 while the run is still
  // in progress — that's expected, we render liveRun.messages until the
  // result lands.
  useEffect(() => {
    getResult(runId)
      .then(setResult)
      .catch((e: Error) => {
        if (!liveRun) {
          setError(e.message);
        }
      });
  }, [runId, getResult, liveRun]);

  // Re-fetch when the live run flips to completed/failed so we get the full
  // saved state (cost, assertions, audio refs, etc.).
  useEffect(() => {
    if (liveRun && (liveRun.status === "completed" || liveRun.status === "failed")) {
      getResult(runId)
        .then(setResult)
        .catch((e: Error) => {
          console.warn("getResult after run completion failed:", e.message);
        });
    }
  }, [liveRun?.status, runId, getResult]);

  // Build the conversation to render. While the run is live, prefer the
  // SSE-streamed messages — they update turn-by-turn. The static `result`
  // is only refreshed on mount and on completion, so during a run it's a
  // stale snapshot (and would mask the live stream if preferred). After
  // completion we use `result` because it carries the richer shape (parts,
  // validations, per-message cost_info).
  //
  // Live messages are sorted by `index` to handle the case where SSE events
  // arrive out of dispatch order (worker pool, network jitter). Without this
  // a tool result can render before its preceding assistant tool_call.
  const displayMessages: Message[] = useMemo(() => {
    if (isLive && liveRun?.messages?.length) {
      return [...liveRun.messages]
        .sort((a, b) => (a.index ?? 0) - (b.index ?? 0))
        .map(liveMessageToMessage);
    }
    if (result?.Messages?.length) return result.Messages;
    if (liveRun?.messages?.length) {
      return [...liveRun.messages]
        .sort((a, b) => (a.index ?? 0) - (b.index ?? 0))
        .map(liveMessageToMessage);
    }
    return [];
  }, [result, liveRun, isLive]);

  if (error && !liveRun) {
    return (
      <div className="rounded-xl border border-red-200 bg-red-50 p-6">
        <p className="text-[#EF4444]">Failed to load run: {error}</p>
        <button onClick={onBack} className="mt-4 flex items-center gap-2 text-sm text-[#2563EB] hover:underline">
          <ArrowLeft className="h-4 w-4" /> Back
        </button>
      </div>
    );
  }

  if (!result && !liveRun) {
    return <RunDetailSkeleton onBack={onBack} />;
  }

  // Header data: prefer the saved result; fall back to live run while in flight.
  const scenarioID = result?.ScenarioID ?? liveRun?.scenario ?? runId;
  const providerID = result?.ProviderID ?? liveRun?.provider ?? "—";
  const region = result?.Region ?? liveRun?.region ?? "—";
  const durationSec = result ? result.Duration / 1_000_000_000 : null;
  const totalCost = result?.Cost?.total_cost_usd ?? liveRun?.costs.totalCost ?? 0;
  const inputTokens = result?.Cost?.input_tokens ?? liveRun?.costs.inputTokens ?? 0;
  const outputTokens = result?.Cost?.output_tokens ?? liveRun?.costs.outputTokens ?? 0;
  const turnCount = result?.Messages?.length ?? liveRun?.messages.length ?? 0;
  const promptPack = result?.PromptPack || "—";

  const statusLabel = isLive
    ? "Live"
    : (result?.Error || liveRun?.status === "failed") ? "Failed" : "Passed";
  const statusClass = isLive
    ? "rounded-full bg-blue-50 border border-blue-200 px-2.5 py-0.5 text-xs font-semibold text-[#2563EB] flex items-center gap-1.5"
    : (result?.Error || liveRun?.status === "failed")
      ? "rounded-full bg-red-50 border border-red-200 px-2.5 py-0.5 text-xs font-semibold text-[#EF4444]"
      : "rounded-full bg-emerald-50 border border-emerald-200 px-2.5 py-0.5 text-xs font-semibold text-[#10B981]";

  return (
    <div>
      {/* Sticky top section — header, summary metadata, error, assertions
          and the player bar all stay visible while the conversation scrolls
          underneath. The negative horizontal margins + matching padding
          extend the background to the page gutters so content scrolling
          underneath doesn't show through. `top-0` is relative to the
          nearest scrolling ancestor (the page body in our layout). */}
      <div className="sticky top-0 z-20 -mx-6 px-6 pt-2 pb-4 bg-canvas border-b border-mist space-y-4">
        <div className="flex items-center gap-4">
          <button onClick={onBack} className="flex items-center gap-2 text-sm text-[#2563EB] hover:underline">
            <ArrowLeft className="h-4 w-4" /> Back
          </button>
          <h2 className="text-lg font-semibold text-fg truncate">{scenarioID}</h2>
          <span className={statusClass}>
            {isLive && <span className="h-1.5 w-1.5 rounded-full bg-[#2563EB] animate-pulse" />}
            {statusLabel}
          </span>
          {isLive && (
            <button
              onClick={() => onToggleListen(runId)}
              className={
                isListening
                  ? "ml-auto rounded-lg border border-blue-200 bg-blue-50 px-3 py-1.5 text-xs font-medium text-[#2563EB] hover:bg-blue-100 transition-colors"
                  : "ml-auto rounded-lg border border-mist bg-surfacepx-3 py-1.5 text-xs font-medium text-fg hover:bg-[var(--c-surface-2)] transition-colors"
              }
              title={isListening ? "Stop audio playback" : "Listen to live audio (user → left, agent → right)"}
            >
              {isListening ? "🔇 Stop" : "🔊 Listen"}
            </button>
          )}
        </div>

        {/* Compact metadata strip — single row, smaller than the original
            8-card grid so the sticky header stays a reasonable height. */}
        <div className="flex flex-wrap items-baseline gap-x-5 gap-y-1 text-xs">
          <Metric label="Provider" value={providerID} />
          <Metric label="Region" value={region} />
          {durationSec != null && <Metric label="Duration" value={`${durationSec.toFixed(1)}s`} />}
          <Metric label="Cost" value={`$${totalCost.toFixed(4)}`} mono />
          <Metric label="Tokens" value={`${inputTokens.toLocaleString()} → ${outputTokens.toLocaleString()}`} mono />
          <Metric label="Turns" value={String(turnCount)} />
          {promptPack && promptPack !== "—" && <Metric label="Pack" value={promptPack} />}
        </div>

        {(result?.Error || liveRun?.error) && (
          <div className="rounded-lg border border-red-200 bg-red-50 px-3 py-2">
            <div className="text-xs text-[#EF4444]">{result?.Error ?? liveRun?.error}</div>
          </div>
        )}

        {result?.ConversationAssertions && (
          <AssertionsPanel assertions={result.ConversationAssertions} compactDefault />
        )}

        {/* Player only meaningful for completed runs (live runs are
            streaming via SSE audio + messages, no point replaying). */}
        {!isLive && displayMessages.length > 0 && (
          <ConversationPlayer
            messages={displayMessages}
            activeIdx={playbackIdx}
            onActiveChange={setPlaybackIdx}
          />
        )}
      </div>

      {/* Scrollable content below the sticky chrome. Conversation is the
          primary surface; below it we surface the same panels the HTML
          report renders — evals, trials, media, A2A — so the web UI is
          a complete view of the run, not a stripped-down preview. */}
      <div className="pt-4 space-y-6">
        <div className="space-y-3">
          <h3 className="text-xs font-semibold text-fg-muted uppercase tracking-wider flex items-center gap-2">
            Conversation
            {isLive && (
              <span className="inline-flex items-center gap-1 text-[10px] font-normal text-[#2563EB]">
                <span className="h-1.5 w-1.5 rounded-full bg-[#2563EB] animate-pulse" /> streaming
              </span>
            )}
          </h3>
          <ConversationThread
            messages={displayMessages}
            activeIdx={playbackIdx}
            streaming={isLive}
            onSelectMessage={(i, msg) => onSelectMessage?.(i, msg, displayMessages)}
          />
        </div>

        <TrialResultsPanel trial={result?.TrialResults} />
        <EvalsPanel evals={result?.eval_results} />
        <MediaOutputsPanel outputs={result?.MediaOutputs} />
        <A2AAgentsPanel agents={result?.A2AAgents} />

        {result?.Labels && Object.keys(result.Labels).length > 0 && (
          <div className="space-y-2">
            <h3 className="text-xs font-semibold text-fg-muted uppercase tracking-wider">Labels</h3>
            <div className="flex flex-wrap gap-1.5">
              {Object.entries(result.Labels).map(([k, v]) => (
                <span
                  key={k}
                  className="inline-flex items-center gap-1 rounded-full bg-[var(--c-surface-2)] border border-mist px-2 py-0.5 text-[11px] font-mono text-fg"
                >
                  <span className="text-fg-muted">{k}</span>
                  <span>=</span>
                  <span>{v}</span>
                </span>
              ))}
            </div>
          </div>
        )}

        {result?.RecordingPath && (
          <div className="space-y-1">
            <h3 className="text-xs font-semibold text-fg-muted uppercase tracking-wider">Recording</h3>
            <div className="text-[11px] font-mono text-fg-muted truncate" title={result.RecordingPath}>
              {result.RecordingPath}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function Metric({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <span className="inline-flex items-baseline gap-1.5">
      <span className="text-[10px] uppercase tracking-wider text-fg-muted">{label}</span>
      <span className={mono ? "text-xs font-mono text-fg" : "text-xs text-fg"}>{value}</span>
    </span>
  );
}

// RunDetailSkeleton renders a layout-shaped pulse so the eye lands on
// where the data will be, instead of the page rearranging when the fetch
// resolves. Matches the real RunDetail's stripes (header + metadata +
// player + thread).
function RunDetailSkeleton({ onBack }: { onBack: () => void }) {
  return (
    <div>
      <div className="sticky top-0 z-20 -mx-6 px-6 pt-2 pb-4 bg-canvas border-b border-mist space-y-4">
        <div className="flex items-center gap-4">
          <button onClick={onBack} className="flex items-center gap-2 text-sm text-[#2563EB] hover:underline">
            <ArrowLeft className="h-4 w-4" /> Back
          </button>
          <div className="h-5 w-48 rounded bg-mist animate-pulse" />
        </div>
        <div className="flex flex-wrap gap-x-5 gap-y-1.5">
          {[60, 50, 70, 80, 110, 50, 70].map((w, i) => (
            <div key={i} className="h-3 rounded bg-mist animate-pulse" style={{ width: w }} />
          ))}
        </div>
        <div className="h-12 rounded-xl bg-mist animate-pulse" />
      </div>
      <div className="pt-4 space-y-3">
        {[120, 90, 150, 110].map((h, i) => (
          <div key={i} className="rounded-lg bg-mist/60 animate-pulse" style={{ height: h }} />
        ))}
      </div>
    </div>
  );
}
