import type { TrialResults } from "@/types";

interface TrialResultsPanelProps {
  trial?: TrialResults;
}

function pct(v: number): string {
  return `${(v * 100).toFixed(0)}%`;
}

function flakinessTone(score: number): string {
  if (score < 0.1) return "text-[#10B981]";
  if (score < 0.3) return "text-[#F59E0B]";
  return "text-[#EF4444]";
}

// TrialResultsPanel surfaces aggregated stats from a Trials > 1 scenario:
// pass rate, flakiness, and per-assertion stability. Mirrors the HTML
// report's trial summary section.
export function TrialResultsPanel({ trial }: TrialResultsPanelProps) {
  if (!trial || trial.trial_count <= 1) return null;
  const perAssertion = trial.per_assertion_stats ?? {};
  const entries = Object.entries(perAssertion);
  return (
    <div className="space-y-2">
      <h3 className="text-xs font-semibold text-fg-muted uppercase tracking-wider flex items-center gap-2">
        Trial Aggregation
        <span className="rounded-full bg-[var(--c-surface-2)] text-fg-muted px-2 py-0.5 text-[10px] font-mono">
          {trial.trial_count} trials
        </span>
      </h3>
      <div className="rounded-lg bg-surface border border-mist p-4 space-y-3">
        <div className="grid grid-cols-3 gap-3">
          <Stat label="Pass Rate" value={pct(trial.pass_rate)} tone={trial.pass_rate >= 0.9 ? "text-[#10B981]" : trial.pass_rate >= 0.5 ? "text-[#F59E0B]" : "text-[#EF4444]"} />
          <Stat label="Flakiness" value={trial.flakiness_score.toFixed(2)} tone={flakinessTone(trial.flakiness_score)} />
          <Stat label="Trials" value={String(trial.trial_count)} />
        </div>
        {entries.length > 0 && (
          <div className="border-t border-mist pt-3">
            <div className="text-[10px] uppercase tracking-wider text-fg-muted mb-2">Per-assertion stability</div>
            <table className="w-full text-[11px]">
              <thead>
                <tr className="border-b border-mist text-fg-muted">
                  <th className="text-left py-1 font-medium">Assertion</th>
                  <th className="text-right py-1 font-medium">Pass</th>
                  <th className="text-right py-1 font-medium">Fail</th>
                  <th className="text-right py-1 font-medium">Pass %</th>
                  <th className="text-right py-1 font-medium">Flakiness</th>
                </tr>
              </thead>
              <tbody>
                {entries.map(([id, s]) => (
                  <tr key={id} className="border-b border-mist/60 last:border-b-0">
                    <td className="py-1 font-mono text-fg truncate max-w-[200px]" title={id}>{id}</td>
                    <td className="py-1 text-right font-mono text-[#10B981]">{s.pass_count}</td>
                    <td className="py-1 text-right font-mono text-[#EF4444]">{s.fail_count}</td>
                    <td className="py-1 text-right font-mono text-fg">{pct(s.pass_rate)}</td>
                    <td className={`py-1 text-right font-mono ${flakinessTone(s.flakiness_score)}`}>
                      {s.flakiness_score.toFixed(2)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}

function Stat({ label, value, tone }: { label: string; value: string; tone?: string }) {
  return (
    <div>
      <div className="text-[10px] uppercase tracking-wider text-fg-muted mb-0.5">{label}</div>
      <div className={`text-xl font-bold font-mono ${tone ?? "text-fg"}`}>{value}</div>
    </div>
  );
}
