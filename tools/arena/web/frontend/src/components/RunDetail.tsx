import { useEffect, useState } from "react";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { ArrowLeft } from "lucide-react";
import { Button } from "@/components/ui/button";
import { ConversationThread } from "@/components/ConversationThread";
import { AssertionsPanel } from "@/components/AssertionsPanel";
import { useArenaAPI } from "@/hooks/useArenaAPI";
import type { RunResult } from "@/types";

interface RunDetailProps {
  runId: string;
  onBack: () => void;
  onSelectMessage?: (index: number) => void;
}

export function RunDetail({ runId, onBack, onSelectMessage }: RunDetailProps) {
  const { getResult } = useArenaAPI();
  const [result, setResult] = useState<RunResult | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    getResult(runId).then(setResult).catch((e: Error) => setError(e.message));
  }, [runId, getResult]);

  if (error) {
    return (
      <Card className="bg-onyx border-white/10 p-6">
        <p className="text-error-red">Failed to load run: {error}</p>
        <Button variant="ghost" onClick={onBack} className="mt-4 text-altair-blue">
          <ArrowLeft className="mr-2 h-4 w-4" /> Back
        </Button>
      </Card>
    );
  }

  if (!result) {
    return (
      <Card className="bg-onyx border-white/10 p-6 text-center">
        <p className="text-slate-muted">Loading run details...</p>
      </Card>
    );
  }

  const durationSec = result.Duration / 1_000_000_000;

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <Button variant="ghost" onClick={onBack} size="sm" className="text-altair-blue">
          <ArrowLeft className="mr-2 h-4 w-4" /> Back
        </Button>
        <h2 className="text-lg font-semibold text-cloud-white">{result.ScenarioID}</h2>
        <Badge className={result.Error
          ? "bg-error-red/10 text-error-red"
          : "bg-deploy-green/10 text-deploy-green"
        }>
          {result.Error ? "Failed" : "Passed"}
        </Badge>
      </div>

      {/* Metadata grid */}
      <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
        {[
          { label: "Provider", value: result.ProviderID },
          { label: "Region", value: result.Region },
          { label: "Duration", value: `${durationSec.toFixed(1)}s` },
          { label: "Cost", value: `$${result.Cost.total_cost_usd.toFixed(4)}` },
          { label: "Input Tokens", value: result.Cost.input_tokens.toLocaleString() },
          { label: "Output Tokens", value: result.Cost.output_tokens.toLocaleString() },
          { label: "Turns", value: String(result.Messages.length) },
          { label: "Pack", value: result.PromptPack || "—" },
        ].map((m) => (
          <Card key={m.label} className="bg-onyx border-white/10 p-3">
            <div className="text-xs text-slate-muted uppercase tracking-wider">{m.label}</div>
            <div className="text-sm font-mono text-cloud-white mt-1">{m.value}</div>
          </Card>
        ))}
      </div>

      {result.Error && (
        <Card className="bg-error-red/5 border-error-red/20 p-4">
          <div className="text-sm text-error-red">{result.Error}</div>
        </Card>
      )}

      <AssertionsPanel assertions={result.ConversationAssertions} />

      <div>
        <h3 className="text-sm font-medium text-slate-muted uppercase tracking-wider mb-3">
          Conversation
        </h3>
        <ConversationThread messages={result.Messages} onSelectMessage={onSelectMessage} />
      </div>
    </div>
  );
}
