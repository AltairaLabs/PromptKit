import { useEffect, useState } from "react";
import { ArrowLeft } from "lucide-react";
import { ConversationThread } from "@/components/ConversationThread";
import { AssertionsPanel } from "@/components/AssertionsPanel";
import { useArenaAPI } from "@/hooks/useArenaAPI";
import type { RunResult } from "@/types";

import type { Message } from "@/types";

interface RunDetailProps {
  runId: string;
  onBack: () => void;
  onSelectMessage?: (index: number, message?: Message, allMessages?: Message[]) => void;
}

export function RunDetail({ runId, onBack, onSelectMessage }: RunDetailProps) {
  const { getResult } = useArenaAPI();
  const [result, setResult] = useState<RunResult | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    getResult(runId).then(setResult).catch((e: Error) => setError(e.message));
  }, [runId, getResult]);

  if (error) {
    return (
      <div className="rounded-xl border border-red-200 bg-red-50 p-6">
        <p className="text-[#EF4444]">Failed to load run: {error}</p>
        <button onClick={onBack} className="mt-4 flex items-center gap-2 text-sm text-[#2563EB] hover:underline">
          <ArrowLeft className="h-4 w-4" /> Back
        </button>
      </div>
    );
  }

  if (!result) {
    return (
      <div className="rounded-xl border border-mist bg-white p-6 text-center shadow-sm">
        <p className="text-slate-muted">Loading run details...</p>
      </div>
    );
  }

  const durationSec = result.Duration / 1_000_000_000;

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <button onClick={onBack} className="flex items-center gap-2 text-sm text-[#2563EB] hover:underline">
          <ArrowLeft className="h-4 w-4" /> Back
        </button>
        <h2 className="text-lg font-semibold text-deep-space">{result.ScenarioID}</h2>
        <span className={
          result.Error
            ? "rounded-full bg-red-50 border border-red-200 px-2.5 py-0.5 text-xs font-semibold text-[#EF4444]"
            : "rounded-full bg-emerald-50 border border-emerald-200 px-2.5 py-0.5 text-xs font-semibold text-[#10B981]"
        }>
          {result.Error ? "Failed" : "Passed"}
        </span>
      </div>

      {/* Metadata grid */}
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        {[
          { label: "Provider", value: result.ProviderID },
          { label: "Region", value: result.Region },
          { label: "Duration", value: `${durationSec.toFixed(1)}s` },
          { label: "Cost", value: `$${result.Cost.total_cost_usd.toFixed(4)}` },
          { label: "Input Tokens", value: result.Cost.input_tokens.toLocaleString() },
          { label: "Output Tokens", value: result.Cost.output_tokens.toLocaleString() },
          { label: "Turns", value: String(result.Messages.length) },
          { label: "Pack", value: result.PromptPack || "—" },
        ].map((m) => (
          <div key={m.label} className="rounded-xl border border-mist bg-white p-3 shadow-sm">
            <div className="text-xs text-slate-muted uppercase tracking-wider">{m.label}</div>
            <div className="text-sm font-mono text-deep-space mt-1">{m.value}</div>
          </div>
        ))}
      </div>

      {result.Error && (
        <div className="rounded-xl border border-red-200 bg-red-50 p-4">
          <div className="text-sm text-[#EF4444]">{result.Error}</div>
        </div>
      )}

      <AssertionsPanel assertions={result.ConversationAssertions} />

      <div>
        <h3 className="text-xs font-semibold text-slate-muted uppercase tracking-wider mb-3">
          Conversation
        </h3>
        <ConversationThread
          messages={result.Messages}
          onSelectMessage={(i, msg) => onSelectMessage?.(i, msg, result.Messages)}
        />
      </div>
    </div>
  );
}
