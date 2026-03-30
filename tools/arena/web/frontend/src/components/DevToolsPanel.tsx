import { useState, useMemo } from "react";
import { cn } from "@/lib/utils";
import { X } from "lucide-react";
import type { Message, ActiveRun } from "@/types";

interface DevToolsPanelProps {
  message?: Message;
  messageIndex?: number;
  allMessages?: Message[];
  run?: ActiveRun;
  open: boolean;
  onClose: () => void;
}

type TabId = "info" | "workflow" | "metrics" | "tools" | "prompt" | "selfplay" | "request" | "response" | "trace" | "assertions" | "evals" | "validators" | "raw";

interface TabDef {
  id: TabId;
  label: string;
  icon: string;
  count?: number;
}

function buildTabs(message?: Message, allMessages?: Message[]): TabDef[] {
  const tabs: TabDef[] = [{ id: "info", label: "Info", icon: "ℹ️" }];
  if (!message) return tabs;

  const meta = message.meta || {};

  if (meta._workflow_state) tabs.push({ id: "workflow", label: "Workflow", icon: "⚡" });
  if (message.cost_info) tabs.push({ id: "metrics", label: "Metrics", icon: "📊" });

  const toolCalls = message.tool_calls || [];
  const toolDescs = meta._tool_descriptors as unknown[] | undefined;
  if (toolCalls.length > 0 || toolDescs) {
    tabs.push({ id: "tools", label: "Tools", icon: "🔧", count: toolCalls.length || undefined });
  }

  if (message.role === "system" || meta.system_prompt) tabs.push({ id: "prompt", label: "Prompt", icon: "📝" });
  if (meta._selfplay_prompt) tabs.push({ id: "selfplay", label: "Self-Play", icon: "🤖" });
  if (meta._llm_raw_request) tabs.push({ id: "request", label: "Request", icon: "📡" });
  if (meta._llm_raw_response) tabs.push({ id: "response", label: "Response", icon: "📥" });
  if (meta._llm_trace) tabs.push({ id: "trace", label: "Trace", icon: "🔍" });

  // Show assertions tab if current message or any message in conversation has assertions
  const hasAssertions = meta.assertions || allMessages?.some((m) => (m.meta || {}).assertions);
  if (hasAssertions) {
    tabs.push({ id: "assertions", label: "Assertions", icon: "🛡️" });
  }

  if (meta.pack_evals) tabs.push({ id: "evals", label: "Evals", icon: "🧪" });

  const validations = message.validations || [];
  if (validations.length > 0) tabs.push({ id: "validators", label: "Validators", icon: "✓", count: validations.length });

  tabs.push({ id: "raw", label: "Raw", icon: "{ }" });
  return tabs;
}

export function DevToolsPanel({ message, messageIndex, allMessages, run, open, onClose }: DevToolsPanelProps) {
  const [activeTab, setActiveTab] = useState<TabId>("info");
  const tabs = useMemo(() => buildTabs(message, allMessages), [message, allMessages]);

  // Reset to info if current tab isn't available
  const currentTab = tabs.find((t) => t.id === activeTab) ? activeTab : "info";

  if (!open) return null;

  return (
    <div className="fixed top-0 right-0 h-screen w-[420px] z-40 flex flex-col border-l border-white/10 bg-[#1e1e2e] shadow-2xl">
      <div className="flex items-center justify-between px-4 py-3 bg-[#181825] border-b border-[#313244]">
        <div>
          <span className="text-sm font-medium text-[#cdd6f4]">Details</span>
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
              currentTab === tab.id
                ? "text-[#89b4fa] border-b-2 border-[#89b4fa]"
                : "text-[#6c7086] hover:text-[#cdd6f4]"
            )}
          >
            <span className="text-[10px]">{tab.icon}</span>
            {tab.label}
            {tab.count != null && (
              <span className="ml-1 rounded-full bg-[#313244] px-1.5 py-0 text-[9px] font-mono text-[#6c7086]">
                {tab.count}
              </span>
            )}
          </button>
        ))}
      </div>

      <div className="flex-1 overflow-y-auto min-h-0">
        <div className="p-4">
          {currentTab === "info" && <InfoTab message={message} run={run} />}
          {currentTab === "workflow" && <MetaJsonTab message={message} metaKey="_workflow_state" />}
          {currentTab === "metrics" && <MetricsTab message={message} />}
          {currentTab === "tools" && <ToolsTab message={message} />}
          {currentTab === "prompt" && <PromptTab message={message} />}
          {currentTab === "selfplay" && <MetaTextTab message={message} metaKey="_selfplay_prompt" />}
          {currentTab === "request" && <MetaJsonTab message={message} metaKey="_llm_raw_request" />}
          {currentTab === "response" && <MetaJsonTab message={message} metaKey="_llm_raw_response" />}
          {currentTab === "trace" && <MetaJsonTab message={message} metaKey="_llm_trace" />}
          {currentTab === "assertions" && <AssertionsTab message={message} allMessages={allMessages} />}
          {currentTab === "evals" && <MetaJsonTab message={message} metaKey="pack_evals" />}
          {currentTab === "validators" && <ValidatorsTab message={message} />}
          {currentTab === "raw" && <RawTab message={message} />}
        </div>
      </div>
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
          {message.timestamp && <MetricRow label="Timestamp" value={new Date(message.timestamp).toLocaleTimeString()} />}
          {message.latency_ms != null && <MetricRow label="Latency" value={`${message.latency_ms}ms`} />}
        </>
      )}
    </div>
  );
}

