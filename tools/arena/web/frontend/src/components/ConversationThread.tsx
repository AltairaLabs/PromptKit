import { useState } from "react";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { ChevronRight } from "lucide-react";
import type { Message } from "@/types";

interface ConversationThreadProps {
  messages: Message[];
  onSelectMessage?: (index: number) => void;
}

const roleStyles: Record<string, { bg: string; border: string; label: string }> = {
  user: { bg: "bg-altair-blue/10", border: "border-altair-blue", label: "text-altair-blue" },
  assistant: { bg: "bg-deploy-green/10", border: "border-deploy-green", label: "text-deploy-green" },
  system: { bg: "bg-cosmic-violet/10", border: "border-cosmic-violet", label: "text-cosmic-violet" },
  tool: { bg: "bg-stellar-gold/10", border: "border-stellar-gold", label: "text-stellar-gold" },
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
  onSelect?: (i: number) => void;
}) {
  const [toolsExpanded, setToolsExpanded] = useState(false);
  const style = roleStyles[message.role] ?? roleStyles.system;

  return (
    <div
      className={cn(
        "rounded-lg p-4 border-l-2 cursor-pointer transition-colors",
        style.bg,
        style.border,
        "hover:brightness-110"
      )}
      onClick={() => onSelect?.(index)}
    >
      <div className="flex items-center justify-between mb-2">
        <span className={cn("text-xs font-semibold uppercase tracking-wider", style.label)}>
          {message.role}
        </span>
        <div className="flex items-center gap-2">
          {message.cost_info && (
            <span className="text-xs font-mono text-stellar-gold">
              ${message.cost_info.total_cost_usd.toFixed(4)}
            </span>
          )}
          {message.latency_ms != null && (
            <span className="text-xs text-slate-muted">{message.latency_ms}ms</span>
          )}
        </div>
      </div>

      <div className="text-sm text-cloud-white whitespace-pre-wrap">{message.content}</div>

      {message.tool_calls && message.tool_calls.length > 0 && (
        <div className="mt-3">
          <button
            className="flex items-center gap-1 text-xs text-altair-blue hover:text-nebula-cyan"
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
                <div key={tc.id} className="rounded bg-onyx p-3 border border-white/10">
                  <div className="flex items-center gap-2 mb-1">
                    <Badge className="bg-cosmic-violet/20 text-cosmic-violet text-xs">
                      {tc.name}
                    </Badge>
                    <span className="text-xs text-slate-muted font-mono">{tc.id}</span>
                  </div>
                  <pre className="text-xs font-mono text-slate-muted overflow-x-auto">
                    {JSON.stringify(tc.args, null, 2)}
                  </pre>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {message.tool_result && (
        <div className="mt-3 rounded bg-onyx p-3 border border-white/10">
          <div className="flex items-center gap-2 mb-1">
            <Badge className="bg-stellar-gold/20 text-stellar-gold text-xs">
              Result: {message.tool_result.name}
            </Badge>
            {message.tool_result.error && (
              <Badge className="bg-error-red/10 text-error-red text-xs">Error</Badge>
            )}
          </div>
          {message.tool_result.parts?.map((part, j) => (
            <div key={j} className="text-xs text-slate-muted whitespace-pre-wrap mt-1">
              {part.text}
            </div>
          ))}
          {message.tool_result.error && (
            <div className="text-xs text-error-red mt-1">{message.tool_result.error}</div>
          )}
        </div>
      )}

      {message.validations && message.validations.length > 0 && (
        <div className="mt-3 flex flex-wrap gap-1">
          {message.validations.map((v, j) => (
            <Badge
              key={j}
              className={cn(
                "text-xs",
                v.passed
                  ? "bg-deploy-green/10 text-deploy-green"
                  : "bg-error-red/10 text-error-red"
              )}
            >
              {v.passed ? "✓" : "✗"} {v.validator_type}
            </Badge>
          ))}
        </div>
      )}
    </div>
  );
}
