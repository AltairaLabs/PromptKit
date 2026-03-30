import { useState } from "react";
import { cn } from "@/lib/utils";
import { ChevronRight } from "lucide-react";
import type { Message } from "@/types";

interface ConversationThreadProps {
  messages: Message[];
  onSelectMessage?: (index: number, message: Message) => void;
}

const roleStyles: Record<string, { bg: string; border: string; label: string }> = {
  user: { bg: "bg-blue-50", border: "border-l-[#2563EB]", label: "text-[#2563EB]" },
  assistant: { bg: "bg-emerald-50", border: "border-l-[#10B981]", label: "text-[#10B981]" },
  system: { bg: "bg-violet-50", border: "border-l-[#8B5CF6]", label: "text-[#8B5CF6]" },
  tool: { bg: "bg-amber-50", border: "border-l-[#F59E0B]", label: "text-[#F59E0B]" },
};

export function ConversationThread({ messages, onSelectMessage }: ConversationThreadProps) {
  return (
    <div className="space-y-3">
      {messages.map((msg, i) => (
        <MessageBubble key={i} message={msg} index={i} onSelect={onSelectMessage} />
      ))}
    </div>
  );
}

function MessageBubble({
  message,
  index,
  onSelect,
}: {
  message: Message;
  index: number;
  onSelect?: (i: number, msg: Message) => void;
}) {
  const [toolsExpanded, setToolsExpanded] = useState(false);
  const style = roleStyles[message.role] ?? roleStyles.system;

  return (
    <div
      className={cn(
        "rounded-lg border-l-[3px] px-4 py-3 cursor-pointer transition-colors hover:brightness-95",
        style.bg,
        style.border
      )}
      onClick={() => onSelect?.(index, message)}
    >
      <div className="flex items-center justify-between mb-1.5">
        <span className={cn("text-[10px] font-bold uppercase tracking-widest", style.label)}>
          {message.role}
        </span>
        <div className="flex items-center gap-2">
          {message.cost_info && (
            <span className="text-[11px] font-mono text-[#F59E0B]">
              ${message.cost_info.total_cost_usd.toFixed(4)}
            </span>
          )}
          {message.latency_ms != null && (
            <span className="text-[11px] text-slate-muted">{message.latency_ms}ms</span>
          )}
        </div>
      </div>

      <div className="text-sm text-deep-space leading-relaxed whitespace-pre-wrap">
        {message.content}
      </div>

      {message.tool_calls && message.tool_calls.length > 0 && (
        <div className="mt-3">
          <button
            className="flex items-center gap-1 text-xs text-[#2563EB] hover:text-[#1D4ED8]"
            onClick={(e) => {
              e.stopPropagation();
              setToolsExpanded(!toolsExpanded);
            }}
          >
            <ChevronRight
              className={cn("h-3 w-3 transition-transform", toolsExpanded && "rotate-90")}
            />
            {message.tool_calls.length} tool call{message.tool_calls.length > 1 ? "s" : ""}
          </button>
          {toolsExpanded && (
            <div className="mt-2 space-y-2">
              {message.tool_calls.map((tc) => (
                <div key={tc.id} className="rounded-lg bg-white p-3 border border-mist">
                  <div className="flex items-center gap-2 mb-1">
                    <span className="rounded-full bg-violet-100 text-[#8B5CF6] px-2 py-0.5 text-[10px] font-semibold">
                      {tc.name}
                    </span>
                    <span className="text-[10px] text-slate-muted font-mono">{tc.id}</span>
                  </div>
                  <pre className="text-xs font-mono text-deep-space/70 overflow-x-auto">
                    {JSON.stringify(tc.args, null, 2)}
                  </pre>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {message.tool_result && (
        <div className="mt-3 rounded-lg bg-white p-3 border border-mist">
          <div className="flex items-center gap-2 mb-1">
            <span className="rounded-full bg-amber-100 text-[#F59E0B] px-2 py-0.5 text-[10px] font-semibold">
              Result: {message.tool_result.name}
            </span>
            {message.tool_result.error && (
              <span className="rounded-full bg-red-100 text-[#EF4444] px-2 py-0.5 text-[10px] font-semibold">Error</span>
            )}
          </div>
          {message.tool_result.parts?.map((part, j) => (
            <div key={j} className="text-xs text-deep-space/70 whitespace-pre-wrap mt-1">
              {part.text}
            </div>
          ))}
          {message.tool_result.error && (
            <div className="text-xs text-[#EF4444] mt-1">{message.tool_result.error}</div>
          )}
        </div>
      )}

      {message.validations && message.validations.length > 0 && (
        <div className="mt-3 flex flex-wrap gap-1">
          {message.validations.map((v, j) => (
            <span
              key={j}
              className={cn(
                "rounded-full px-2 py-0.5 text-[10px] font-semibold",
                v.passed ? "bg-emerald-100 text-[#10B981]" : "bg-red-100 text-[#EF4444]"
              )}
            >
              {v.passed ? "✓" : "✗"} {v.validator_type}
            </span>
          ))}
        </div>
      )}
    </div>
  );
}
