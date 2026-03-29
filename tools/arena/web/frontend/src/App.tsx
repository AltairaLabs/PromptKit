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
  static getDerivedStateFromError(error: Error) {
    return { error };
  }
  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("Arena UI error:", error, info);
  }
  render() {
    if (this.state.error) {
      return (
        <div className="min-h-screen bg-deep-space flex items-center justify-center p-8">
          <div className="bg-onyx border border-error-red/30 rounded-xl p-8 max-w-lg w-full">
            <h2 className="text-lg font-semibold text-error-red mb-2">Something went wrong</h2>
            <p className="text-sm text-slate-muted mb-4">{this.state.error.message}</p>
            <button
              className="text-sm text-altair-blue hover:underline"
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
  const [devToolsMessage, setDevToolsMessage] = useState<Message | undefined>();
  const [devToolsIndex, setDevToolsIndex] = useState<number | undefined>();
  const [devToolsOpen, setDevToolsOpen] = useState(false);
  const [startError, setStartError] = useState<string | null>(null);

  const runs = Object.values(state.runs);
  const selectedRun = selectedRunId ? state.runs[selectedRunId] : undefined;

  const handleSelectMessage = (index: number) => {
    // For completed run detail view, the message comes from RunResult
    // DevTools will show what it can from the Message type
    setDevToolsIndex(index);
    setDevToolsOpen(true);
  };

  const handleStartRun = async () => {
    setStartError(null);
    try {
      await startRun();
    } catch (err) {
      setStartError(err instanceof Error ? err.message : "Failed to start run");
    }
  };

  // Suppress unused variable warning — devToolsMessage is set for future use
  void devToolsMessage;
  void setDevToolsMessage;

  return (
    <ErrorBoundary>
    <Layout connected={state.connected} onStartRun={handleStartRun} loading={loading}>
      <div className={devToolsOpen ? "mr-[420px] transition-[margin]" : ""}>
        {selectedRunId ? (
          <RunDetail
            runId={selectedRunId}
            onBack={() => setSelectedRunId(null)}
            onSelectMessage={handleSelectMessage}
          />
        ) : (
          <div className="space-y-6">
            {startError && (
              <div className="rounded-lg bg-error-red/10 border border-error-red/30 px-4 py-3 text-sm text-error-red">
                {startError}
              </div>
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

      <DevToolsPanel
        message={devToolsMessage}
        messageIndex={devToolsIndex}
        run={selectedRun}
        open={devToolsOpen}
        onClose={() => setDevToolsOpen(false)}
      />
    </Layout>
    </ErrorBoundary>
  );
}
