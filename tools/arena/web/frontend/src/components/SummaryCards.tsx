import { Card } from "@/components/ui/card";
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
  { key: "total", label: "Total Runs", icon: Activity, color: "text-altair-blue" },
  { key: "active", label: "Active", icon: Zap, color: "text-nebula-cyan" },
  { key: "completed", label: "Completed", icon: CheckCircle, color: "text-deploy-green" },
  { key: "failed", label: "Failed", icon: XCircle, color: "text-error-red" },
  { key: "cost", label: "Total Cost", icon: DollarSign, color: "text-stellar-gold" },
  { key: "tokens", label: "Tokens", icon: Hash, color: "text-cosmic-violet" },
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
        <Card key={stat.key} className="bg-onyx border-white/10 p-4">
          <div className="flex items-center gap-2 mb-2">
            <stat.icon className={cn("h-4 w-4", stat.color)} />
            <span className="text-xs font-medium text-slate-muted uppercase tracking-wider">
              {stat.label}
            </span>
          </div>
          <div className={cn("text-2xl font-bold font-mono", stat.color)}>
            {values[stat.key]}
          </div>
        </Card>
      ))}
    </div>
  );
}
