import { useState } from "react";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
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

  return (
    <div className="space-y-4">
      {activeRuns.length > 0 && (
        <div>
          <h3 className="text-sm font-medium text-slate-muted uppercase tracking-wider mb-3">
            Active Runs
          </h3>
          <div className="space-y-2">
            {activeRuns.map((run) => (
              <RunEntry
                key={run.runId}
                run={run}
                expanded={expandedRun === run.runId}
                onToggle={() => toggleRun(run.runId)}
              />
            ))}
          </div>
        </div>
      )}
      {doneRuns.length > 0 && (
        <div>
          <h3 className="text-sm font-medium text-slate-muted uppercase tracking-wider mb-3">
            Completed Runs
          </h3>
          <div className="space-y-2">
            {doneRuns.map((run) => (
              <RunEntry
                key={run.runId}
                run={run}
                expanded={expandedRun === run.runId}
                onToggle={() => toggleRun(run.runId)}
                onViewDetails={() => onSelectRun?.(run.runId)}
              />
            ))}
          </div>
        </div>
      )}
      {runs.length === 0 && (
        <Card className="bg-onyx border-white/10 p-8 text-center">
          <p className="text-slate-muted">No runs yet. Click "Start Run" to begin.</p>
        </Card>
      )}
    </div>
  );
}

function RunEntry({
  run,
  expanded,
  onToggle,
  onViewDetails,
}: {
  run: ActiveRun;
  expanded: boolean;
  onToggle: () => void;
  onViewDetails?: () => void;
}) {
  const isDone = run.status !== "running";
  return (
    <Card
      className={cn(
        "bg-onyx border-white/10 cursor-pointer transition-colors hover:border-altair-blue/50",
        expanded && "border-altair-blue/50"
      )}
      onClick={onToggle}
    >
      <div className="flex items-center justify-between p-4">
        <div className="flex items-center gap-3">
          <StatusDot status={run.status} />
          <div>
            <div className="font-medium text-sm">{run.scenario}</div>
            <div className="text-xs text-slate-muted">
              {run.provider} · {run.region}
            </div>
          </div>
        </div>
        <div className="flex items-center gap-3">
          <Badge variant="outline" className="text-xs border-white/10 text-slate-muted">
            Turn {run.turnIndex}
          </Badge>
          {run.costs.totalCost > 0 && (
            <span className="text-xs font-mono text-stellar-gold">
              ${run.costs.totalCost.toFixed(4)}
            </span>
          )}
          {run.status === "failed" && (
            <Badge className="bg-error-red/10 text-error-red border-error-red/30 text-xs">
              Failed
            </Badge>
          )}
          {run.status === "completed" && (
            <Badge className="bg-deploy-green/10 text-deploy-green border-deploy-green/30 text-xs">
              Done
            </Badge>
          )}
          <span className="text-xs text-slate-muted">{run.messages.length} msgs</span>
          {isDone && onViewDetails && (
            <button
              className="text-xs text-altair-blue hover:underline"
              onClick={(e) => { e.stopPropagation(); onViewDetails(); }}
            >
              View Details
            </button>
          )}
        </div>
      </div>
      {expanded && run.messages.length > 0 && (
        <div className="border-t border-white/10 p-4">
          <div className="space-y-2 max-h-96 overflow-y-auto">
            {run.messages.map((msg, i) => (
              <MessagePreview key={i} msg={msg} />
            ))}
          </div>
        </div>
      )}
    </Card>
  );
}

function MessagePreview({ msg }: { msg: MessageCreatedData }) {
  return (
    <div
      className={cn(
        "rounded-md p-3 text-sm",
        msg.role === "user" && "bg-altair-blue/10 border-l-2 border-altair-blue",
        msg.role === "assistant" && "bg-deploy-green/10 border-l-2 border-deploy-green",
        msg.role === "system" && "bg-cosmic-violet/10 border-l-2 border-cosmic-violet",
        msg.role === "tool" && "bg-stellar-gold/10 border-l-2 border-stellar-gold"
      )}
    >
      <span className="text-xs font-medium uppercase text-slate-muted">{msg.role}</span>
      <p className="mt-1 text-cloud-white whitespace-pre-wrap">
        {msg.content?.slice(0, 500)}
        {msg.content?.length > 500 ? "..." : ""}
      </p>
    </div>
  );
}

function StatusDot({ status }: { status: string }) {
  return (
    <span
      className={cn(
        "h-2.5 w-2.5 rounded-full",
        status === "running" && "bg-deploy-green animate-pulse",
        status === "completed" && "bg-deploy-green",
        status === "failed" && "bg-error-red"
      )}
    />
  );
}
