import { useEffect, useMemo, useState } from "react";
import { Play } from "lucide-react";
import { useArenaAPI } from "@/hooks/useArenaAPI";
import type { ProviderInfo, ScenarioInfo } from "@/types";

interface RunControlsProps {
  connected: boolean;
  loading: boolean;
  startError: string | null;
  onStart: (providerId: string, scenarioId: string) => Promise<void>;
}

// looksMock returns true when a provider's id or type suggests a no-cost mock.
// Used to default the picker to a free option so a casual click doesn't burn tokens.
function looksMock(p: ProviderInfo): boolean {
  return /mock/i.test(p.id) || /mock/i.test(p.type);
}

export function RunControls({ connected, loading, startError, onStart }: RunControlsProps) {
  const { getRunOptions } = useArenaAPI();
  const [providers, setProviders] = useState<ProviderInfo[]>([]);
  const [scenarios, setScenarios] = useState<ScenarioInfo[]>([]);
  const [providerId, setProviderId] = useState<string>("");
  const [scenarioId, setScenarioId] = useState<string>("");
  const [optionsError, setOptionsError] = useState<string | null>(null);

  useEffect(() => {
    getRunOptions()
      .then((opts) => {
        setProviders(opts.providers ?? []);
        setScenarios(opts.scenarios ?? []);
      })
      .catch((e: Error) => setOptionsError(e.message));
  }, [getRunOptions]);

  // Default selection: prefer a mock provider so the first click is free.
  // First scenario by sort order.
  useEffect(() => {
    if (!providerId && providers.length > 0) {
      const mock = providers.find(looksMock);
      setProviderId(mock?.id ?? providers[0].id);
    }
  }, [providers, providerId]);

  useEffect(() => {
    if (!scenarioId && scenarios.length > 0) {
      setScenarioId(scenarios[0].id);
    }
  }, [scenarios, scenarioId]);

  const isMockSelected = useMemo(() => {
    const p = providers.find((x) => x.id === providerId);
    return p ? looksMock(p) : false;
  }, [providers, providerId]);

  const canStart =
    connected && !loading && providerId !== "" && scenarioId !== "";

  return (
    <div className="flex items-center gap-2">
      {/* Provider dropdown — `· free` for mocks, `· 💰` for real providers
          so the cost mode is unambiguous. */}
      <select
        value={providerId}
        onChange={(e) => setProviderId(e.target.value)}
        disabled={!connected || providers.length === 0}
        className="rounded-lg bg-white/10 border border-white/20 px-2 py-1.5 text-xs font-medium text-white hover:bg-white/15 focus:outline-none focus:ring-1 focus:ring-white/40 disabled:opacity-40"
        title="Provider — mock providers don't spend tokens"
      >
        {providers.length === 0 && <option value="">(loading…)</option>}
        {providers.map((p) => (
          <option key={p.id} value={p.id}>
            {p.id}
            {looksMock(p) ? " · free" : " · 💰"}
          </option>
        ))}
      </select>

      {/* Scenario dropdown */}
      <select
        value={scenarioId}
        onChange={(e) => setScenarioId(e.target.value)}
        disabled={!connected || scenarios.length === 0}
        className="rounded-lg bg-white/10 border border-white/20 px-2 py-1.5 text-xs font-medium text-white hover:bg-white/15 focus:outline-none focus:ring-1 focus:ring-white/40 disabled:opacity-40"
        title="Scenario to execute"
      >
        {scenarios.length === 0 && <option value="">(loading…)</option>}
        {scenarios.map((s) => (
          <option key={s.id} value={s.id}>
            {s.id}
          </option>
        ))}
      </select>

      <button
        onClick={() => {
          if (!isMockSelected) {
            const ok = window.confirm(
              `"${providerId}" is a real provider — this run will spend tokens. Continue?`,
            );
            if (!ok) return;
          }
          onStart(providerId, scenarioId);
        }}
        disabled={!canStart}
        className="rounded-lg bg-white px-4 py-2 text-sm font-semibold text-[#0F172A] hover:bg-white/90 disabled:opacity-40 disabled:cursor-not-allowed flex items-center gap-2 transition-colors"
        title={isMockSelected ? "Start a free run with the mock provider" : "Start a run — this will spend real tokens"}
      >
        <Play className="h-3.5 w-3.5" />
        Start Run
      </button>

      {(optionsError || startError) && (
        <span className="text-xs text-red-300" role="alert">
          {optionsError ?? startError}
        </span>
      )}
    </div>
  );
}
