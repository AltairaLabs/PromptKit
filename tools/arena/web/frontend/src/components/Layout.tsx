import { cn } from "@/lib/utils";
import { Play, Wifi, WifiOff } from "lucide-react";
import { Button } from "@/components/ui/button";

interface LayoutProps {
  connected: boolean;
  onStartRun: () => void;
  loading: boolean;
  children: React.ReactNode;
}

export function Layout({ connected, onStartRun, loading, children }: LayoutProps) {
  return (
    <div className="min-h-screen bg-deep-space">
      <header className="sticky top-0 z-50 border-b border-white/10 bg-[rgba(15,23,42,0.95)] backdrop-blur-[10px]">
        <div className="mx-auto flex h-16 max-w-[1400px] items-center justify-between px-6">
          <div className="flex items-center gap-3">
            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 128 128" fill="none" className="h-8 w-8">
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
            <span className="text-lg font-bold text-cloud-white">PromptArena</span>
          </div>
          <div className="flex items-center gap-4">
            <div className={cn(
              "flex items-center gap-2 rounded-full px-3 py-1 text-xs font-medium",
              connected
                ? "bg-deploy-green/10 text-deploy-green border border-deploy-green/30"
                : "bg-error-red/10 text-error-red border border-error-red/30"
            )}>
              {connected ? <Wifi className="h-3 w-3" /> : <WifiOff className="h-3 w-3" />}
              {connected ? "Connected" : "Disconnected"}
            </div>
            <Button
              onClick={onStartRun}
              disabled={loading || !connected}
              className="bg-altair-blue hover:bg-altair-blue-dark text-white"
              size="sm"
            >
              <Play className="mr-2 h-4 w-4" />
              Start Run
            </Button>
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-[1400px] px-6 py-6">
        {children}
      </main>
    </div>
  );
}
