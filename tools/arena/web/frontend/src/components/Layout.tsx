import { cn } from "@/lib/utils";
import { Activity, Play, Wifi, WifiOff } from "lucide-react";
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
            <Activity className="h-6 w-6 text-altair-blue" />
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
