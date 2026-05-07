import { useEffect, useMemo, useState } from "react";
import { Search, X, ArrowUpDown, ArrowUp, ArrowDown, ChevronLeft, ChevronRight } from "lucide-react";
import type { RunResult } from "@/types";

interface HistoricalResultsProps {
  results: RunResult[];
  onSelectRun: (id: string) => void;
  onClear: () => void;
}

type SortKey = "scenario" | "provider" | "region" | "result" | "cost" | "msgs" | "when";
type SortDir = "asc" | "desc";

const PAGE_SIZE = 25;

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

// runTime returns the canonical timestamp for sorting/display.
function runTime(r: RunResult): number {
  return new Date(r.EndTime || r.StartTime).getTime();
}

// runPassed treats Error + failed assertions as Fail; everything else
// (including missing assertion summary) is Pass.
function runPassed(r: RunResult): boolean {
  return !r.Error && (r.ConversationAssertions?.passed ?? true);
}

// compare returns the canonical sort comparison for a given column. The
// caller flips the sign for descending order. Strings are compared
// case-insensitively so "openai-gpt-4" and "Openai-Gpt-4" group.
function compare(a: RunResult, b: RunResult, key: SortKey): number {
  switch (key) {
    case "scenario": return a.ScenarioID.localeCompare(b.ScenarioID, undefined, { sensitivity: "base" });
    case "provider": return a.ProviderID.localeCompare(b.ProviderID, undefined, { sensitivity: "base" });
    case "region":   return (a.Region ?? "").localeCompare(b.Region ?? "", undefined, { sensitivity: "base" });
    case "result":   return Number(runPassed(b)) - Number(runPassed(a)); // pass first when asc
    case "cost":     return (a.Cost?.total_cost_usd ?? 0) - (b.Cost?.total_cost_usd ?? 0);
    case "msgs":     return (a.Messages?.length ?? 0) - (b.Messages?.length ?? 0);
    case "when":     return runTime(a) - runTime(b);
  }
}

