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
  { key: "total", label: "Total Runs", icon: Activity, color: "text-altair-blue", glow: "shadow-[0_0_15px_rgba(37,99,235,0.15)]" },
  { key: "active", label: "Active", icon: Zap, color: "text-nebula-cyan", glow: "shadow-[0_0_15px_rgba(6,182,212,0.15)]" },
  { key: "completed", label: "Completed", icon: CheckCircle, color: "text-deploy-green", glow: "shadow-[0_0_15px_rgba(16,185,129,0.15)]" },
  { key: "failed", label: "Failed", icon: XCircle, color: "text-error-red", glow: "shadow-[0_0_15px_rgba(239,68,68,0.15)]" },
  { key: "cost", label: "Total Cost", icon: DollarSign, color: "text-stellar-gold", glow: "shadow-[0_0_15px_rgba(245,158,11,0.15)]" },
  { key: "tokens", label: "Tokens", icon: Hash, color: "text-cosmic-violet", glow: "shadow-[0_0_15px_rgba(139,92,246,0.15)]" },
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
    <div className="grid grid-cols-2 gap-4 md:grid-cols-3 lg:grid-cols-6">
      {stats.map((stat) => (
        <div
          key={stat.key}
          className={cn(
            "rounded-xl border border-white/[0.06] bg-onyx p-5 transition-all hover:border-white/[0.12]",
            stat.glow
          )}
        >
          <div className="flex items-center gap-2 mb-3">
            <stat.icon className={cn("h-4 w-4 opacity-70", stat.color)} />
            <span className="text-[11px] font-medium text-slate-muted uppercase tracking-widest">
              {stat.label}
            </span>
          </div>
          <div className={cn("text-3xl font-bold font-mono tracking-tight", stat.color)}>
            {values[stat.key]}
          </div>
        </div>
      ))}
    </div>
  );
}