function MetricsTab({ message }: { message?: Message }) {
  if (!message?.cost_info) {
    return <Empty>No metrics available</Empty>;
  }
  const c = message.cost_info;
  return (
    <div className="space-y-3">
      {message.latency_ms != null && <MetricRow label="Latency" value={`${message.latency_ms}ms`} />}
      <MetricRow label="Input Tokens" value={c.input_tokens.toLocaleString()} />
      <MetricRow label="Output Tokens" value={c.output_tokens.toLocaleString()} />
      {c.cached_tokens != null && <MetricRow label="Cached Tokens" value={c.cached_tokens.toLocaleString()} />}
      <MetricRow label="Input Cost" value={`$${c.input_cost_usd.toFixed(6)}`} />
      <MetricRow label="Output Cost" value={`$${c.output_cost_usd.toFixed(6)}`} />
      <MetricRow label="Total Cost" value={`$${c.total_cost_usd.toFixed(6)}`} />
    </div>
  );
}

function ToolsTab({ message }: { message?: Message }) {
  const toolCalls = message?.tool_calls || [];
  const toolDescs = (message?.meta?._tool_descriptors || message?.meta?._available_tools) as unknown[] | undefined;

  if (!toolCalls.length && !toolDescs) {
    return <Empty>No tool data</Empty>;
  }

  return (
    <div className="space-y-4">
      {toolCalls.length > 0 && (
        <Section title="Tool Calls">
          {toolCalls.map((tc) => (
            <div key={tc.id} className="rounded bg-[#181825] border border-[#313244] p-3">
              <div className="flex items-center gap-2 mb-1">
                <span className="text-xs font-medium text-[#89b4fa]">{tc.name}</span>
                <span className="text-[10px] font-mono text-[#6c7086]">{tc.id}</span>
              </div>
              <JsonBlock data={tc.args} />
            </div>
          ))}
        </Section>
      )}
      {toolDescs && (
        <Section title="Available Tools">
          <JsonBlock data={toolDescs} />
        </Section>
      )}
    </div>
  );
}

function PromptTab({ message }: { message?: Message }) {
  const prompt = (message?.meta?.system_prompt as string) || (message?.role === "system" ? message.content : undefined);
  if (!prompt) return <Empty>No system prompt available</Empty>;
  return <pre className="text-xs font-mono text-[#cdd6f4] whitespace-pre-wrap leading-relaxed">{prompt}</pre>;
}

function MetaJsonTab({ message, metaKey }: { message?: Message; metaKey: string }) {
  const data = message?.meta?.[metaKey];
  if (!data) return <Empty>No data</Empty>;
  return <JsonBlock data={data} />;
}

function MetaTextTab({ message, metaKey }: { message?: Message; metaKey: string }) {
  const text = message?.meta?.[metaKey] as string | undefined;
  if (!text) return <Empty>No data</Empty>;
  return <pre className="text-xs font-mono text-[#cdd6f4] whitespace-pre-wrap leading-relaxed">{text}</pre>;
}

