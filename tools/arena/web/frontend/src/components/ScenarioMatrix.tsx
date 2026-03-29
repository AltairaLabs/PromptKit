import { cn } from "@/lib/utils";
import type { ActiveRun } from "@/types";

interface ScenarioMatrixProps {
  runs: ActiveRun[];
  onSelectRun?: (runId: string) => void;
}

export function ScenarioMatrix({ runs, onSelectRun }: ScenarioMatrixProps) {
  const completed = runs.filter((r) => r.status !== "running");
  if (completed.length === 0) return null;

  const groups = new Map<string, ActiveRun[]>();
  for (const run of completed) {
    const arr = groups.get(run.scenario) || [];
    arr.push(run);
    groups.set(run.scenario, arr);
  }

  return (
    <div className="space-y-4">
      <h3 className="text-xs font-semibold text-slate-muted uppercase tracking-wider">Results Matrix</h3>
      {Array.from(groups.entries()).map(([scenario, scenarioRuns]) => (
        <ScenarioGroup key={scenario} scenario={scenario} runs={scenarioRuns} onSelectRun={onSelectRun} />
      ))}
    </div>
  );
}

function ScenarioGroup({ scenario, runs, onSelectRun }: {
  scenario: string;
  runs: ActiveRun[];
  onSelectRun?: (runId: string) => void;
}) {
  const providers = [...new Set(runs.map((r) => r.provider))].sort();
  const regions = [...new Set(runs.map((r) => r.region))].sort();
  const index = new Map<string, ActiveRun>();
  for (const run of runs) index.set(`${run.provider}:${run.region}`, run);

  return (
    <div className="rounded-xl border border-mist bg-white shadow-sm overflow-hidden">
      <div className="px-4 py-2.5 border-b border-mist bg-[#F8FAFC]">
        <span className="text-sm font-semibold text-deep-space">{scenario}</span>
        <span className="text-xs text-slate-muted ml-2">{runs.length} run{runs.length !== 1 ? "s" : ""}</span>
      </div>
      <table className="w-full text-xs">
        <thead>
          <tr className="border-b border-mist">
            <th className="px-3 py-2 text-left font-medium text-slate-muted uppercase tracking-wider" />
            {providers.map((p) => (
              <th key={p} className="px-3 py-2 text-center font-medium text-slate-muted">{p}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {regions.map((region) => (
            <tr key={region} className="border-t border-mist/50 hover:bg-[#F8FAFC]">
              <td className="px-3 py-2 text-slate-muted">{region}</td>
              {providers.map((provider) => {
                const run = index.get(`${provider}:${region}`);
                if (!run) return <td key={provider} className="px-3 py-2 text-center text-slate-muted/30">—</td>;
                const passed = run.status === "completed";
                return (
                  <td key={provider} className="px-3 py-2 text-center">
                    <button
                      onClick={() => onSelectRun?.(run.runId)}
                      className={cn(
                        "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium transition-colors",
                        passed ? "text-[#10B981] hover:bg-emerald-50" : "text-[#EF4444] hover:bg-red-50"
                      )}
                    >
                      <span className={cn("h-1.5 w-1.5 rounded-full", passed ? "bg-[#10B981]" : "bg-[#EF4444]")} />
                      {passed ? "Pass" : "Fail"}
                    </button>
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
