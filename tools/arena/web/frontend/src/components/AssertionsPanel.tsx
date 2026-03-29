import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { CheckCircle, XCircle } from "lucide-react";
import type { AssertionsSummary } from "@/types";

interface AssertionsPanelProps {
  assertions?: AssertionsSummary;
}

export function AssertionsPanel({ assertions }: AssertionsPanelProps) {
  if (!assertions || assertions.total === 0) return null;

  const passCount = assertions.total - assertions.failed;

  return (
    <div className="rounded-lg bg-onyx border border-white/10 overflow-hidden">
      <div className="flex items-center gap-3 p-4 border-b border-white/10">
        <Badge
          className={cn(
            "text-xs",
            assertions.passed
              ? "bg-deploy-green/10 text-deploy-green border-deploy-green/30"
              : "bg-error-red/10 text-error-red border-error-red/30"
          )}
        >
          {assertions.passed ? (
            <CheckCircle className="h-3 w-3 mr-1" />
          ) : (
            <XCircle className="h-3 w-3 mr-1" />
          )}
          {assertions.passed ? "Passed" : "Failed"}
        </Badge>
        <span className="text-sm font-medium text-cloud-white">Assertions</span>
        <span className="text-xs text-slate-muted ml-auto">
          {passCount}/{assertions.total}
        </span>
      </div>
      <div className="divide-y divide-white/5">
        {assertions.results.map((result, i) => (
          <div
            key={i}
            className={cn(
              "flex items-center gap-3 px-4 py-3",
              result.passed ? "bg-deploy-green/5" : "bg-error-red/5"
            )}
          >
            <span
              className={cn(
                "h-5 w-5 rounded-full flex items-center justify-center text-xs",
                result.passed ? "bg-deploy-green text-white" : "bg-error-red text-white"
              )}
            >
              {result.passed ? "✓" : "✗"}
            </span>
            <div className="flex-1 min-w-0">
              <div className="text-sm font-medium text-cloud-white">{result.name}</div>
              <div className="text-xs text-slate-muted">{result.type}</div>
              {result.message && (
                <div className="text-xs text-slate-muted mt-1">{result.message}</div>
              )}
            </div>
            {result.score != null && (
              <span className="text-xs font-mono text-slate-muted">{result.score.toFixed(2)}</span>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