export function HistoricalResults({ results, onSelectRun, onClear }: HistoricalResultsProps) {
  const [filter, setFilter] = useState("");
  const [sortKey, setSortKey] = useState<SortKey>("when");
  const [sortDir, setSortDir] = useState<SortDir>("desc");
  const [page, setPage] = useState(0);

  const filtered = useMemo(() => {
    const dir = sortDir === "asc" ? 1 : -1;
    const sorted = [...results].sort((a, b) => dir * compare(a, b, sortKey));
    if (!filter.trim()) return sorted;
    const q = filter.toLowerCase();
    return sorted.filter((r) =>
      r.ScenarioID.toLowerCase().includes(q) ||
      r.ProviderID.toLowerCase().includes(q) ||
      (r.Region ?? "").toLowerCase().includes(q) ||
      (r.Error ?? "").toLowerCase().includes(q),
    );
  }, [results, filter, sortKey, sortDir]);

  const totalPages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
  // Clamp the page index when the filtered set shrinks underneath us
  // (e.g. user types into the filter on page 5 of an unfiltered set).
  useEffect(() => {
    if (page > totalPages - 1) setPage(0);
  }, [page, totalPages]);
  const pageStart = page * PAGE_SIZE;
  const visible = filtered.slice(pageStart, pageStart + PAGE_SIZE);

  const setSort = (key: SortKey) => {
    if (sortKey === key) {
      setSortDir((d) => (d === "asc" ? "desc" : "asc"));
    } else {
      setSortKey(key);
      // Time defaults to newest-first; everything else defaults to A→Z / low→high.
      setSortDir(key === "when" || key === "cost" ? "desc" : "asc");
    }
    setPage(0);
  };

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
              onChange={(e) => { setFilter(e.target.value); setPage(0); }}
              placeholder="Filter scenario, provider, region…"
              className="w-full rounded-lg border border-mist bg-surface pl-7 pr-7 py-1.5 text-xs text-fg placeholder:text-fg-muted focus:outline-none focus:ring-1 focus:ring-[#2563EB]/40 focus:border-[#2563EB]"
            />
            {filter && (
              <button
                onClick={() => { setFilter(""); setPage(0); }}
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
        <div className="rounded-xl border border-mist bg-surface px-6 py-8 text-center text-xs text-fg-muted">
          {filter ? <>No runs match "{filter}".</> : <>No runs yet.</>}
        </div>
      ) : (
        <>
          <div className="rounded-xl border border-mist bg-surface shadow-sm overflow-hidden">
            <table className="w-full text-[13px]">
              <thead>
                <tr className="border-b border-mist bg-[var(--c-surface-2)]">
                  <SortHeader label="Scenario" sortKey="scenario" current={sortKey} dir={sortDir} onSort={setSort} />
                  <SortHeader label="Provider" sortKey="provider" current={sortKey} dir={sortDir} onSort={setSort} />
                  <SortHeader label="Region"   sortKey="region"   current={sortKey} dir={sortDir} onSort={setSort} />
                  <SortHeader label="Result"   sortKey="result"   current={sortKey} dir={sortDir} onSort={setSort} />
                  <SortHeader label="Cost"     sortKey="cost"     current={sortKey} dir={sortDir} onSort={setSort} align="right" />
                  <SortHeader label="Msgs"     sortKey="msgs"     current={sortKey} dir={sortDir} onSort={setSort} align="right" />
                  <SortHeader label="When"     sortKey="when"     current={sortKey} dir={sortDir} onSort={setSort} align="right" />
                </tr>
              </thead>
              <tbody>
                {visible.map((r) => {
                  const passed = runPassed(r);
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

          {totalPages > 1 && (
            <Pagination
              page={page}
              totalPages={totalPages}
              pageStart={pageStart}
              pageEnd={pageStart + visible.length}
              total={filtered.length}
              onPrev={() => setPage((p) => Math.max(0, p - 1))}
              onNext={() => setPage((p) => Math.min(totalPages - 1, p + 1))}
            />
          )}
        </>
      )}
    </div>
  );
}

function SortHeader({
  label,
  sortKey,
  current,
  dir,
  onSort,
  align = "left",
}: {
  label: string;
  sortKey: SortKey;
  current: SortKey;
  dir: SortDir;
  onSort: (k: SortKey) => void;
  align?: "left" | "right";
}) {
  const active = current === sortKey;
  const Icon = !active ? ArrowUpDown : dir === "asc" ? ArrowUp : ArrowDown;
  return (
    <th className={`px-4 py-2.5 text-${align} text-[11px] font-semibold uppercase tracking-wider`}>
      <button
        type="button"
        onClick={() => onSort(sortKey)}
        className={`inline-flex items-center gap-1 ${align === "right" ? "ml-auto" : ""} ${active ? "text-fg" : "text-fg-muted hover:text-fg"} transition-colors`}
        aria-sort={active ? (dir === "asc" ? "ascending" : "descending") : "none"}
      >
        {label}
        <Icon className={`h-3 w-3 ${active ? "opacity-100" : "opacity-50"}`} />
      </button>
    </th>
  );
}

function Pagination({
  page,
  totalPages,
  pageStart,
  pageEnd,
  total,
  onPrev,
  onNext,
}: {
  page: number;
  totalPages: number;
  pageStart: number;
  pageEnd: number;
  total: number;
  onPrev: () => void;
  onNext: () => void;
}) {
  return (
    <div className="flex items-center justify-between text-xs text-fg-muted">
      <div className="font-mono">
        {pageStart + 1}–{pageEnd} of {total}
      </div>
      <div className="flex items-center gap-2">
        <button
          type="button"
          onClick={onPrev}
          disabled={page === 0}
          className="inline-flex items-center gap-1 rounded-md border border-mist bg-surface px-2 py-1 hover:bg-[var(--c-surface-2)] disabled:opacity-40 disabled:cursor-not-allowed"
        >
          <ChevronLeft className="h-3 w-3" /> Prev
        </button>
        <span className="font-mono">
          {page + 1} / {totalPages}
        </span>
        <button
          type="button"
          onClick={onNext}
          disabled={page >= totalPages - 1}
          className="inline-flex items-center gap-1 rounded-md border border-mist bg-surface px-2 py-1 hover:bg-[var(--c-surface-2)] disabled:opacity-40 disabled:cursor-not-allowed"
        >
          Next <ChevronRight className="h-3 w-3" />
        </button>
      </div>
    </div>
  );
}
