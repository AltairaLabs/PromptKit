import { useState, useCallback } from "react";

export interface InteractiveOptions {
  agents: Array<{ taskType: string; description: string }>;
  providers: string[];
  hasEvals: boolean;
}

export interface CreateSessionResult {
  sessionId?: string;
  missingVars?: string[];
  error?: string;
}

export function useInteractiveChat() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchOptions = useCallback(async (): Promise<InteractiveOptions> => {
    const resp = await fetch("/api/interactive/options");
    if (!resp.ok) {
      const body = await resp.json().catch(() => ({})) as { error?: string };
      throw new Error(body.error ?? `Failed to load options: ${resp.status}`);
    }
    return resp.json() as Promise<InteractiveOptions>;
  }, []);

  const createSession = useCallback(async (params: {
    agent: string;
    provider: string;
    variables: Record<string, string>;
    evals: boolean;
  }): Promise<CreateSessionResult> => {
    const resp = await fetch("/api/interactive/session", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(params),
    });
    const body = await resp.json() as { sessionId?: string; missingVars?: string[]; error?: string };
    if (!resp.ok) {
      return { error: body.error ?? `Session creation failed: ${resp.status}` };
    }
    return body;
  }, []);

  const sendMessage = useCallback(async (sessionId: string, text: string): Promise<void> => {
    setBusy(true);
    setError(null);
    try {
      const resp = await fetch("/api/interactive/message", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ sessionId, text }),
      });
      if (!resp.ok) {
        const body = await resp.json().catch(() => ({})) as { error?: string };
        setError(body.error ?? `Send failed: ${resp.status}`);
      }
    } finally {
      setBusy(false);
    }
  }, []);

  return { fetchOptions, createSession, sendMessage, busy, error };
}
