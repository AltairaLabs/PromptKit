import { cn } from "@/lib/utils";
import { Wifi, WifiOff, Sun, Moon } from "lucide-react";
import { useTheme } from "@/hooks/useTheme";

interface LayoutProps {
  connected: boolean;
  headerActions?: React.ReactNode;
  children: React.ReactNode;
}

export function Layout({ connected, headerActions, children }: LayoutProps) {
  const { theme, toggle } = useTheme();
  return (
    <div className="min-h-screen bg-canvas">
      <header className="bg-gradient-to-r from-[#0F172A] to-[#1E293B] border-b border-white/10">
        <div className="mx-auto flex h-16 max-w-[1200px] items-center justify-between px-6">
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
            <span className="text-base font-bold text-white">PromptArena</span>
          </div>
          <div className="flex items-center gap-3">
            <div className={cn(
              "flex items-center gap-1.5 rounded-full px-3 py-1 text-xs font-medium",
              connected
                ? "bg-emerald-400/15 text-emerald-300"
                : "bg-red-400/15 text-red-300"
            )}>
              {connected ? <Wifi className="h-3 w-3" /> : <WifiOff className="h-3 w-3" />}
              {connected ? "Live" : "Offline"}
            </div>
            <button
              onClick={toggle}
              className="rounded-full p-1.5 text-white/70 hover:text-white hover:bg-white/10 transition-colors"
              aria-label={theme === "dark" ? "Switch to light mode" : "Switch to dark mode"}
              aria-pressed={theme === "dark"}
              title={theme === "dark" ? "Light mode" : "Dark mode"}
            >
              {theme === "dark" ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
            </button>
            {headerActions}
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-[1200px] px-6 py-8">
        {children}
      </main>
    </div>
  );
}
