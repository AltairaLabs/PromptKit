import { Layout } from "@/components/Layout";
import { SummaryCards } from "@/components/SummaryCards";
import { RunProgress } from "@/components/RunProgress";
import { useArenaEvents } from "@/hooks/useArenaEvents";
import { useArenaAPI } from "@/hooks/useArenaAPI";

export default function App() {
  const state = useArenaEvents();
  const { startRun, loading } = useArenaAPI();

  const runs = Object.values(state.runs);
  const activeRuns = runs.filter((r) => r.status === "running");
  const completedRuns = runs.filter((r) => r.status !== "running");

  return (
    <Layout connected={state.connected} onStartRun={() => startRun()} loading={loading}>
      <div className="space-y-6">
        <SummaryCards
          totalRuns={runs.length}
          activeRuns={activeRuns.length}
          completedRuns={completedRuns.length}
          failedRuns={runs.filter((r) => r.status === "failed").length}
          totalCost={state.totalCost}
          totalTokens={state.totalTokens}
        />
        <RunProgress runs={runs} />
      </div>
    </Layout>
  );
}
