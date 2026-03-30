import { cn } from "@/lib/utils";
import { Activity, CheckCircle, XCircle, Zap, DollarSign, Hash } from "lucide-react";

interface SummaryCardsProps {
  totalRuns: number;
  activeRuns: number;
  completedRuns: number;
  failedRuns: number;
  totalCost: number;
  totalTokens: number;
}

const stats = [
  { key: "total", label: "Total Runs", icon: Activity, color: "text-[#2563EB]", bg: "bg-blue-50" },
  { key: "active", label: "Active", icon: Zap, color: "text-[#06B6D4]", bg: "bg-cyan-50" },
  { key: "completed", label: "Completed", icon: CheckCircle, color: "text-[#10B981]", bg: "bg-emerald-50" },
  { key: "failed", label: "Failed", icon: XCircle, color: "text-[#EF4444]", bg: "bg-red-50" },
  { key: "cost", label: "Total Cost", icon: DollarSign, color: "text-[#F59E0B]", bg: "bg-amber-50" },
  { key: "tokens", label: "Tokens", icon: Hash, color: "text-[#8B5CF6]", bg: "bg-violet-50" },
] as const;

export function SummaryCards(props: SummaryCardsProps) {
  const values: Record<string, string> = {
    total: String(props.totalRuns),
    active: String(props.activeRuns),
    completed: String(props.completedRuns),
    failed: String(props.failedRuns),
    cost: `$${props.totalCost.toFixed(4)}`,
    tokens: props.totalTokens.toLocaleString(),
  };

  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
      {stats.map((stat) => (
        <div key={stat.key} className="rounded-xl border border-mist bg-white p-4 shadow-sm">
          <div className="flex items-center gap-2 mb-2">
            <div className={cn("rounded-lg p-1.5", stat.bg)}>
              <stat.icon className={cn("h-3.5 w-3.5", stat.color)} />
            </div>
            <span className="text-[11px] font-medium text-slate-muted uppercase tracking-wider">
              {stat.label}
            </span>
          </div>
          <div className={cn("text-2xl font-bold font-mono", stat.color)}>
            {values[stat.key]}
          </div>
        </div>
      ))}
    </div>
  );
}
