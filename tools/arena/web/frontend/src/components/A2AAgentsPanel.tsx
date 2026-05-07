import type { A2AAgentInfo } from "@/types";

interface A2AAgentsPanelProps {
  agents?: A2AAgentInfo[];
}

// A2AAgentsPanel mirrors the HTML report's A2A agent cards. Each card
// shows the agent name, description, and skill chips with their tags.
export function A2AAgentsPanel({ agents }: A2AAgentsPanelProps) {
  if (!agents || agents.length === 0) return null;
  return (
    <div className="space-y-2">
      <h3 className="text-xs font-semibold text-fg-muted uppercase tracking-wider flex items-center gap-2">
        A2A Agents
        <span className="rounded-full bg-[var(--c-surface-2)] text-fg-muted px-2 py-0.5 text-[10px] font-mono">{agents.length}</span>
      </h3>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
        {agents.map((a, i) => (
          <div key={i} className="rounded-lg bg-surfaceborder border-mist p-3">
            <div className="flex items-center gap-2 mb-1">
              <span className="text-base">🤖</span>
              <span className="text-sm font-semibold text-fg truncate">{a.name}</span>
            </div>
            {a.description && (
              <p className="text-xs text-fg-muted leading-relaxed mb-2">{a.description}</p>
            )}
            {a.skills && a.skills.length > 0 && (
              <div className="space-y-1.5">
                {a.skills.map((s, j) => (
                  <div key={j} className="rounded border border-mist bg-[var(--c-surface-2)] p-2">
                    <div className="flex items-center gap-1.5 mb-0.5">
                      <span className="text-[11px] font-mono font-semibold text-[#8B5CF6]">{s.name}</span>
                      <span className="text-[10px] font-mono text-fg-muted">@{s.id}</span>
                    </div>
                    {s.description && (
                      <p className="text-[11px] text-fg-muted leading-snug">{s.description}</p>
                    )}
                    {s.tags && s.tags.length > 0 && (
                      <div className="flex flex-wrap gap-1 mt-1">
                        {s.tags.map((tag, k) => (
                          <span
                            key={k}
                            className="rounded-full bg-violet-100 text-[#8B5CF6] px-1.5 py-0.5 text-[9px] font-medium"
                          >
                            {tag}
                          </span>
                        ))}
                      </div>
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
