import { useState } from "react";
import { cn } from "@/lib/utils";
import { Activity, ChevronDown, ExternalLink } from "lucide-react";
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
      <div className="rounded-xl border border-white/[0.06] bg-onyx p-12 text-center">
        <div className="mx-auto mb-4 h-12 w-12 rounded-full bg-altair-blue/10 flex items-center justify-center">
          <Activity className="h-6 w-6 text-altair-blue" />
        </div>
        <p className="text-sm text-slate-muted">No runs yet</p>
        <p className="text-xs text-slate-muted/60 mt-1">Click "Start Run" to begin testing</p>
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
      <div className="flex items-center gap-3 mb-3">
        <h3 className="text-sm font-semibold bg-gradient-to-r from-[#3B82F6] via-[#06B6D4] to-[#8B5CF6] bg-clip-text text-transparent uppercase tracking-widest">
          {title}
        </h3>
        <span className="text-xs font-mono text-slate-muted/60 bg-white/[0.04] rounded-full px-2 py-0.5">{count}</span>
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
        "rounded-xl border bg-onyx overflow-hidden transition-all cursor-pointer",
        expanded ? "border-altair-blue/30" : "border-white/[0.06] hover:border-white/[0.12]"
      )}
      onClick={onToggle}
    >
      <div className="flex items-center justify-between px-5 py-4">
        <div className="flex items-center gap-4">
          <StatusIndicator status={run.status} />
          <div>
            <div className="text-sm font-medium text-cloud-white">{run.scenario}</div>
            <div className="text-xs text-slate-muted mt-0.5">
              {run.provider}
              <span className="text-white/20 mx-1.5">·</span>
              {run.region}
            </div>
          </div>
        </div>
        <div className="flex items-center gap-4">
          <div className="text-right">
            <div className="text-xs font-mono text-slate-muted">Turn {run.turnIndex}</div>
            {run.costs.totalCost > 0 && (
              <div className="text-xs font-mono text-stellar-gold">${run.costs.totalCost.toFixed(4)}</div>
            )}
          </div>
          <div className="flex items-center gap-2">
            {onViewDetails && (
              <button
                className="flex items-center gap-1 rounded-lg border border-altair-blue/20 bg-altair-blue/[0.08] px-3 py-1.5 text-xs font-medium text-altair-blue hover:bg-altair-blue/[0.15] transition-colors"
                onClick={(e) => { e.stopPropagation(); onViewDetails(); }}
              >
                Details <ExternalLink className="h-3 w-3" />
              </button>
            )}
            <ChevronDown className={cn("h-4 w-4 text-slate-muted transition-transform", expanded && "rotate-180")} />
          </div>
        </div>
      </div>
      {expanded && run.messages.length > 0 && (
        <div className="border-t border-white/[0.06] px-5 py-4 bg-deep-space/50">
          <div className="space-y-2.5 max-h-[400px] overflow-y-auto pr-2">
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
  const styles: Record<string, string> = {
    user: "border-l-altair-blue bg-altair-blue/[0.06]",
    assistant: "border-l-deploy-green bg-deploy-green/[0.06]",
    system: "border-l-cosmic-violet bg-cosmic-violet/[0.06]",
    tool: "border-l-stellar-gold bg-stellar-gold/[0.06]",
  };
  const labelColors: Record<string, string> = {
    user: "text-altair-blue",
    assistant: "text-deploy-green",
    system: "text-cosmic-violet",
    tool: "text-stellar-gold",
  };
  return (
    <div className={cn("rounded-lg border-l-[3px] px-4 py-3", styles[msg.role] || styles.system)}>
      <span className={cn("text-[10px] font-bold uppercase tracking-widest", labelColors[msg.role] || labelColors.system)}>
        {msg.role}
      </span>
      <p className="mt-1.5 text-sm text-cloud-white/90 leading-relaxed whitespace-pre-wrap">
        {msg.content?.slice(0, 300)}
        {(msg.content?.length ?? 0) > 300 ? <span className="text-slate-muted">...</span> : ""}
      </p>
    </div>
  );
}

function StatusIndicator({ status }: { status: string }) {
  return (
    <div className={cn(
      "h-9 w-9 rounded-lg flex items-center justify-center",
      status === "running" && "bg-deploy-green/10",
      status === "completed" && "bg-deploy-green/10",
      status === "failed" && "bg-error-red/10",
    )}>
      <span className={cn(
        "h-2.5 w-2.5 rounded-full",
        status === "running" && "bg-deploy-green animate-pulse",
        status === "completed" && "bg-deploy-green",
        status === "failed" && "bg-error-red",
      )} />
    </div>
  );
}
