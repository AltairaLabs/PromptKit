import { useState } from "react";
import { Layout } from "@/components/Layout";
import { SummaryCards } from "@/components/SummaryCards";
import { RunProgress } from "@/components/RunProgress";
import { ScenarioMatrix } from "@/components/ScenarioMatrix";
import { RunDetail } from "@/components/RunDetail";
import { DevToolsPanel } from "@/components/DevToolsPanel";
import { useArenaEvents } from "@/hooks/useArenaEvents";
import { useArenaAPI } from "@/hooks/useArenaAPI";
import type { Message } from "@/types";

export default function App() {
  const state = useArenaEvents();
  const { startRun, loading } = useArenaAPI();
  const [selectedRunId, setSelectedRunId] = useState<string | null>(null);
  const [devToolsMessage, setDevToolsMessage] = useState<Message | undefined>();
  const [devToolsIndex, setDevToolsIndex] = useState<number | undefined>();
  const [devToolsOpen, setDevToolsOpen] = useState(false);

  const runs = Object.values(state.runs);
  const selectedRun = selectedRunId ? state.runs[selectedRunId] : undefined;

  const handleSelectMessage = (index: number) => {
    // For completed run detail view, the message comes from RunResult
    // DevTools will show what it can from the Message type
    setDevToolsIndex(index);
    setDevToolsOpen(true);
  };

  // Suppress unused variable warning — devToolsMessage is set for future use
  void devToolsMessage;
  void setDevToolsMessage;

  return (
    <Layout connected={state.connected} onStartRun={() => startRun()} loading={loading}>
      <div className={devToolsOpen ? "mr-[420px] transition-[margin]" : ""}>
        {selectedRunId ? (
          <RunDetail
            runId={selectedRunId}
            onBack={() => setSelectedRunId(null)}
            onSelectMessage={handleSelectMessage}
          />
        ) : (
          <div className="space-y-6">
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
  );
}
