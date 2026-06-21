import { useCallback, useEffect, useRef, useState, Component } from "react";
import type { ReactNode, ErrorInfo } from "react";
import { Layout } from "@/components/Layout";
import { SummaryCards } from "@/components/SummaryCards";
import { RunProgress } from "@/components/RunProgress";
import { RunDetail } from "@/components/RunDetail";
import { DevToolsPanel } from "@/components/DevToolsPanel";
import { RunControls } from "@/components/RunControls";
import { EmptyStateLauncher } from "@/components/EmptyStateLauncher";
import { HistoricalResults } from "@/components/HistoricalResults";
import { InteractiveChat } from "@/components/InteractiveChat";
import { useArenaEvents } from "@/hooks/useArenaEvents";
import { useArenaAPI } from "@/hooks/useArenaAPI";
import { AudioPlayer } from "@/audio/player";
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
        <div className="min-h-screen bg-canvas flex items-center justify-center p-8">
          <div className="rounded-xl border border-red-200 bg-surface p-8 max-w-lg w-full text-center shadow-sm">
            <h2 className="text-lg font-semibold text-[#EF4444] mb-2">Something went wrong</h2>
            <p className="text-sm text-fg-muted mb-6">{this.state.error.message}</p>
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
  const { registerInteractiveRun, ...state } = useArenaEvents();
  const [activeTab, setActiveTab] = useState<"runs" | "chat">("runs");
  const { startRun, getResults, getResult, clearResults, loading } = useArenaAPI();
  const [selectedRunId, setSelectedRunId] = useState<string | null>(null);
  const [devToolsMessage, setDevToolsMessage] = useState<Message | undefined>();
  const [devToolsAllMessages, setDevToolsAllMessages] = useState<Message[] | undefined>();
  const [devToolsIndex, setDevToolsIndex] = useState<number | undefined>();
  const [devToolsOpen, setDevToolsOpen] = useState(false);
  const [startError, setStartError] = useState<string | null>(null);
  const [historicalResults, setHistoricalResults] = useState<RunResult[]>([]);

  // Single global AudioPlayer; rebuilt when the user switches Listen target.
  const playerRef = useRef<AudioPlayer | null>(null);
  const [listeningRunId, setListeningRunId] = useState<string | null>(null);

  // Track runIds we've already seen so a freshly-spawned run can be auto-
  // selected even when StartRun returns no runId.
  const knownRunIdsRef = useRef<Set<string>>(new Set(Object.keys(state.runs)));
  const [pendingAutoSelect, setPendingAutoSelect] = useState(false);

  // Load historical results on mount and after each run completes.
  const completedCount = state.completedRunIds.length;
  useEffect(() => {
    getResults().then(async (ids) => {
      if (!ids || ids.length === 0) { setHistoricalResults([]); return; }
      const results = await Promise.all(ids.map((id) => getResult(id).catch(() => null)));
      setHistoricalResults(results.filter((r): r is RunResult => r !== null));
    }).catch(() => {});
  }, [getResults, getResult, completedCount]);

  // When the user clicks Start Run we set pendingAutoSelect; the next new
  // runId that lands in state.runs gets auto-selected so the demo flow is
  // "click Run → page navigates to live conversation."
  useEffect(() => {
    const ids = Object.keys(state.runs);
    if (pendingAutoSelect) {
      const newId = ids.find((id) => !knownRunIdsRef.current.has(id));
      if (newId) {
        setSelectedRunId(newId);
        setPendingAutoSelect(false);
      }
    }
    knownRunIdsRef.current = new Set(ids);
  }, [state.runs, pendingAutoSelect]);

  useEffect(() => {
    return () => {
      playerRef.current?.close();
      playerRef.current = null;
    };
  }, []);

  const liveRuns = Object.values(state.runs);
  const selectedRun = selectedRunId ? state.runs[selectedRunId] : undefined;

  const handleSelectMessage = (index: number, message?: Message, allMsgs?: Message[]) => {
    setDevToolsIndex(index);
    setDevToolsMessage(message);
    setDevToolsAllMessages(allMsgs);
    setDevToolsOpen(true);
  };

  const handleStartRun = useCallback(async (providerId: string, scenarioId: string) => {
    setStartError(null);
    setPendingAutoSelect(true);
    // If the user is currently viewing a previous run's detail, navigate
    // them back to the dashboard immediately. Without this they'd stare
    // at the old run until SSE delivered the first turn of the new one,
    // which feels like nothing happened. The dashboard shows the live
    // run appearing, then pendingAutoSelect kicks in and switches to
    // the new RunDetail when the runId lands.
    setSelectedRunId(null);
    setDevToolsOpen(false);
    try {
      await startRun({ providers: [providerId], scenarios: [scenarioId] });
    } catch (err) {
      setPendingAutoSelect(false);
      setStartError(err instanceof Error ? err.message : "Failed to start run");
    }
  }, [startRun]);

  // Toggle audio playback for a run. Closes any prior EventSource before
  // opening a new one so we never have two audio streams in flight.
  const handleListen = useCallback((runId: string) => {
    if (listeningRunId === runId) {
      playerRef.current?.pause();
      setListeningRunId(null);
      return;
    }
    if (playerRef.current) {
      playerRef.current.close();
      playerRef.current = null;
    }
    playerRef.current = new AudioPlayer({
      runId,
      onError: (msg) => console.warn("audio:", msg),
    });
    playerRef.current.connect("/api/events");
    playerRef.current.play();
    setListeningRunId(runId);
  }, [listeningRunId]);

  const showEmptyHero = liveRuns.length === 0 && historicalResults.length === 0;

  return (
    <ErrorBoundary>
      <Layout
        connected={state.connected}
        headerActions={
          activeTab === "runs" ? (
            <RunControls
              connected={state.connected}
              loading={loading}
              startError={startError}
              onStart={handleStartRun}
            />
          ) : null
        }
      >
        {/* Tab bar */}
        <div className="flex gap-1 mb-6 border-b border-mist pb-0">
          <button
            className={`px-4 py-2 text-sm font-medium rounded-t-lg border border-b-0 transition-colors ${
              activeTab === "runs"
                ? "bg-surface border-mist text-fg"
                : "bg-canvas border-transparent text-fg-muted hover:text-fg hover:bg-surface"
            }`}
            onClick={() => setActiveTab("runs")}
          >
            Runs
          </button>
          <button
            className={`px-4 py-2 text-sm font-medium rounded-t-lg border border-b-0 transition-colors ${
              activeTab === "chat"
                ? "bg-surface border-mist text-fg"
                : "bg-canvas border-transparent text-fg-muted hover:text-fg hover:bg-surface"
            }`}
            onClick={() => setActiveTab("chat")}
          >
            Interactive Chat
          </button>
        </div>

        {activeTab === "chat" ? (
          <InteractiveChat
            state={state}
            registerInteractiveRun={registerInteractiveRun}
            onBack={() => setActiveTab("runs")}
          />
        ) : (
          <>
            <div className={devToolsOpen ? "lg:mr-[420px] transition-[margin] duration-200" : "transition-[margin] duration-200"}>
              {selectedRunId ? (
                <RunDetail
                  runId={selectedRunId}
                  liveRun={state.runs[selectedRunId]}
                  listeningRunId={listeningRunId}
                  onToggleListen={handleListen}
                  onBack={() => { setSelectedRunId(null); setDevToolsOpen(false); }}
                  onSelectMessage={handleSelectMessage}
                />
              ) : showEmptyHero ? (
                <div className="max-w-2xl mx-auto">
                  <EmptyStateLauncher
                    connected={state.connected}
                    loading={loading}
                    startError={startError}
                    onStart={handleStartRun}
                  />
                </div>
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
                  {liveRuns.length > 0 && (
                    <RunProgress
                      runs={liveRuns}
                      listeningRunId={listeningRunId}
                      onSelectRun={setSelectedRunId}
                      onToggleListen={handleListen}
                    />
                  )}
                  {historicalResults.length > 0 && (
                    <HistoricalResults
                      results={historicalResults}
                      onSelectRun={setSelectedRunId}
                      onClear={async () => {
                        await clearResults();
                        setHistoricalResults([]);
                      }}
                    />
                  )}
                </div>
              )}
            </div>
            <DevToolsPanel
              message={devToolsMessage}
              messageIndex={devToolsIndex}
              allMessages={devToolsAllMessages}
              run={selectedRun}
              open={devToolsOpen}
              onClose={() => setDevToolsOpen(false)}
            />
          </>
        )}
      </Layout>
    </ErrorBoundary>
  );
}
