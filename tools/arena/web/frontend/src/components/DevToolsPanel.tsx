import { useState } from "react";
import { cn } from "@/lib/utils";
import { X, Info, BarChart3, Wrench, FileText, Code } from "lucide-react";
import { ScrollArea } from "@/components/ui/scroll-area";
import type { Message, ActiveRun } from "@/types";

interface DevToolsPanelProps {
  message?: Message;
  messageIndex?: number;
  run?: ActiveRun;
  open: boolean;
  onClose: () => void;
}

type Tab = "info" | "metrics" | "tools" | "prompt" | "raw";

const tabs: { id: Tab; label: string; icon: typeof Info }[] = [
  { id: "info", label: "Info", icon: Info },
  { id: "metrics", label: "Metrics", icon: BarChart3 },
  { id: "tools", label: "Tools", icon: Wrench },
  { id: "prompt", label: "Prompt", icon: FileText },
  { id: "raw", label: "Raw", icon: Code },
];

export function DevToolsPanel({ message, messageIndex, run, open, onClose }: DevToolsPanelProps) {
  const [activeTab, setActiveTab] = useState<Tab>("info");

  if (!open) return null;

  return (
    <div className="fixed top-0 right-0 h-screen w-[420px] z-40 flex flex-col border-l border-white/10 bg-[#1e1e2e] shadow-2xl">
      <div className="flex items-center justify-between px-4 py-3 bg-[#181825] border-b border-[#313244]">
        <div>
          <span className="text-sm font-medium text-[#cdd6f4]">DevTools</span>
          {message && (
            <span className="ml-2 text-xs text-[#6c7086]">
              Turn {messageIndex ?? 0} · {message.role}
            </span>
          )}
        </div>
        <button onClick={onClose} className="text-[#6c7086] hover:text-[#cdd6f4]">
          <X className="h-4 w-4" />
        </button>
      </div>

      <div className="flex border-b border-[#313244] bg-[#181825] overflow-x-auto">
        {tabs.map((tab) => (
          <button
            key={tab.id}
            onClick={() => setActiveTab(tab.id)}
            className={cn(
              "flex items-center gap-1.5 px-3 py-2 text-xs whitespace-nowrap transition-colors",
              activeTab === tab.id
                ? "text-[#89b4fa] border-b-2 border-[#89b4fa]"
                : "text-[#6c7086] hover:text-[#cdd6f4]"
            )}
          >
            <tab.icon className="h-3 w-3" />
            {tab.label}
          </button>
        ))}
      </div>

      <ScrollArea className="flex-1">
        <div className="p-4">
          {activeTab === "info" && <InfoTab message={message} run={run} />}
          {activeTab === "metrics" && <MetricsTab message={message} />}
          {activeTab === "tools" && <ToolsTab message={message} />}
          {activeTab === "prompt" && <PromptTab message={message} />}
          {activeTab === "raw" && <RawTab message={message} />}
        </div>
      </ScrollArea>
    </div>
  );
}

function InfoTab({ message, run }: { message?: Message; run?: ActiveRun }) {
  return (
    <div className="space-y-3">
      {run && (
        <>
          <MetricRow label="Scenario" value={run.scenario} />
          <MetricRow label="Provider" value={run.provider} />
          <MetricRow label="Region" value={run.region} />
          <MetricRow label="Status" value={run.status} />
        </>
      )}
      {message && (
        <>
          <MetricRow label="Role" value={message.role} />
          {message.timestamp && (
            <MetricRow
              label="Timestamp"
              value={new Date(message.timestamp).toLocaleTimeString()}
            />
          )}
        </>
      )}
    </div>
  );
}

function MetricsTab({ message }: { message?: Message }) {
  if (!message?.cost_info) {
    return <div className="text-xs text-[#6c7086]">No metrics available</div>;
  }
  const c = message.cost_info;
  return (
    <div className="space-y-3">
      {message.latency_ms != null && (
        <MetricRow label="Latency" value={`${message.latency_ms}ms`} />
      )}
      <MetricRow label="Input Tokens" value={c.input_tokens.toLocaleString()} />
      <MetricRow label="Output Tokens" value={c.output_tokens.toLocaleString()} />
      {c.cached_tokens != null && (
        <MetricRow label="Cached Tokens" value={c.cached_tokens.toLocaleString()} />
      )}
      <MetricRow label="Total Cost" value={`$${c.total_cost_usd.toFixed(6)}`} />
    </div>
  );
}

function ToolsTab({ message }: { message?: Message }) {
  if (!message?.tool_calls?.length) {
    return <div className="text-xs text-[#6c7086]">No tool calls</div>;
  }
  return (
    <div className="space-y-3">
      {message.tool_calls.map((tc) => (
        <div key={tc.id} className="rounded bg-[#181825] border border-[#313244] p-3">
          <div className="text-xs font-medium text-[#89b4fa] mb-1">{tc.name}</div>
          <pre className="text-xs font-mono text-[#cdd6f4] overflow-x-auto whitespace-pre-wrap">
            {JSON.stringify(tc.args, null, 2)}
          </pre>
        </div>
      ))}
    </div>
  );
}

function PromptTab({ message }: { message?: Message }) {
  const systemPrompt = message?.meta?.system_prompt as string | undefined;
  if (!systemPrompt) {
    return <div className="text-xs text-[#6c7086]">No system prompt available</div>;
  }
  return (
    <pre className="text-xs font-mono text-[#cdd6f4] whitespace-pre-wrap">{systemPrompt}</pre>
  );
}

function RawTab({ message }: { message?: Message }) {
  if (!message) {
    return <div className="text-xs text-[#6c7086]">No message selected</div>;
  }
  return (
    <pre className="text-xs font-mono text-[#cdd6f4] overflow-x-auto whitespace-pre-wrap">
      {JSON.stringify(message, null, 2)}
    </pre>
  );
}

function MetricRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between items-center">
      <span className="text-xs text-[#6c7086] uppercase tracking-wider">{label}</span>
      <span className="text-xs font-mono text-[#cdd6f4]">{value}</span>
    </div>
  );
}
