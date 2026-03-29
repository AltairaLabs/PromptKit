import { useState, Component } from "react";
import type { ReactNode, ErrorInfo } from "react";
import { Layout } from "@/components/Layout";
import { SummaryCards } from "@/components/SummaryCards";
import { RunProgress } from "@/components/RunProgress";
import { ScenarioMatrix } from "@/components/ScenarioMatrix";
import { RunDetail } from "@/components/RunDetail";
import { DevToolsPanel } from "@/components/DevToolsPanel";
import { useArenaEvents } from "@/hooks/useArenaEvents";
import { useArenaAPI } from "@/hooks/useArenaAPI";
import type { Message } from "@/types";

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
  const { startRun, loading } = useArenaAPI();
  const [selectedRunId, setSelectedRunId] = useState<string | null>(null);
  const [devToolsMessage] = useState<Message | undefined>();
  const [devToolsIndex, setDevToolsIndex] = useState<number | undefined>();
  const [devToolsOpen, setDevToolsOpen] = useState(false);
  const [startError, setStartError] = useState<string | null>(null);

  const runs = Object.values(state.runs);
  const selectedRun = selectedRunId ? state.runs[selectedRunId] : undefined;

  const handleSelectMessage = (index: number) => {
    setDevToolsIndex(index);
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
            <RunDetail runId={selectedRunId} onBack={() => setSelectedRunId(null)} onSelectMessage={handleSelectMessage} />
          ) : (
            <div className="space-y-8">
              {startError && (
                <div className="rounded-xl bg-red-50 border border-red-200 px-4 py-3 text-sm text-[#EF4444]">{startError}</div>
              )}
              <SummaryCards
                totalRuns={runs.length}
                activeRuns={runs.filter((r) => r.status === "running").length}
                completedRuns={runs.filter((r) => r.status !== "running").length}
                failedRuns={runs.filter((r) => r.status === "failed").length}
                totalCost={state.totalCost}
                totalTokens={state.totalTokens}
              />
              <RunProgress runs={runs} onSelectRun={setSelectedRunId} />
              <ScenarioMatrix runs={runs} onSelectRun={setSelectedRunId} />
            </div>
          )}
        </div>
        <DevToolsPanel message={devToolsMessage} messageIndex={devToolsIndex} run={selectedRun} open={devToolsOpen} onClose={() => setDevToolsOpen(false)} />
      </Layout>
    </ErrorBoundary>
  );
}
