import { useMemo, useState } from "react";
import { Search, X } from "lucide-react";
import type { RunResult } from "@/types";

interface HistoricalResultsProps {
  results: RunResult[];
  onSelectRun: (id: string) => void;
  onClear: () => void;
}

function timeAgo(dateStr: string): string {
  const seconds = Math.floor((Date.now() - new Date(dateStr).getTime()) / 1000);
  if (seconds < 60) return "just now";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  if (days < 30) return `${days}d ago`;
  return `${Math.floor(days / 30)}mo ago`;
}

// failureBlurb summarises why a run failed in a one-liner suitable for a
// table cell: prefer assertion counts when available, fall back to the
// first chunk of the error message.
function failureBlurb(r: RunResult): string {
  if (r.ConversationAssertions && r.ConversationAssertions.failed > 0) {
    const f = r.ConversationAssertions.failed;
    const total = r.ConversationAssertions.total;
    return `${f}/${total} assertion${total === 1 ? "" : "s"} failed`;
  }
  if (r.Error) {
    return r.Error.length > 80 ? r.Error.slice(0, 80) + "…" : r.Error;
  }
  return "Failed";
}

export function HistoricalResults({ results, onSelectRun, onClear }: HistoricalResultsProps) {
  const [filter, setFilter] = useState("");

  const filtered = useMemo(() => {
    const sorted = [...results].sort((a, b) =>
      new Date(b.EndTime || b.StartTime).getTime() - new Date(a.EndTime || a.StartTime).getTime()
    );
    if (!filter.trim()) return sorted;
    const q = filter.toLowerCase();
    return sorted.filter((r) =>
      r.ScenarioID.toLowerCase().includes(q) ||
      r.ProviderID.toLowerCase().includes(q) ||
      (r.Region ?? "").toLowerCase().includes(q) ||
      (r.Error ?? "").toLowerCase().includes(q),
    );
  }, [results, filter]);

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-3">
        <h3 className="text-xs font-semibold text-fg-muted uppercase tracking-wider whitespace-nowrap">
          Previous Runs
          <span className="ml-2 rounded-full bg-[var(--c-surface-2)] text-fg-muted px-2 py-0.5 text-[10px] font-mono normal-case tracking-normal">
            {filtered.length}{filter ? ` / ${results.length}` : ""}
          </span>
        </h3>
        <div className="flex items-center gap-2 flex-1 max-w-sm">
          <div className="relative flex-1">
            <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-fg-muted" />
            <input
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="Filter scenario, provider, region…"
              className="w-full rounded-lg border border-mist bg-surfacepl-7 pr-7 py-1.5 text-xs text-fg placeholder:text-fg-muted focus:outline-none focus:ring-1 focus:ring-[#2563EB]/40 focus:border-[#2563EB]"
            />
            {filter && (
              <button
                onClick={() => setFilter("")}
                className="absolute right-1 top-1/2 -translate-y-1/2 p-0.5 text-fg-muted hover:text-fg"
                aria-label="Clear filter"
              >
                <X className="h-3 w-3" />
              </button>
            )}
          </div>
        </div>
        <button
          onClick={onClear}
          className="rounded-lg border border-red-200 bg-red-50 px-3 py-1.5 text-xs font-medium text-[#EF4444] hover:bg-red-100 transition-colors whitespace-nowrap"
        >
          Clear all
        </button>
      </div>

      {filtered.length === 0 ? (
        <div className="rounded-xl border border-mist bg-surfacepx-6 py-8 text-center text-xs text-fg-muted">
          No runs match "{filter}".
        </div>
      ) : (
        <div className="rounded-xl border border-mist bg-surfaceshadow-sm overflow-hidden">
          <table className="w-full text-[13px]">
            <thead>
              <tr className="border-b border-mist bg-[var(--c-surface-2)]">
                <th className="px-4 py-2.5 text-left text-[11px] font-semibold text-fg-muted uppercase tracking-wider">Scenario</th>
                <th className="px-4 py-2.5 text-left text-[11px] font-semibold text-fg-muted uppercase tracking-wider">Provider</th>
                <th className="px-4 py-2.5 text-left text-[11px] font-semibold text-fg-muted uppercase tracking-wider">Region</th>
                <th className="px-4 py-2.5 text-left text-[11px] font-semibold text-fg-muted uppercase tracking-wider">Result</th>
                <th className="px-4 py-2.5 text-right text-[11px] font-semibold text-fg-muted uppercase tracking-wider">Cost</th>
                <th className="px-4 py-2.5 text-right text-[11px] font-semibold text-fg-muted uppercase tracking-wider">Msgs</th>
                <th className="px-4 py-2.5 text-right text-[11px] font-semibold text-fg-muted uppercase tracking-wider">When</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((r) => {
                const passed = !r.Error && (r.ConversationAssertions?.passed ?? true);
                return (
                  <tr
                    key={r.RunID}
                    className="border-t border-mist/60 hover:bg-[var(--c-surface-2)] cursor-pointer transition-colors"
                    onClick={() => onSelectRun(r.RunID)}
                  >
                    <td className="px-4 py-2.5 font-medium text-fg">{r.ScenarioID}</td>
                    <td className="px-4 py-2.5 text-fg-muted">{r.ProviderID}</td>
                    <td className="px-4 py-2.5 text-fg-muted">{r.Region}</td>
                    <td className="px-4 py-2.5">
                      <div className="flex flex-col gap-0.5">
                        <span className={`inline-flex items-center gap-1.5 text-[12px] font-semibold ${passed ? "text-[#10B981]" : "text-[#EF4444]"}`}>
                          <span className={`h-1.5 w-1.5 rounded-full ${passed ? "bg-[#10B981]" : "bg-[#EF4444]"}`} />
                          {passed ? "Pass" : "Fail"}
                        </span>
                        {!passed && (
                          <span className="text-[10px] text-fg-muted truncate max-w-[260px]" title={r.Error}>
                            {failureBlurb(r)}
                          </span>
                        )}
                      </div>
                    </td>
                    <td className="px-4 py-2.5 text-right font-mono text-fg-muted">
                      ${r.Cost?.total_cost_usd?.toFixed(4) ?? "0.0000"}
                    </td>
                    <td className="px-4 py-2.5 text-right text-fg-muted">
                      {r.Messages?.length ?? 0}
                    </td>
                    <td className="px-4 py-2.5 text-right text-fg-muted">
                      {r.EndTime ? timeAgo(r.EndTime) : r.StartTime ? timeAgo(r.StartTime) : "—"}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
