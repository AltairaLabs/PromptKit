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

// SummaryCards renders a hero card (pass rate + total runs, the headline
// metric for the dashboard) plus a strip of secondary metrics. Sizing is
// asymmetric on purpose — equal-weight stat grids hide the lede.
export function SummaryCards(props: SummaryCardsProps) {
  const passRate = props.totalRuns > 0
    ? ((props.completedRuns - props.failedRuns) / Math.max(props.completedRuns, 1)) * 100
    : 0;
  const passRateLabel = props.completedRuns === 0 ? "—" : `${passRate.toFixed(0)}%`;

  return (
    <div className="grid grid-cols-1 lg:grid-cols-3 gap-3">
      <HeroCard
        passRate={passRateLabel}
        completed={props.completedRuns}
        failed={props.failedRuns}
      />
      <div className="lg:col-span-2 grid grid-cols-2 sm:grid-cols-4 gap-3">
        <Stat icon={Zap} label="Active" value={String(props.activeRuns)} color="text-[#06B6D4]" bg="bg-cyan-50" />
        <Stat icon={CheckCircle} label="Completed" value={String(props.completedRuns)} color="text-[#10B981]" bg="bg-emerald-50" />
        <Stat icon={XCircle} label="Failed" value={String(props.failedRuns)} color="text-[#EF4444]" bg="bg-red-50" />
        <Stat icon={DollarSign} label="Cost" value={`$${props.totalCost.toFixed(4)}`} color="text-[#F59E0B]" bg="bg-amber-50" mono />
        <Stat icon={Hash} label="Tokens" value={props.totalTokens.toLocaleString()} color="text-[#8B5CF6]" bg="bg-violet-50" mono className="col-span-2 sm:col-span-4" />
      </div>
    </div>
  );
}

function HeroCard({ passRate, completed, failed }: { passRate: string; completed: number; failed: number }) {
  const passing = failed === 0 && completed > 0;
  return (
    <div className="rounded-xl border border-mist bg-white p-5 shadow-sm flex flex-col justify-between">
      <div className="flex items-center gap-2">
        <Activity className="h-4 w-4 text-[#2563EB]" />
        <span className="text-[11px] font-semibold uppercase tracking-wider text-fg-muted">
          Pass rate
        </span>
      </div>
      <div className="flex items-baseline gap-3 mt-2">
        <span className={cn(
          "text-5xl font-extrabold font-mono",
          completed === 0 ? "text-fg-muted" : passing ? "text-[#10B981]" : "text-[#F59E0B]",
        )}>
          {passRate}
        </span>
        {completed > 0 && (
          <span className="text-sm text-fg-muted">
            {completed - failed} / {completed}
          </span>
        )}
      </div>
      <div className="flex items-center gap-3 text-[11px] text-fg-muted mt-2">
        <span className="inline-flex items-center gap-1">
          <span className="h-1.5 w-1.5 rounded-full bg-[#10B981]" />
          {completed - failed} passing
        </span>
        <span className="inline-flex items-center gap-1">
          <span className="h-1.5 w-1.5 rounded-full bg-[#EF4444]" />
          {failed} failing
        </span>
      </div>
    </div>
  );
}

function Stat({
  icon: Icon,
  label,
  value,
  color,
  bg,
  mono,
  className,
}: {
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  value: string;
  color: string;
  bg: string;
  mono?: boolean;
  className?: string;
}) {
  return (
    <div className={cn("rounded-xl border border-mist bg-white p-3 shadow-sm", className)}>
      <div className="flex items-center gap-1.5 mb-1">
        <div className={cn("rounded-md p-1", bg)}>
          <Icon className={cn("h-3 w-3", color)} />
        </div>
        <span className="text-[10px] font-medium text-fg-muted uppercase tracking-wider">
          {label}
        </span>
      </div>
      <div className={cn("text-xl font-bold", mono && "font-mono", color)}>
        {value}
      </div>
    </div>
  );
}
