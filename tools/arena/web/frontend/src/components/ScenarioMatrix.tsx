import { cn } from "@/lib/utils";
import type { ActiveRun } from "@/types";

interface ScenarioMatrixProps {
  runs: ActiveRun[];
  onSelectRun?: (runId: string) => void;
}

export function ScenarioMatrix({ runs, onSelectRun }: ScenarioMatrixProps) {
  const completed = runs.filter((r) => r.status !== "running");
  if (completed.length === 0) return null;

  // Group by scenario
  const groups = new Map<string, ActiveRun[]>();
  for (const run of completed) {
    const arr = groups.get(run.scenario) || [];
    arr.push(run);
    groups.set(run.scenario, arr);
  }

  return (
    <div className="space-y-3">
      <h3 className="text-xs font-semibold text-slate-muted uppercase tracking-wider">Results</h3>
      <div className="rounded-xl border border-mist bg-white shadow-sm overflow-hidden">
        <table className="w-full text-[13px]">
          <thead>
            <tr className="border-b border-mist bg-[#F8FAFC]">
              <th className="px-4 py-2.5 text-left text-[11px] font-semibold text-slate-muted uppercase tracking-wider">Scenario</th>
              <th className="px-4 py-2.5 text-left text-[11px] font-semibold text-slate-muted uppercase tracking-wider">Provider</th>
              <th className="px-4 py-2.5 text-left text-[11px] font-semibold text-slate-muted uppercase tracking-wider">Region</th>
              <th className="px-4 py-2.5 text-center text-[11px] font-semibold text-slate-muted uppercase tracking-wider">Result</th>
              <th className="px-4 py-2.5 text-right text-[11px] font-semibold text-slate-muted uppercase tracking-wider">Cost</th>
              <th className="px-4 py-2.5 text-right text-[11px] font-semibold text-slate-muted uppercase tracking-wider">Msgs</th>
            </tr>
          </thead>
          <tbody>
            {Array.from(groups.entries()).map(([scenario, scenarioRuns]) =>
              scenarioRuns.map((run, i) => {
                const passed = run.status === "completed" && !run.error;
                return (
                  <tr
                    key={run.runId}
                    className="border-t border-mist/60 hover:bg-[#F8FAFC] cursor-pointer transition-colors"
                    onClick={() => onSelectRun?.(run.runId)}
                  >
                    {i === 0 ? (
                      <td className="px-4 py-2.5 font-medium text-deep-space" rowSpan={scenarioRuns.length}>
                        {scenario}
                      </td>
                    ) : null}
                    <td className="px-4 py-2.5 text-slate-muted">{run.provider}</td>
                    <td className="px-4 py-2.5 text-slate-muted">{run.region}</td>
                    <td className="px-4 py-2.5 text-center">
                      <span className={cn(
                        "inline-flex items-center gap-1.5 text-[12px] font-semibold",
                        passed ? "text-[#10B981]" : "text-[#EF4444]"
                      )}>
                        <span className={cn("h-1.5 w-1.5 rounded-full", passed ? "bg-[#10B981]" : "bg-[#EF4444]")} />
                        {passed ? "Pass" : "Fail"}
                      </span>
                    </td>
                    <td className="px-4 py-2.5 text-right font-mono text-slate-muted">
                      ${run.costs.totalCost.toFixed(4)}
                    </td>
                    <td className="px-4 py-2.5 text-right text-slate-muted">
                      {run.messages.length}
                    </td>
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
