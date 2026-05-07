import { useEffect, useMemo, useState } from "react";
import { Play, Sparkles, FlaskConical, Mic } from "lucide-react";
import { useArenaAPI } from "@/hooks/useArenaAPI";
import type { ProviderInfo, ScenarioInfo } from "@/types";

interface EmptyStateLauncherProps {
  connected: boolean;
  loading: boolean;
  startError: string | null;
  onStart: (providerId: string, scenarioId: string) => Promise<void>;
}

function looksMock(p: ProviderInfo): boolean {
  return /mock/i.test(p.id) || /mock/i.test(p.type);
}

// EmptyStateLauncher is the centred "Start a run" hero shown when the user
// has no live or historical runs. Bigger, more inviting than the header
// controls — it explains what to do and provides the same picker surface.
export function EmptyStateLauncher({ connected, loading, startError, onStart }: EmptyStateLauncherProps) {
  const { getRunOptions } = useArenaAPI();
  const [providers, setProviders] = useState<ProviderInfo[]>([]);
  const [scenarios, setScenarios] = useState<ScenarioInfo[]>([]);
  const [providerId, setProviderId] = useState("");
  const [scenarioId, setScenarioId] = useState("");
  const [optionsError, setOptionsError] = useState<string | null>(null);

  useEffect(() => {
    getRunOptions()
      .then((opts) => {
        setProviders(opts.providers ?? []);
        setScenarios(opts.scenarios ?? []);
      })
      .catch((e: Error) => setOptionsError(e.message));
  }, [getRunOptions]);

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

  const canStart = connected && !loading && providerId !== "" && scenarioId !== "";

  return (
    <div className="rounded-2xl border border-mist bg-surface shadow-sm p-10 text-center">
      <div className="mx-auto mb-4 inline-flex items-center justify-center h-12 w-12 rounded-full bg-violet-100">
        <Sparkles className="h-6 w-6 text-[#8B5CF6]" />
      </div>
      <h2 className="text-xl font-bold text-fg mb-2">Run your first scenario</h2>
      <p className="text-sm text-fg-muted max-w-md mx-auto mb-8 leading-relaxed">
        Pick a provider and a scenario, then click Run. Mock providers don't spend tokens, so the
        first click is free.
      </p>

      <div className="flex flex-wrap items-end gap-3 justify-center max-w-xl mx-auto">
        <Picker
          label="Provider"
          icon={<FlaskConical className="h-3.5 w-3.5" />}
          value={providerId}
          onChange={setProviderId}
          options={providers.map((p) => ({
            value: p.id,
            label: p.id + (looksMock(p) ? " · free" : " · 💰"),
          }))}
          loading={providers.length === 0}
        />
        <Picker
          label="Scenario"
          icon={<Mic className="h-3.5 w-3.5" />}
          value={scenarioId}
          onChange={setScenarioId}
          options={scenarios.map((s) => ({ value: s.id, label: s.id }))}
          loading={scenarios.length === 0}
        />
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
          className="rounded-lg bg-[#2563EB] hover:bg-[#1D4ED8] text-white px-5 py-2.5 text-sm font-semibold flex items-center gap-2 transition-colors disabled:opacity-40 disabled:cursor-not-allowed shadow-sm"
        >
          <Play className="h-4 w-4 fill-white" />
          {loading ? "Starting…" : "Run"}
        </button>
      </div>

      {!isMockSelected && providerId !== "" && (
        <p className="text-[11px] text-[#F59E0B] mt-3">
          ⚠ This provider spends real tokens.
        </p>
      )}

      {(optionsError || startError) && (
        <p className="text-xs text-[#EF4444] mt-3" role="alert">
          {optionsError ?? startError}
        </p>
      )}
    </div>
  );
}

function Picker({
  label,
  icon,
  value,
  onChange,
  options,
  loading,
}: {
  label: string;
  icon?: React.ReactNode;
  value: string;
  onChange: (v: string) => void;
  options: Array<{ value: string; label: string }>;
  loading: boolean;
}) {
  return (
    <div className="text-left">
      <label className="text-[10px] font-semibold uppercase tracking-wider text-fg-muted flex items-center gap-1 mb-1">
        {icon}
        {label}
      </label>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        disabled={loading}
        className="rounded-lg border border-mist bg-surface px-3 py-2 text-sm font-medium text-fg focus:outline-none focus:ring-2 focus:ring-[#2563EB]/30 focus:border-[#2563EB] disabled:opacity-40 min-w-[200px]"
      >
        {loading && <option value="">(loading…)</option>}
        {options.map((o) => (
          <option key={o.value} value={o.value}>{o.label}</option>
        ))}
      </select>
    </div>
  );
}
