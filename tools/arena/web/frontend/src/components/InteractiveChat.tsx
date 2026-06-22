import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ArrowLeft, Send } from "lucide-react";
import { ConversationThread } from "@/components/ConversationThread";
import { useInteractiveChat } from "@/hooks/useInteractiveChat";
import type { ArenaState, MessageCreatedData, Message } from "@/types";

interface InteractiveChatProps {
  state: ArenaState;
  registerInteractiveRun: (sessionId: string) => void;
  onBack: () => void;
  // Clicking a message opens the shared DevTools panel (same as the run view).
  onSelectMessage?: (index: number, message: Message, allMessages: Message[]) => void;
}

// liveMessageToMessage maps in-flight SSE MessageCreatedData to the Message
// shape ConversationThread renders. Mirrors the same helper in RunDetail.
function liveMessageToMessage(m: MessageCreatedData): Message {
  return {
    role: m.role,
    content: m.content,
    tool_calls: m.toolCalls,
    tool_result: m.toolResult ?? undefined,
  };
}

type Phase = "setup" | "vars" | "chat";

export function InteractiveChat({ state, registerInteractiveRun, onBack, onSelectMessage }: InteractiveChatProps) {
  const { fetchOptions, createSession, sendMessage, busy, error } = useInteractiveChat();

  // Setup phase state
  const [loadingOptions, setLoadingOptions] = useState(true);
  const [optionsError, setOptionsError] = useState<string | null>(null);
  const [agents, setAgents] = useState<Array<{ taskType: string; description: string }>>([]);
  const [providers, setProviders] = useState<string[]>([]);
  const [hasEvals, setHasEvals] = useState(false);

  const [selectedAgent, setSelectedAgent] = useState<string>("");
  const [selectedProvider, setSelectedProvider] = useState<string>("");
  const [enableEvals, setEnableEvals] = useState(false);
  const [sessionCreating, setSessionCreating] = useState(false);
  const [sessionError, setSessionError] = useState<string | null>(null);

  // Vars phase state
  const [missingVars, setMissingVars] = useState<string[]>([]);
  const [varValues, setVarValues] = useState<Record<string, string>>({});
  const [pendingParams, setPendingParams] = useState<{
    agent: string;
    provider: string;
    evals: boolean;
  } | null>(null);

  // Chat phase state
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [inputText, setInputText] = useState("");
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  // Auto-scroll: keep the conversation pinned to the bottom as messages arrive,
  // unless the user has scrolled up to read history.
  const scrollRef = useRef<HTMLDivElement>(null);
  const stickToBottomRef = useRef(true);

  const phase: Phase = sessionId ? "chat" : missingVars.length > 0 ? "vars" : "setup";

  // Load options on mount
  useEffect(() => {
    setLoadingOptions(true);
    fetchOptions()
      .then((opts) => {
        setAgents(opts.agents);
        setProviders(opts.providers);
        setHasEvals(opts.hasEvals);
        if (opts.agents.length === 1) setSelectedAgent(opts.agents[0].taskType);
        if (opts.providers.length === 1) setSelectedProvider(opts.providers[0]);
      })
      .catch((e: Error) => setOptionsError(e.message))
      .finally(() => setLoadingOptions(false));
  }, [fetchOptions]);

  const doCreateSession = useCallback(
    async (agent: string, provider: string, variables: Record<string, string>, evals: boolean) => {
      setSessionCreating(true);
      setSessionError(null);
      try {
        const result = await createSession({ agent, provider, variables, evals });
        if (result.error) {
          setSessionError(result.error);
        } else if (result.missingVars && result.missingVars.length > 0) {
          setMissingVars(result.missingVars);
          setPendingParams({ agent, provider, evals });
          const init: Record<string, string> = {};
          for (const v of result.missingVars) init[v] = "";
          setVarValues(init);
        } else if (result.sessionId) {
          registerInteractiveRun(result.sessionId);
          setSessionId(result.sessionId);
        }
      } finally {
        setSessionCreating(false);
      }
    },
    [createSession, registerInteractiveRun],
  );

  const handleSetupSubmit = useCallback(async () => {
    if (!selectedAgent || !selectedProvider) return;
    await doCreateSession(selectedAgent, selectedProvider, {}, enableEvals);
  }, [selectedAgent, selectedProvider, enableEvals, doCreateSession]);

  const handleVarsSubmit = useCallback(async () => {
    if (!pendingParams) return;
    await doCreateSession(pendingParams.agent, pendingParams.provider, varValues, pendingParams.evals);
  }, [pendingParams, varValues, doCreateSession]);

  const handleSend = useCallback(async () => {
    if (!sessionId || !inputText.trim() || busy) return;
    const text = inputText.trim();
    setInputText("");
    await sendMessage(sessionId, text);
    textareaRef.current?.focus();
  }, [sessionId, inputText, busy, sendMessage]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault();
        void handleSend();
      }
    },
    [handleSend],
  );

  const handleReset = useCallback(() => {
    setSessionId(null);
    setMissingVars([]);
    setPendingParams(null);
    setVarValues({});
    setSessionError(null);
    setInputText("");
  }, []);

  // Messages for the active session, sorted by index (upsert already sorts,
  // but defensive sort here handles any edge case).
  const displayMessages: Message[] = useMemo(() => {
    if (!sessionId) return [];
    const run = state.runs[sessionId];
    if (!run?.messages?.length) return [];
    return [...run.messages]
      .sort((a, b) => (a.index ?? 0) - (b.index ?? 0))
      .map(liveMessageToMessage);
  }, [sessionId, state.runs]);

  // Track whether the user is near the bottom so we only auto-stick when they
  // haven't scrolled up to read history.
  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    stickToBottomRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
  }, []);

  // Pin to the bottom as new messages stream in (when stuck to bottom).
  useEffect(() => {
    const el = scrollRef.current;
    if (el && stickToBottomRef.current) {
      el.scrollTop = el.scrollHeight;
    }
  }, [displayMessages, busy]);

  if (loadingOptions) {
    return (
      <div className="flex items-center justify-center py-24">
        <div className="text-sm text-fg-muted animate-pulse">Loading options…</div>
      </div>
    );
  }

  if (optionsError) {
    return (
      <div className="rounded-xl border border-red-200 bg-red-50 p-6 max-w-lg mx-auto">
        <p className="text-sm text-[#EF4444] mb-4">Failed to load interactive options: {optionsError}</p>
        <button onClick={onBack} className="flex items-center gap-2 text-sm text-[#2563EB] hover:underline">
          <ArrowLeft className="h-4 w-4" /> Back to Runs
        </button>
      </div>
    );
  }

  // --- Setup phase ---
  if (phase === "setup") {
    return (
      <div className="max-w-lg mx-auto">
        <div className="rounded-xl border border-mist bg-surface p-8 shadow-sm">
          <h2 className="text-lg font-semibold text-fg mb-1">Interactive Chat</h2>
          <p className="text-sm text-fg-muted mb-6">Chat live with an agent from your Arena config.</p>

          {sessionError && (
            <div className="mb-4 rounded-lg bg-red-50 border border-red-200 px-4 py-3 text-sm text-[#EF4444]">
              {sessionError}
            </div>
          )}

          <div className="space-y-4">
            {agents.length > 1 && (
              <div>
                <label className="block text-sm font-medium text-fg mb-1">Agent</label>
                <select
                  className="w-full rounded-lg border border-mist bg-canvas px-3 py-2 text-sm text-fg focus:outline-none focus:ring-2 focus:ring-blue-400"
                  value={selectedAgent}
                  onChange={(e) => setSelectedAgent(e.target.value)}
                >
                  <option value="">Select agent…</option>
                  {agents.map((a) => (
                    <option key={a.taskType} value={a.taskType}>
                      {a.taskType}
                    </option>
                  ))}
                </select>
              </div>
            )}

            {providers.length > 1 && (
              <div>
                <label className="block text-sm font-medium text-fg mb-1">Provider</label>
                <select
                  className="w-full rounded-lg border border-mist bg-canvas px-3 py-2 text-sm text-fg focus:outline-none focus:ring-2 focus:ring-blue-400"
                  value={selectedProvider}
                  onChange={(e) => setSelectedProvider(e.target.value)}
                >
                  <option value="">Select provider…</option>
                  {providers.map((p) => (
                    <option key={p} value={p}>
                      {p}
                    </option>
                  ))}
                </select>
              </div>
            )}

            {hasEvals && (
              <div className="flex items-center gap-2">
                <input
                  id="enable-evals"
                  type="checkbox"
                  checked={enableEvals}
                  onChange={(e) => setEnableEvals(e.target.checked)}
                  className="rounded border-mist"
                />
                <label htmlFor="enable-evals" className="text-sm text-fg">
                  Run evals per turn
                </label>
              </div>
            )}

            <button
              className="w-full rounded-lg bg-[#2563EB] px-4 py-2.5 text-sm font-medium text-white hover:bg-blue-700 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
              disabled={!selectedAgent || !selectedProvider || sessionCreating}
              onClick={() => void handleSetupSubmit()}
            >
              {sessionCreating ? "Starting…" : "Start Chat"}
            </button>
          </div>
        </div>
      </div>
    );
  }

  // --- Vars phase ---
  if (phase === "vars") {
    return (
      <div className="max-w-lg mx-auto">
        <div className="rounded-xl border border-mist bg-surface p-8 shadow-sm">
          <h2 className="text-lg font-semibold text-fg mb-1">Required Variables</h2>
          <p className="text-sm text-fg-muted mb-6">
            The selected agent requires values for the following template variables.
          </p>

          {sessionError && (
            <div className="mb-4 rounded-lg bg-red-50 border border-red-200 px-4 py-3 text-sm text-[#EF4444]">
              {sessionError}
            </div>
          )}

          <div className="space-y-4">
            {missingVars.map((v) => (
              <div key={v}>
                <label className="block text-sm font-medium text-fg mb-1">{v}</label>
                <input
                  type="text"
                  className="w-full rounded-lg border border-mist bg-canvas px-3 py-2 text-sm text-fg focus:outline-none focus:ring-2 focus:ring-blue-400"
                  value={varValues[v] ?? ""}
                  onChange={(e) => setVarValues((prev) => ({ ...prev, [v]: e.target.value }))}
                  placeholder={`Enter ${v}…`}
                />
              </div>
            ))}

            <div className="flex gap-3">
              <button
                className="flex-1 rounded-lg border border-mist bg-canvas px-4 py-2.5 text-sm font-medium text-fg-muted hover:text-fg hover:bg-surface transition-colors"
                onClick={handleReset}
              >
                Back
              </button>
              <button
                className="flex-1 rounded-lg bg-[#2563EB] px-4 py-2.5 text-sm font-medium text-white hover:bg-blue-700 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                disabled={missingVars.some((v) => !varValues[v]?.trim()) || sessionCreating}
                onClick={() => void handleVarsSubmit()}
              >
                {sessionCreating ? "Starting…" : "Start Chat"}
              </button>
            </div>
          </div>
        </div>
      </div>
    );
  }

  // --- Chat phase ---
  return (
    <div className="flex flex-col h-[calc(100vh-220px)] min-h-[500px]">
      {/* Sticky header */}
      <div className="sticky top-0 z-10 flex items-center justify-between bg-canvas border-b border-mist px-4 py-3 mb-4">
        <div className="flex items-center gap-3">
          <button
            onClick={handleReset}
            className="flex items-center gap-1.5 text-sm text-fg-muted hover:text-fg transition-colors"
          >
            <ArrowLeft className="h-4 w-4" />
            Reset
          </button>
          <div className="h-4 w-px bg-mist" />
          <div className="flex items-center gap-3 text-xs text-fg-muted">
            <span>
              <span className="font-medium text-fg">{selectedAgent}</span>
            </span>
            <span>·</span>
            <span>{selectedProvider}</span>
            {enableEvals && (
              <>
                <span>·</span>
                <span className="text-[#10B981]">evals on</span>
              </>
            )}
          </div>
        </div>
        <button
          onClick={onBack}
          className="text-xs text-fg-muted hover:text-fg transition-colors"
        >
          ← Runs
        </button>
      </div>

      {/* Conversation thread */}
      <div ref={scrollRef} onScroll={handleScroll} className="flex-1 overflow-y-auto min-h-0">
        {displayMessages.length === 0 ? (
          <div className="flex items-center justify-center h-full">
            <p className="text-sm text-fg-muted">Send a message to start the conversation.</p>
          </div>
        ) : (
          <ConversationThread
            messages={displayMessages}
            streaming={busy}
            onSelectMessage={
              onSelectMessage ? (i, m) => onSelectMessage(i, m, displayMessages) : undefined
            }
          />
        )}
      </div>

      {/* Error banner */}
      {error && (
        <div className="mx-4 mb-2 rounded-lg bg-red-50 border border-red-200 px-4 py-2 text-sm text-[#EF4444]">
          {error}
        </div>
      )}

      {/* Input area */}
      <div className="border-t border-mist bg-surface p-4">
        <div className="flex items-end gap-3">
          <textarea
            ref={textareaRef}
            className="flex-1 resize-none rounded-lg border border-mist bg-canvas px-3 py-2 text-sm text-fg placeholder:text-fg-muted focus:outline-none focus:ring-2 focus:ring-blue-400 min-h-[44px] max-h-40"
            placeholder="Type a message… (Enter to send, Shift+Enter for newline)"
            rows={1}
            value={inputText}
            onChange={(e) => setInputText(e.target.value)}
            onKeyDown={handleKeyDown}
            disabled={busy}
          />
          <button
            className="flex-shrink-0 rounded-lg bg-[#2563EB] p-2.5 text-white hover:bg-blue-700 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            disabled={busy || !inputText.trim()}
            onClick={() => void handleSend()}
            aria-label="Send message"
          >
            <Send className="h-4 w-4" />
          </button>
        </div>
      </div>
    </div>
  );
}
