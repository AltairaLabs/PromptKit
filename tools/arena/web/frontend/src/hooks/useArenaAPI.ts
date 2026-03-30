import { useState, useCallback } from "react";
import type { RunResult, RunRequest } from "@/types";

export function useArenaAPI() {
  const [loading, setLoading] = useState(false);

  const startRun = useCallback(async (req?: RunRequest) => {
    setLoading(true);
    try {
      const resp = await fetch("/api/run", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(req || {}),
      });
      return resp.json();
    } finally {
      setLoading(false);
    }
  }, []);

  const getResults = useCallback(async (): Promise<string[]> => {
    const resp = await fetch("/api/results");
    return resp.json();
  }, []);

  const getResult = useCallback(async (id: string): Promise<RunResult> => {
    const resp = await fetch(`/api/results/${encodeURIComponent(id)}`);
    return resp.json();
  }, []);

  const getConfig = useCallback(async () => {
    const resp = await fetch("/api/config");
    return resp.json();
  }, []);

  const clearResults = useCallback(async () => {
    const resp = await fetch("/api/results", { method: "DELETE" });
    return resp.json();
  }, []);

  return { startRun, getResults, getResult, getConfig, clearResults, loading };
}