function AssertionsTab({ message, allMessages }: { message?: Message; allMessages?: Message[] }) {
  // Collect assertions data — check current message first, then all messages
  const data = message?.meta?.assertions as Record<string, unknown> | undefined;
  const aData = data || (() => {
    for (const m of allMessages || []) {
      const a = (m.meta || {}).assertions;
      if (a) return a as Record<string, unknown>;
    }
    return undefined;
  })();
  if (!aData) return <Empty>No assertions</Empty>;

  // The assertions object has: passed (bool), total (int), failed (int), results (array)
  const passed = aData.passed as boolean;
  const total = (aData.total as number) || 0;
  const failed = (aData.failed as number) || 0;
  const results = (aData.results as Record<string, unknown>[]) || [];

  return (
    <div className="space-y-3">
      {/* Summary bar */}
      <div className="flex items-center gap-2">
        <span className="text-base">{passed ? "✅" : "❌"}</span>
        <span className={cn("text-xs font-semibold", passed ? "text-[#a6e3a1]" : "text-[#f38ba8]")}>
          {passed ? "All Passed" : `${failed} of ${total} Failed`}
        </span>
      </div>
      {/* Individual assertions */}
      {results.map((ar, i) => {
        const arPassed = ar.passed === true;
        const arSkipped = ar.skipped === true;
        const color = arSkipped ? "#6c7086" : arPassed ? "#a6e3a1" : "#f38ba8";
        const icon = arSkipped ? "⏭" : arPassed ? "✓" : "✗";
        const label = arSkipped ? "Skipped" : arPassed ? "Passed" : "Failed";
        const bg = arSkipped ? "#1e1e2e" : arPassed ? "#1a2e1a" : "#2e1a1a";
        const border = arSkipped ? "#45475a" : arPassed ? "#2d4a3e" : "#4a2d2d";
        const details = ar.details as Record<string, unknown> | undefined;
        const config = ar.config as Record<string, unknown> | undefined;
        return (
          <div key={i} className="rounded-md p-3" style={{ background: bg, border: `1px solid ${border}` }}>
            <div className="flex items-center justify-between mb-1">
              <span className="text-xs font-semibold font-mono text-[#89b4fa]">{String(ar.type || "")}</span>
              <span className="text-[10px] font-semibold" style={{ color }}>{icon} {label}</span>
            </div>
            {ar.message ? <div className="text-xs text-[#cdd6f4] mb-1">{String(ar.message)}</div> : null}
            {config?.params ? <CollapsibleSection title="Config"><JsonBlock data={config.params} /></CollapsibleSection> : null}
            {details?.error ? <div className="text-xs text-[#f38ba8]">{String(details.error)}</div> : null}
            {details?.skip_reason ? <div className="text-xs text-[#6c7086] italic">Skipped: {String(details.skip_reason)}</div> : null}
            {details?.explanation && !arPassed ? <div className="text-xs text-[#fab387]">{String(details.explanation)}</div> : null}
            {details && <CollapsibleSection title="Results"><JsonBlock data={details} /></CollapsibleSection>}
          </div>
        );
      })}
    </div>
  );
}

function ValidatorsTab({ message }: { message?: Message }) {
  const validations = message?.validations || [];
  if (!validations.length) return <Empty>No validations</Empty>;

  const failed = validations.filter((v) => !v.passed).length;
  const allPassed = failed === 0;

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <span className="text-base">{allPassed ? "✅" : "❌"}</span>
        <span className={cn("text-xs font-semibold", allPassed ? "text-[#a6e3a1]" : "text-[#f38ba8]")}>
          {allPassed ? "All Passed" : `${failed} of ${validations.length} Failed`}
        </span>
      </div>
      {validations.map((v, i) => {
        const bg = v.passed ? "#1a2e1a" : "#2e1a1a";
        const border = v.passed ? "#2d4a3e" : "#4a2d2d";
        return (
          <div key={i} className="rounded-md p-3" style={{ background: bg, border: `1px solid ${border}` }}>
            <div className="flex items-center justify-between mb-1">
              <span className="text-xs font-semibold font-mono text-[#89b4fa]">{v.validator_type}</span>
              <span className={cn("text-[10px] font-semibold", v.passed ? "text-[#a6e3a1]" : "text-[#f38ba8]")}>
                {v.passed ? "✓ Passed" : "✗ Failed"}
              </span>
            </div>
            {v.details && <CollapsibleSection title="Details"><JsonBlock data={v.details} /></CollapsibleSection>}
          </div>
        );
      })}
    </div>
  );
}

function CollapsibleSection({ title, children }: { title: string; children: React.ReactNode }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="mt-1">
      <button onClick={() => setOpen(!open)} className="text-[10px] text-[#6c7086] hover:text-[#cdd6f4]">
        {title} {open ? "▾" : "▸"}
      </button>
      {open && <div className="mt-1">{children}</div>}
    </div>
  );
}

function RawTab({ message }: { message?: Message }) {
  if (!message) return <Empty>No message selected</Empty>;
  return <JsonBlock data={message} />;
}

