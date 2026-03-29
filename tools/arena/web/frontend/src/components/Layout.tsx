import { cn } from "@/lib/utils";
import { Play, Wifi, WifiOff } from "lucide-react";

interface LayoutProps {
  connected: boolean;
  onStartRun: () => void;
  loading: boolean;
  children: React.ReactNode;
}

export function Layout({ connected, onStartRun, loading, children }: LayoutProps) {
  return (
    <div className="min-h-screen bg-deep-space">
      {/* Gradient header matching altairalabs-web hero style */}
      <header className="relative overflow-hidden border-b border-white/[0.06]">
        <div className="absolute inset-0 bg-gradient-to-r from-[#3B82F6]/20 via-[#06B6D4]/10 to-[#8B5CF6]/20" />
        <div className="absolute inset-0 bg-deep-space/80 backdrop-blur-sm" />
        <div className="relative mx-auto flex h-20 max-w-[1400px] items-center justify-between px-8">
          <div className="flex items-center gap-4">
            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 128 128" fill="none" className="h-10 w-10">
              <defs>
                <linearGradient id="lg1" x1="0%" y1="0%" x2="100%" y2="100%">
                  <stop offset="0%" stopColor="#fff" stopOpacity="0.95"/>
                  <stop offset="100%" stopColor="#e0d4ff" stopOpacity="0.9"/>
                </linearGradient>
                <linearGradient id="lg2" x1="0%" y1="0%" x2="100%" y2="100%">
                  <stop offset="0%" stopColor="#e0d4ff" stopOpacity="0.9"/>
                  <stop offset="100%" stopColor="#c4b5fd" stopOpacity="0.85"/>
                </linearGradient>
              </defs>
              <path d="M24 32 L48 64 L24 96" stroke="url(#lg1)" strokeWidth="12" strokeLinecap="round" strokeLinejoin="round" fill="none"/>
              <rect x="60" y="28" width="44" height="20" rx="4" fill="url(#lg1)"/>
              <rect x="60" y="54" width="44" height="20" rx="4" fill="url(#lg2)"/>
              <rect x="60" y="80" width="44" height="20" rx="4" fill="#c4b5fd" fillOpacity="0.85"/>
            </svg>
            <div>
              <h1 className="text-xl font-bold text-cloud-white tracking-tight">PromptArena</h1>
              <p className="text-xs text-slate-muted">Live Testing Dashboard</p>
            </div>
          </div>
          <div className="flex items-center gap-5">
            <div className={cn(
              "flex items-center gap-2 rounded-full px-4 py-1.5 text-xs font-medium border",
              connected
                ? "bg-deploy-green/[0.08] text-deploy-green border-deploy-green/20"
                : "bg-error-red/[0.08] text-error-red border-error-red/20"
            )}>
              {connected ? <Wifi className="h-3.5 w-3.5" /> : <WifiOff className="h-3.5 w-3.5" />}
              {connected ? "Live" : "Disconnected"}
            </div>
            <button
              onClick={onStartRun}
              disabled={loading || !connected}
              className={cn(
                "flex items-center gap-2 rounded-lg px-5 py-2.5 text-sm font-semibold text-white transition-all",
                "bg-gradient-to-r from-[#3B82F6] to-[#06B6D4] hover:from-[#2563EB] hover:to-[#0891B2]",
                "shadow-[0_0_20px_rgba(59,130,246,0.3)] hover:shadow-[0_0_30px_rgba(59,130,246,0.5)]",
                "disabled:opacity-40 disabled:cursor-not-allowed disabled:shadow-none"
              )}
            >
              <Play className="h-4 w-4" />
              Start Run
            </button>
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-[1400px] px-8 py-8">
        {children}
      </main>
    </div>
  );
}
