import { useCallback, useEffect, useRef, useState } from "react";
import { api, errorMessage } from "./api";
import { APP_VERSION } from "./version";
import type { AppInfo, MutationResult, Snapshot, Theme } from "./types";
export function useApplication() {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null),
    [loading, setLoading] = useState(true),
    [busy, setBusy] = useState(""),
    [alert, setAlert] = useState<string[]>([]),
    [toast, setToast] = useState(""),
    [info, setInfo] = useState<AppInfo>({
      os: "",
      arch: "",
      version: APP_VERSION,
    });
  const [theme, setThemeState] = useState<Theme>(() => {
    try {
      const t = localStorage.getItem("siu.theme.v1");
      return t === "light" || t === "dark" ? t : "system";
    } catch {
      return "system";
    }
  });
  const busyRef = useRef(false),
    generation = useRef(0),
    mounted = useRef(true);
  const notify = useCallback((text: string) => setToast(text), []);
  const refresh = useCallback(async (force = false) => {
    if (busyRef.current && !force) return;
    const id = ++generation.current;
    try {
      const value = await api.request<Snapshot>("snapshot");
      if (mounted.current && id === generation.current) {
        setSnapshot(value);
      }
    } catch (e) {
      if (mounted.current && id === generation.current)
        setAlert((previous) => [...(force ? previous : []), errorMessage(e)]);
    } finally {
      if (mounted.current) setLoading(false);
    }
  }, []);
  useEffect(() => {
    mounted.current = true;
    void refresh();
    void api
      .info()
      .then((v) => {
        if (mounted.current) setInfo(v);
      })
      .catch(() => {});
    const focus = () => void refresh();
    window.addEventListener("focus", focus);
    return () => {
      mounted.current = false;
      window.removeEventListener("focus", focus);
    };
  }, [refresh]);
  useEffect(() => {
    if (!toast) return;
    const t = setTimeout(() => setToast(""), 4500);
    return () => clearTimeout(t);
  }, [toast]);
  useEffect(() => {
    const media = matchMedia("(prefers-color-scheme: dark)");
    const apply = () => {
      const value =
        theme === "system" ? (media.matches ? "dark" : "light") : theme;
      document.documentElement.dataset.theme = value;
      document.documentElement.style.colorScheme = value;
    };
    apply();
    media.addEventListener("change", apply);
    try {
      localStorage.setItem("siu.theme.v1", theme);
    } catch {
      /* Read-only web storage does not prevent use. */
    }
    void api.setTheme(theme).catch((e) => setAlert([errorMessage(e)]));
    return () => media.removeEventListener("change", apply);
  }, [theme]);
  const mutate = useCallback(
    async (
      method: string,
      params: Record<string, unknown>,
      label: string,
    ): Promise<MutationResult> => {
      if (busyRef.current) throw new Error("上一项操作尚未完成，请稍后再试。");
      busyRef.current = true;
      ++generation.current;
      setBusy(label);
      setAlert([]);
      setToast("");
      try {
        const result = await api.request<MutationResult>(method, params);
        if (mounted.current) {
          setSnapshot(result.snapshot);
          if (result.report.issues.length) setAlert(result.report.issues);
          else notify(label + "完成");
        }
        return result;
      } catch (e) {
        if (mounted.current) {
          setAlert([errorMessage(e)]);
          // Report the failed operation immediately; an unresponsive
          // recovery read must not keep the update dialog spinning.
          void refresh(true);
        }
        throw e;
      } finally {
        busyRef.current = false;
        if (mounted.current) setBusy("");
      }
    },
    [notify, refresh],
  );
  return {
    snapshot,
    loading,
    busy,
    alert,
    toast,
    info,
    theme,
    setTheme: setThemeState,
    refresh,
    mutate,
    notify,
    clearAlert: () => setAlert([]),
  };
}
