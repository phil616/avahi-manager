import { useEffect, useState } from "react";
import { api } from "./api";

// Discard late responses and cancel reads on navigation. Never abort a mutation here.
export function useResource<T>(path: string, revision?: string | number) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError("");
    api<T>(path, "GET", undefined, controller.signal)
      .then((value) => {
        if (!controller.signal.aborted) setData(value);
      })
      .catch((err) => {
        if (!controller.signal.aborted) setError(String(err));
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [path, revision, attempt]);
  return { data, error, loading, retry: () => setAttempt((n) => n + 1) };
}
