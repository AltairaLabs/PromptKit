import { useState } from "react";
import { cn } from "@/lib/utils";
import { ChevronDown, ExternalLink, Activity } from "lucide-react";
import type { ActiveRun, MessageCreatedData } from "@/types";

interface RunProgressProps {
  runs: ActiveRun[];
  onSelectRun?: (runId: string) => void;
}

export function RunProgress({ runs, onSelectRun }: RunProgressProps) {
  const [expandedRun, setExpandedRun] = useState<string | null>(null);
  const activeRuns = runs.filter((r) => r.status === "running");
  const doneRuns = runs.filter((r) => r.status !== "running");

  const toggleRun = (runId: string) => {
    setExpandedRun(expandedRun === runId ? null : runId);
  };

  if (runs.length === 0) {
    return (
      <div className="rounded-xl border border-mist bg-white p-10 text-center shadow-sm">
        <div className="mx-auto mb-3 h-10 w-10 rounded-full bg-blue-50 flex items-center justify-center">
          <Activity className="h-5 w-5 text-[#2563EB]" />
        </div>
        <p className="text-sm font-medium text-deep-space">No runs yet</p>
        <p className="text-xs text-slate-muted mt-1">Click "Start Run" to begin testing</p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {activeRuns.length > 0 && (
        <Section title="Active Runs" count={activeRuns.length}>
          {activeRuns.map((run) => (
            <RunCard key={run.runId} run={run} expanded={expandedRun === run.runId} onToggle={() => toggleRun(run.runId)} />
          ))}
        </Section>
      )}
      {doneRuns.length > 0 && (
        <Section title="Completed" count={doneRuns.length}>
          {doneRuns.map((run) => (
            <RunCard
              key={run.runId}
              run={run}
              expanded={expandedRun === run.runId}
              onToggle={() => toggleRun(run.runId)}
              onViewDetails={() => onSelectRun?.(run.runId)}
            />
          ))}
        </Section>
      )}
    </div>
  );
}

function Section({ title, count, children }: { title: string; count: number; children: React.ReactNode }) {
  return (
    <div>
      <div className="flex items-center gap-2 mb-3">
        <h3 className="text-xs font-semibold text-slate-muted uppercase tracking-wider">{title}</h3>
        <span className="text-[10px] font-mono text-slate-muted bg-[#F1F5F9] rounded-full px-1.5 py-0.5">{count}</span>
      </div>
      <div className="space-y-2">{children}</div>
    </div>
  );
}

function RunCard({ run, expanded, onToggle, onViewDetails }: {
  run: ActiveRun;
  expanded: boolean;
  onToggle: () => void;
  onViewDetails?: () => void;
}) {
  return (
    <div
      className={cn(
        "rounded-xl border bg-white shadow-sm overflow-hidden cursor-pointer transition-colors",
        expanded ? "border-[#2563EB]/40" : "border-mist hover:border-[#2563EB]/20"
      )}
      onClick={onToggle}
    >
      <div className="flex items-center justify-between px-4 py-3">
        <div className="flex items-center gap-3">
          <span className={cn(
            "h-2 w-2 rounded-full",
            run.status === "running" && "bg-[#10B981] animate-pulse",
            run.status === "completed" && "bg-[#10B981]",
            run.status === "failed" && "bg-[#EF4444]",
          )} />
          <div>
            <span className="text-sm font-medium text-deep-space">{run.scenario}</span>
            <span className="text-xs text-slate-muted ml-2">{run.provider} · {run.region}</span>
          </div>
        </div>
        <div className="flex items-center gap-3 text-xs">
          <span className="text-slate-muted font-mono">T{run.turnIndex}</span>
          {run.costs.totalCost > 0 && <span className="font-mono text-[#F59E0B]">${run.costs.totalCost.toFixed(4)}</span>}
          {run.status === "completed" && (
            <span className="rounded-full bg-emerald-50 text-[#10B981] px-2 py-0.5 text-[10px] font-semibold">Pass</span>
          )}
          {run.status === "failed" && (
            <span className="rounded-full bg-red-50 text-[#EF4444] px-2 py-0.5 text-[10px] font-semibold">Fail</span>
          )}
          {onViewDetails && (
            <button
              className="text-[#2563EB] hover:underline flex items-center gap-1"
              onClick={(e) => { e.stopPropagation(); onViewDetails(); }}
            >
              Details <ExternalLink className="h-3 w-3" />
            </button>
          )}
          <ChevronDown className={cn("h-3.5 w-3.5 text-slate-muted transition-transform", expanded && "rotate-180")} />
        </div>
      </div>
      {expanded && run.messages.length > 0 && (
        <div className="border-t border-mist px-4 py-3 bg-[#F8FAFC]">
          <div className="space-y-2 max-h-80 overflow-y-auto">
            {run.messages.map((msg, i) => (
              <MessagePreview key={i} msg={msg} />
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function MessagePreview({ msg }: { msg: MessageCreatedData }) {
  const border: Record<string, string> = {
    user: "border-l-[#2563EB]",
    assistant: "border-l-[#10B981]",
    system: "border-l-[#8B5CF6]",
    tool: "border-l-[#F59E0B]",
  };
  const label: Record<string, string> = {
    user: "text-[#2563EB]",
    assistant: "text-[#10B981]",
    system: "text-[#8B5CF6]",
    tool: "text-[#F59E0B]",
  };
  return (
    <div className={cn("rounded-lg border-l-[3px] bg-white px-3 py-2", border[msg.role] || border.system)}>
      <span className={cn("text-[10px] font-bold uppercase tracking-wider", label[msg.role] || label.system)}>{msg.role}</span>
      <p className="mt-1 text-sm text-deep-space/80 leading-relaxed whitespace-pre-wrap">
        {msg.content?.slice(0, 200)}{(msg.content?.length ?? 0) > 200 ? "…" : ""}
      </p>
    </div>
  );
}
