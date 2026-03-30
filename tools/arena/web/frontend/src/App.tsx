import { useState, useEffect, Component } from "react";
import type { ReactNode, ErrorInfo } from "react";
import { Layout } from "@/components/Layout";
import { SummaryCards } from "@/components/SummaryCards";
import { RunProgress } from "@/components/RunProgress";
import { RunDetail } from "@/components/RunDetail";
import { DevToolsPanel } from "@/components/DevToolsPanel";
import { useArenaEvents } from "@/hooks/useArenaEvents";
import { useArenaAPI } from "@/hooks/useArenaAPI";
import type { Message, RunResult } from "@/types";

class ErrorBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  constructor(props: { children: ReactNode }) {
    super(props);
    this.state = { error: null };
  }
  static getDerivedStateFromError(error: Error) { return { error }; }
  componentDidCatch(error: Error, info: ErrorInfo) { console.error("Arena UI error:", error, info); }
  render() {
    if (this.state.error) {
      return (
        <div className="min-h-screen bg-cloud-white flex items-center justify-center p-8">
          <div className="rounded-xl border border-red-200 bg-white p-8 max-w-lg w-full text-center shadow-sm">
            <h2 className="text-lg font-semibold text-[#EF4444] mb-2">Something went wrong</h2>
            <p className="text-sm text-slate-muted mb-6">{this.state.error.message}</p>
            <button
              className="rounded-lg bg-blue-50 border border-blue-200 px-4 py-2 text-sm font-medium text-[#2563EB] hover:bg-blue-100 transition-colors"
              onClick={() => this.setState({ error: null })}
            >
              Try again
            </button>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}

export default function App() {
  const state = useArenaEvents();
  const { startRun, getResults, getResult, clearResults, loading } = useArenaAPI();
  const [selectedRunId, setSelectedRunId] = useState<string | null>(null);
  const [devToolsMessage, setDevToolsMessage] = useState<Message | undefined>();
  const [devToolsAllMessages, setDevToolsAllMessages] = useState<Message[] | undefined>();
  const [devToolsIndex, setDevToolsIndex] = useState<number | undefined>();
  const [devToolsOpen, setDevToolsOpen] = useState(false);
  const [startError, setStartError] = useState<string | null>(null);
  const [historicalResults, setHistoricalResults] = useState<RunResult[]>([]);

  // Load results from the server on mount and when runs complete
  const completedCount = state.completedRunIds.length;
  useEffect(() => {
    getResults().then(async (ids) => {
      if (!ids || ids.length === 0) { setHistoricalResults([]); return; }
      const results = await Promise.all(ids.map((id) => getResult(id).catch(() => null)));
      setHistoricalResults(results.filter((r): r is RunResult => r !== null));
    }).catch(() => {});
  }, [getResults, getResult, completedCount]);

  const liveRuns = Object.values(state.runs);
  const selectedRun = selectedRunId ? state.runs[selectedRunId] : undefined;

  const handleSelectMessage = (index: number, message?: Message, allMsgs?: Message[]) => {
    setDevToolsIndex(index);
    setDevToolsMessage(message);
    setDevToolsAllMessages(allMsgs);
    setDevToolsOpen(true);
  };

  const handleStartRun = async () => {
    setStartError(null);
    try { await startRun(); } catch (err) {
      setStartError(err instanceof Error ? err.message : "Failed to start run");
    }
  };

  return (
    <ErrorBoundary>
      <Layout connected={state.connected} onStartRun={handleStartRun} loading={loading}>
        <div className={devToolsOpen ? "mr-[420px] transition-[margin] duration-200" : "transition-[margin] duration-200"}>
          {selectedRunId ? (
            <RunDetail runId={selectedRunId} onBack={() => { setSelectedRunId(null); setDevToolsOpen(false); }} onSelectMessage={handleSelectMessage} />
          ) : (
            <div className="space-y-8">
              {startError && (
                <div className="rounded-xl bg-red-50 border border-red-200 px-4 py-3 text-sm text-[#EF4444]">{startError}</div>
              )}
              <SummaryCards
                totalRuns={liveRuns.length + historicalResults.length}
                activeRuns={liveRuns.filter((r) => r.status === "running").length}
                completedRuns={liveRuns.filter((r) => r.status !== "running").length + historicalResults.length}
                failedRuns={liveRuns.filter((r) => r.status === "failed").length + historicalResults.filter((r) => !!r.Error).length}
                totalCost={state.totalCost + historicalResults.reduce((sum, r) => sum + (r.Cost?.total_cost_usd || 0), 0)}
                totalTokens={state.totalTokens + historicalResults.reduce((sum, r) => sum + (r.Cost?.input_tokens || 0) + (r.Cost?.output_tokens || 0), 0)}
              />
              <RunProgress runs={liveRuns} onSelectRun={setSelectedRunId} />
              {historicalResults.length > 0 && (
                <HistoricalResults results={historicalResults} onSelectRun={setSelectedRunId} onClear={async () => {
                  await clearResults();
                  setHistoricalResults([]);
                }} />
              )}
            </div>
          )}
        </div>
        <DevToolsPanel message={devToolsMessage} messageIndex={devToolsIndex} allMessages={devToolsAllMessages} run={selectedRun} open={devToolsOpen} onClose={() => setDevToolsOpen(false)} />
      </Layout>
    </ErrorBoundary>
  );
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

function HistoricalResults({ results, onSelectRun, onClear }: { results: RunResult[]; onSelectRun: (id: string) => void; onClear: () => void }) {
  const sorted = [...results].sort((a, b) =>
    new Date(b.EndTime || b.StartTime).getTime() - new Date(a.EndTime || a.StartTime).getTime()
  );

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <h3 className="text-xs font-semibold text-slate-muted uppercase tracking-wider">Previous Runs</h3>
        <button
          onClick={onClear}
          className="rounded-lg border border-red-200 bg-red-50 px-3 py-1.5 text-xs font-medium text-[#EF4444] hover:bg-red-100 transition-colors"
        >
          Clear all
        </button>
      </div>
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
              <th className="px-4 py-2.5 text-right text-[11px] font-semibold text-slate-muted uppercase tracking-wider">When</th>
            </tr>
          </thead>
          <tbody>
            {sorted.map((r) => {
              const passed = !r.Error;
              return (
                <tr
                  key={r.RunID}
                  className="border-t border-mist/60 hover:bg-[#F8FAFC] cursor-pointer transition-colors"
                  onClick={() => onSelectRun(r.RunID)}
                >
                  <td className="px-4 py-2.5 font-medium text-deep-space">{r.ScenarioID}</td>
                  <td className="px-4 py-2.5 text-slate-muted">{r.ProviderID}</td>
                  <td className="px-4 py-2.5 text-slate-muted">{r.Region}</td>
                  <td className="px-4 py-2.5 text-center">
                    <span className={`inline-flex items-center gap-1.5 text-[12px] font-semibold ${passed ? "text-[#10B981]" : "text-[#EF4444]"}`}>
                      <span className={`h-1.5 w-1.5 rounded-full ${passed ? "bg-[#10B981]" : "bg-[#EF4444]"}`} />
                      {passed ? "Pass" : "Fail"}
                    </span>
                  </td>
                  <td className="px-4 py-2.5 text-right font-mono text-slate-muted">
                    ${r.Cost?.total_cost_usd?.toFixed(4) ?? "0.0000"}
                  </td>
                  <td className="px-4 py-2.5 text-right text-slate-muted">
                    {r.Messages?.length ?? 0}
                  </td>
                  <td className="px-4 py-2.5 text-right text-slate-muted">
                    {r.EndTime ? timeAgo(r.EndTime) : r.StartTime ? timeAgo(r.StartTime) : "—"}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}
