import { useState } from "react";
import { ChevronRight } from "lucide-react";
import { cn } from "@/lib/utils";
import type { EvalResult } from "@/types";

interface EvalsPanelProps {
  evals?: EvalResult[];
}

// EvalsPanel renders pack-level eval observations — non-gating measurements
// that fire during a session. Mirrors the HTML report's "Eval Observations"
// table, with a per-row collapsible Details panel.
export function EvalsPanel({ evals }: EvalsPanelProps) {
  if (!evals || evals.length === 0) return null;
  return (
    <Section title="Eval Observations" count={evals.length}>
      <div className="rounded-lg border border-mist bg-white overflow-hidden">
        {evals.map((e, i) => (
          <EvalRow key={`${e.eval_id}-${i}`} evalResult={e} />
        ))}
      </div>
    </Section>
  );
}

function EvalRow({ evalResult }: { evalResult: EvalResult }) {
  const [open, setOpen] = useState(false);
  const hasDetails = !!(
    evalResult.details ||
    evalResult.violations?.length ||
    evalResult.value !== undefined ||
    evalResult.explanation
  );
  const score = evalResult.score;
  const metric = evalResult.metric_value;
  return (
    <div className="border-b border-mist last:border-b-0">
      <button
        type="button"
        onClick={() => hasDetails && setOpen((x) => !x)}
        className={cn(
          "w-full flex items-center gap-3 px-4 py-2.5 text-left text-xs",
          hasDetails ? "cursor-pointer hover:bg-[var(--c-surface-2)]" : "cursor-default",
        )}
      >
        <span className="font-mono text-[#8B5CF6] text-[11px] truncate min-w-[120px]">
          {evalResult.eval_id}
        </span>
        <span className="rounded-full bg-[var(--c-surface-2)] text-fg-muted px-2 py-0.5 text-[10px] font-mono">
          {evalResult.type}
        </span>
        {evalResult.skipped && (
          <span className="text-[10px] uppercase tracking-wider text-fg-muted">⏭ skipped</span>
        )}
        {!evalResult.skipped && score != null && (
          <span className="text-[11px] font-mono text-fg" title="Score">
            score {score.toFixed(3)}
          </span>
        )}
        {!evalResult.skipped && metric != null && (
          <span className="text-[11px] font-mono text-fg" title="Metric value">
            metric {metric.toFixed(3)}
          </span>
        )}
        {evalResult.error && (
          <span className="text-[11px] text-[#EF4444] truncate">{evalResult.error}</span>
        )}
        {evalResult.message && !evalResult.error && (
          <span className="text-[11px] text-fg-muted truncate">{evalResult.message}</span>
        )}
        {evalResult.duration_ms > 0 && (
          <span className="ml-auto text-[10px] font-mono text-fg-muted">
            {evalResult.duration_ms}ms
          </span>
        )}
        {hasDetails && (
          <ChevronRight className={cn("h-3 w-3 text-fg-muted transition-transform", open && "rotate-90", evalResult.duration_ms > 0 ? "" : "ml-auto")} />
        )}
      </button>
      {open && hasDetails && (
        <div className="px-4 pb-3 pt-1 bg-[var(--c-surface-2)] space-y-2">
          {evalResult.explanation && (
            <div className="text-[11px] text-fg leading-relaxed">{evalResult.explanation}</div>
          )}
          {evalResult.skip_reason && (
            <div className="text-[11px] italic text-fg-muted">Skipped: {evalResult.skip_reason}</div>
          )}
          {evalResult.value !== undefined && (
            <KeyValue label="Value" value={evalResult.value} />
          )}
          {evalResult.violations && evalResult.violations.length > 0 && (
            <div>
              <div className="text-[10px] uppercase tracking-wider text-fg-muted mb-1">Violations</div>
              <div className="space-y-1">
                {evalResult.violations.map((v, j) => (
                  <div key={j} className="rounded border border-red-200 bg-red-50 px-2 py-1 text-[11px] text-[#EF4444]">
                    {typeof v.turn_index === "number" && (
                      <span className="font-mono mr-2">turn {v.turn_index}</span>
                    )}
                    {v.description}
                    {v.evidence && (
                      <pre className="mt-1 text-[10px] font-mono text-deep-space/70 whitespace-pre-wrap">
                        {JSON.stringify(v.evidence, null, 2)}
                      </pre>
                    )}
                  </div>
                ))}
              </div>
            </div>
          )}
          {evalResult.details && (
            <KeyValue label="Details" value={evalResult.details} />
          )}
        </div>
      )}
    </div>
  );
}

function KeyValue({ label, value }: { label: string; value: unknown }) {
  const text = typeof value === "string" ? value : JSON.stringify(value, null, 2);
  return (
    <div>
      <div className="text-[10px] uppercase tracking-wider text-fg-muted mb-1">{label}</div>
      <pre className="text-[11px] font-mono text-fg whitespace-pre-wrap overflow-x-auto bg-[var(--c-surface)] border border-mist rounded px-2 py-1">{text}</pre>
    </div>
  );
}

function Section({ title, count, children }: { title: string; count?: number; children: React.ReactNode }) {
  return (
    <div className="space-y-2">
      <h3 className="text-xs font-semibold text-fg-muted uppercase tracking-wider flex items-center gap-2">
        {title}
        {count != null && (
          <span className="rounded-full bg-[var(--c-surface-2)] text-fg-muted px-2 py-0.5 text-[10px] font-mono">{count}</span>
        )}
      </h3>
      {children}
    </div>
  );
}
