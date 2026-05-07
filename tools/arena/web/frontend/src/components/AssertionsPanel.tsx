import { useState } from "react";
import { cn } from "@/lib/utils";
import { CheckCircle, XCircle, ChevronRight } from "lucide-react";
import type { AssertionsSummary, ConversationValidationResult } from "@/types";

interface AssertionsPanelProps {
  assertions?: AssertionsSummary;
  // When true, renders only the summary pill — the full result list is
  // collapsed under a click. Used in RunDetail's sticky header so the
  // assertions don't dominate vertical space.
  compactDefault?: boolean;
}

// AssertionsPanel mirrors the HTML report's conversation-level assertion
// table: a header row showing pass/fail summary and per-row expanded
// details with violations + evidence. Themed (no longer hardcoded dark
// slab) so it sits naturally in both light and dark modes.
export function AssertionsPanel({ assertions, compactDefault }: AssertionsPanelProps) {
  const [expanded, setExpanded] = useState(!compactDefault);
  if (!assertions || assertions.total === 0) return null;

  const passCount = assertions.total - assertions.failed;
  const passed = assertions.passed;

  return (
    <div className={cn(
      "rounded-lg border overflow-hidden bg-surface",
      passed ? "border-emerald-200" : "border-red-200",
    )}>
      <button
        type="button"
        onClick={() => setExpanded((x) => !x)}
        className={cn(
          "w-full flex items-center gap-3 p-3 text-left",
          passed ? "hover:bg-emerald-50/50" : "hover:bg-red-50/50",
        )}
      >
        <span
          className={cn(
            "inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-semibold",
            passed
              ? "bg-emerald-50 text-[#10B981] border border-emerald-200"
              : "bg-red-50 text-[#EF4444] border border-red-200",
          )}
        >
          {passed ? <CheckCircle className="h-3 w-3" /> : <XCircle className="h-3 w-3" />}
          {passed ? "Passed" : "Failed"}
        </span>
        <span className="text-sm font-medium text-fg">Assertions</span>
        <span className="text-xs text-fg-muted ml-auto font-mono">
          {passCount}/{assertions.total}
        </span>
        <ChevronRight
          className={cn("h-3.5 w-3.5 text-fg-muted transition-transform", expanded && "rotate-90")}
        />
      </button>
      {expanded && (
        <div className="border-t border-mist divide-y divide-mist">
          {assertions.results.map((result, i) => (
            <AssertionRow key={i} result={result} />
          ))}
        </div>
      )}
    </div>
  );
}

function AssertionRow({ result }: { result: ConversationValidationResult }) {
  const [open, setOpen] = useState(false);
  const hasDetails = !!(
    (result.violations && result.violations.length > 0) ||
    (result.details && Object.keys(result.details).length > 0)
  );
  return (
    <div className={cn(result.passed ? "bg-emerald-50/30" : "bg-red-50/30")}>
      <button
        type="button"
        onClick={() => hasDetails && setOpen((x) => !x)}
        className={cn(
          "w-full flex items-center gap-3 px-4 py-2.5 text-left",
          hasDetails ? "cursor-pointer hover:bg-white/50" : "cursor-default",
        )}
      >
        <span
          className={cn(
            "h-5 w-5 rounded-full flex items-center justify-center text-[11px] font-bold shrink-0",
            result.passed ? "bg-[#10B981] text-white" : "bg-[#EF4444] text-white"
          )}
        >
          {result.passed ? "✓" : "✗"}
        </span>
        <div className="flex-1 min-w-0">
          <div className="text-sm font-medium text-fg truncate">{result.name ?? result.type}</div>
          <div className="text-[11px] font-mono text-fg-muted truncate">{result.type}</div>
          {result.message && (
            <div className="text-xs text-fg-muted mt-1 line-clamp-2">{result.message}</div>
          )}
        </div>
        {result.score != null && (
          <span className="text-xs font-mono text-fg-muted shrink-0">{result.score.toFixed(2)}</span>
        )}
        {hasDetails && (
          <ChevronRight className={cn("h-3 w-3 text-fg-muted transition-transform shrink-0", open && "rotate-90")} />
        )}
      </button>
      {open && hasDetails && (
        <div className="px-4 pb-3 pt-1 space-y-2 bg-[var(--c-surface-2)]">
          {result.violations && result.violations.length > 0 && (
            <div>
              <div className="text-[10px] uppercase tracking-wider text-fg-muted mb-1 mt-2">
                Violations ({result.violations.length})
              </div>
              <div className="space-y-1.5">
                {result.violations.map((v, j) => (
                  <div key={j} className="rounded border border-red-200 bg-red-50 px-2 py-1.5">
                    <div className="flex items-baseline gap-2">
                      <span className="text-[10px] font-mono text-[#EF4444] shrink-0">turn {v.turn_index}</span>
                      <span className="text-[11px] text-fg">{v.description}</span>
                    </div>
                    {v.evidence && (
                      <pre className="mt-1 text-[10px] font-mono text-fg-muted whitespace-pre-wrap overflow-x-auto">
                        {JSON.stringify(v.evidence, null, 2)}
                      </pre>
                    )}
                  </div>
                ))}
              </div>
            </div>
          )}
          {result.details && Object.keys(result.details).length > 0 && (
            <div>
              <div className="text-[10px] uppercase tracking-wider text-fg-muted mb-1 mt-2">Details</div>
              <pre className="text-[10px] font-mono text-fg whitespace-pre-wrap overflow-x-auto bg-surfaceborder border-mist rounded px-2 py-1">
                {JSON.stringify(result.details, null, 2)}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
