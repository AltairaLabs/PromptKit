import { Card } from "@/components/ui/card";
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
    const existing = groups.get(run.scenario) || [];
    existing.push(run);
    groups.set(run.scenario, existing);
  }

  return (
    <div className="space-y-6">
      <h3 className="text-sm font-medium text-slate-muted uppercase tracking-wider">
        Results Matrix
      </h3>
      {Array.from(groups.entries()).map(([scenario, scenarioRuns]) => (
        <ScenarioGroup
          key={scenario}
          scenario={scenario}
          runs={scenarioRuns}
          onSelectRun={onSelectRun}
        />
      ))}
    </div>
  );
}

function ScenarioGroup({ scenario, runs, onSelectRun }: {
  scenario: string;
  runs: ActiveRun[];
  onSelectRun?: (runId: string) => void;
}) {
  // Build matrix dimensions
  const providers = [...new Set(runs.map((r) => r.provider))].sort();
  const regions = [...new Set(runs.map((r) => r.region))].sort();

  // Index runs by provider+region
  const index = new Map<string, ActiveRun>();
  for (const run of runs) {
    index.set(`${run.provider}:${run.region}`, run);
  }

  return (
    <Card className="bg-onyx border-white/10 overflow-hidden">
      <div className="px-4 py-3 bg-gradient-to-r from-cosmic-violet/20 to-altair-blue/20 border-b border-white/10">
        <span className="text-sm font-semibold text-cloud-white">{scenario}</span>
        <span className="text-xs text-slate-muted ml-2">
          {runs.length} run{runs.length !== 1 ? "s" : ""}
        </span>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full">
          <thead>
            <tr>
              <th className="px-3 py-2 text-left text-xs font-medium text-slate-muted uppercase tracking-wider bg-onyx">
                Region
              </th>
              {providers.map((p) => (
                <th key={p} className="px-3 py-2 text-center text-xs font-medium text-slate-muted uppercase tracking-wider bg-onyx">
                  {p}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {regions.map((region) => (
              <tr key={region} className="border-t border-white/5">
                <td className="px-3 py-2 text-xs text-slate-muted">{region}</td>
                {providers.map((provider) => {
                  const run = index.get(`${provider}:${region}`);
                  return (
                    <td key={provider} className="px-3 py-2 text-center">
                      {run ? (
                        <button
                          onClick={() => onSelectRun?.(run.runId)}
                          className={cn(
                            "inline-flex flex-col items-center gap-0.5 rounded-md px-3 py-2 min-w-[80px] transition-colors",
                            run.status === "completed"
                              ? "bg-deploy-green/10 hover:bg-deploy-green/20 text-deploy-green"
                              : "bg-error-red/10 hover:bg-error-red/20 text-error-red"
                          )}
                        >
                          <span className="text-xs font-semibold">
                            {run.status === "completed" ? "✓ Pass" : "✗ Fail"}
                          </span>
                          <span className="text-[10px] font-mono opacity-70">
                            ${run.costs.totalCost.toFixed(4)}
                          </span>
                        </button>
                      ) : (
                        <span className="text-xs text-slate-muted/50">—</span>
                      )}
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Card>
  );
}