function JsonBlock({ data }: { data: unknown }) {
  return (
    <pre className="text-xs font-mono text-[#cdd6f4] leading-relaxed whitespace-pre-wrap overflow-x-auto">
      <JsonNode value={data} depth={0} />
    </pre>
  );
}

function JsonNode({ value, depth }: { value: unknown; depth: number }) {
  if (value === null) return <span style={{ color: "#6c7086" }}>null</span>;
  if (typeof value === "boolean") return <span style={{ color: "#fab387" }}>{String(value)}</span>;
  if (typeof value === "number") return <span style={{ color: "#f9e2af" }}>{value}</span>;
  if (typeof value === "string") {
    if (value.length > 200) {
      return <CollapsibleString value={value} />;
    }
    return <span style={{ color: "#a6e3a1" }}>"{value}"</span>;
  }
  if (Array.isArray(value)) {
    if (value.length === 0) return <span style={{ color: "#cdd6f4" }}>[]</span>;
    return <CollapsibleArray items={value} depth={depth} />;
  }
  if (typeof value === "object") {
    const entries = Object.entries(value as Record<string, unknown>);
    if (entries.length === 0) return <span style={{ color: "#cdd6f4" }}>{"{}"}</span>;
    return <CollapsibleObject entries={entries} depth={depth} />;
  }
  return <span style={{ color: "#cdd6f4" }}>{String(value)}</span>;
}

function CollapsibleString({ value }: { value: string }) {
  const [expanded, setExpanded] = useState(false);
  const preview = value.slice(0, 80);
  return (
    <span>
      <span style={{ color: "#a6e3a1" }}>
        "{expanded ? value : preview}
        {!expanded && "…"}"
      </span>
      <button onClick={() => setExpanded(!expanded)} className="ml-1 text-[#89b4fa] hover:underline">
        {expanded ? "less" : `+${value.length - 80}`}
      </button>
    </span>
  );
}

function CollapsibleObject({ entries, depth }: { entries: [string, unknown][]; depth: number }) {
  const [expanded, setExpanded] = useState(depth < 2);
  const indent = "  ".repeat(depth + 1);
  const closingIndent = "  ".repeat(depth);

  if (!expanded) {
    return (
      <span>
        <button onClick={() => setExpanded(true)} className="text-[#6c7086] hover:text-[#cdd6f4]">
          {"{"}<span className="text-[10px] mx-1">…{entries.length} keys</span>{"}"}
        </button>
      </span>
    );
  }

  return (
    <span>
      <button onClick={() => setExpanded(false)} className="text-[#6c7086] hover:text-[#cdd6f4]">{"{"}</button>
      {"\n"}
      {entries.map(([key, val], i) => (
        <span key={key}>
          {indent}<span style={{ color: "#89b4fa" }}>"{key}"</span>: <JsonNode value={val} depth={depth + 1} />
          {i < entries.length - 1 ? "," : ""}{"\n"}
        </span>
      ))}
      {closingIndent}<button onClick={() => setExpanded(false)} className="text-[#6c7086] hover:text-[#cdd6f4]">{"}"}</button>
    </span>
  );
}

function CollapsibleArray({ items, depth }: { items: unknown[]; depth: number }) {
  const [expanded, setExpanded] = useState(depth < 2);
  const indent = "  ".repeat(depth + 1);
  const closingIndent = "  ".repeat(depth);

  if (!expanded) {
    return (
      <span>
        <button onClick={() => setExpanded(true)} className="text-[#6c7086] hover:text-[#cdd6f4]">
          [<span className="text-[10px] mx-1">…{items.length} items</span>]
        </button>
      </span>
    );
  }

  return (
    <span>
      <button onClick={() => setExpanded(false)} className="text-[#6c7086] hover:text-[#cdd6f4]">[</button>
      {"\n"}
      {items.map((item, i) => (
        <span key={i}>
          {indent}<JsonNode value={item} depth={depth + 1} />
          {i < items.length - 1 ? "," : ""}{"\n"}
        </span>
      ))}
      {closingIndent}<button onClick={() => setExpanded(false)} className="text-[#6c7086] hover:text-[#cdd6f4]">]</button>
    </span>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div>
      <div className="text-[10px] font-semibold text-[#6c7086] uppercase tracking-widest mb-2 pb-1 border-b border-[#313244]">
        {title}
      </div>
      <div className="space-y-2">{children}</div>
    </div>
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

function Empty({ children }: { children: React.ReactNode }) {
  return <div className="text-xs text-[#6c7086]">{children}</div>;
}
