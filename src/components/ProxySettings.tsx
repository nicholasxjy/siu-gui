import { useEffect, useReducer, useRef, useState } from "react";
import { api, errorMessage } from "../api";
import { Icon } from "../icons";
import type { ProxyMode, ProxySettings } from "../types";

const DEFAULT_SETTINGS: ProxySettings = {
  mode: "system",
  url: "",
  noProxy: "",
};

type State =
  | { phase: "loading" }
  | { phase: "error"; message: string }
  | {
      phase: "idle" | "saving";
      saved: ProxySettings | null;
      draft: ProxySettings;
    };

type Action =
  | { type: "loaded"; settings: ProxySettings }
  | { type: "load-failed"; message: string }
  | { type: "set-mode"; mode: ProxyMode }
  | { type: "set-url"; url: string }
  | { type: "set-no-proxy"; noProxy: string }
  | { type: "saving" }
  | { type: "saved"; settings: ProxySettings }
  | { type: "save-failed" }
  | { type: "retry" }
  | { type: "use-defaults" };

function reducer(state: State, action: Action): State {
  switch (action.type) {
    case "loaded":
      return { phase: "idle", saved: action.settings, draft: action.settings };
    case "load-failed":
      return { phase: "error", message: action.message };
    case "use-defaults":
      return { phase: "idle", saved: null, draft: DEFAULT_SETTINGS };
    case "retry":
      return { phase: "loading" };
    case "set-mode":
      if (state.phase !== "idle" && state.phase !== "saving") return state;
      return { ...state, draft: { ...state.draft, mode: action.mode } };
    case "set-url":
      if (state.phase !== "idle" && state.phase !== "saving") return state;
      return { ...state, draft: { ...state.draft, url: action.url } };
    case "set-no-proxy":
      if (state.phase !== "idle" && state.phase !== "saving") return state;
      return { ...state, draft: { ...state.draft, noProxy: action.noProxy } };
    case "saving":
      if (state.phase !== "idle") return state;
      return { ...state, phase: "saving" };
    case "saved":
      if (state.phase !== "saving") return state;
      return { phase: "idle", saved: action.settings, draft: action.settings };
    case "save-failed":
      if (state.phase !== "saving") return state;
      return { ...state, phase: "idle" };
  }
}

export function ProxySettingsForm({
  busy,
  notify,
}: {
  busy: string;
  notify: (message: string) => void;
}) {
  const [state, dispatch] = useReducer(reducer, { phase: "loading" });
  const [showUrl, setShowUrl] = useState(false);
  const [saveError, setSaveError] = useState("");
  // retryCount increments on retry to re-trigger the load effect
  const [retryCount, setRetryCount] = useReducer((n: number) => n + 1, 0);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  // Load on mount and on retry
  useEffect(() => {
    let cancelled = false;
    dispatch({ type: "retry" });
    setSaveError("");
    api
      .request<ProxySettings>("proxy_settings")
      .then((settings) => {
        if (cancelled) return;
        const normalized: ProxySettings = {
          mode: settings?.mode ?? DEFAULT_SETTINGS.mode,
          url: settings?.url ?? DEFAULT_SETTINGS.url,
          noProxy: settings?.noProxy ?? DEFAULT_SETTINGS.noProxy,
        };
        dispatch({ type: "loaded", settings: normalized });
      })
      .catch((e) => {
        if (cancelled) return;
        dispatch({ type: "load-failed", message: errorMessage(e) });
      });
    return () => {
      cancelled = true;
    };
  }, [retryCount]);

  const loading = state.phase === "loading";
  const loadError = state.phase === "error";
  const saving = state.phase === "saving";
  const disabled = Boolean(busy) || loading || saving || loadError;

  const draft =
    state.phase === "idle" || state.phase === "saving"
      ? state.draft
      : undefined;
  const saved =
    state.phase === "idle" || state.phase === "saving"
      ? state.saved
      : undefined;

  const isDirty = draft
    ? !saved ||
      draft.mode !== saved.mode ||
      draft.url !== saved.url ||
      draft.noProxy !== saved.noProxy
    : false;

  const handleSave = async () => {
    if (!draft || state.phase !== "idle") return;
    if (draft.mode === "custom" && !draft.url.trim()) {
      setSaveError("手动配置模式下，代理地址不能为空。");
      return;
    }
    const settings = draft;
    setSaveError("");
    dispatch({ type: "saving" });
    try {
      const result = await api.request<ProxySettings>("set_proxy_settings", {
        proxy: settings,
      });
      if (!mountedRef.current) return;
      const normalized: ProxySettings = {
        mode: result?.mode ?? DEFAULT_SETTINGS.mode,
        url: result?.url ?? DEFAULT_SETTINGS.url,
        noProxy: result?.noProxy ?? DEFAULT_SETTINGS.noProxy,
      };
      dispatch({ type: "saved", settings: normalized });
      notify("代理设置已保存");
    } catch (e) {
      if (!mountedRef.current) return;
      dispatch({ type: "save-failed" });
      setSaveError(errorMessage(e));
    }
  };

  return (
    <section className="settings-section">
      <h2>网络</h2>
      {loading && (
        <div className="inline-loading">
          <Icon name="spinner" />
          <span>读取代理设置…</span>
        </div>
      )}
      {loadError && state.phase === "error" && (
        <div className="proxy-load-error">
          <p className="error-text">{state.message}</p>
          <button
            className="btn small"
            onClick={() => setRetryCount()}
            disabled={Boolean(busy)}
          >
            <Icon name="refresh" />
            重新读取
          </button>
          <button
            className="btn small"
            onClick={() => dispatch({ type: "use-defaults" })}
            disabled={Boolean(busy)}
          >
            使用系统默认值
          </button>
        </div>
      )}
      {(state.phase === "idle" || state.phase === "saving") && draft && (
        <>
          <div className="setting-row">
            <div>
              <h3>下载代理</h3>
              <p>
                作用于 GitHub Skills、资源发现和 MCP 版本查询。
                <br />
                Docker 镜像由 Docker 引擎下载，需在 Docker 中配置代理。
              </p>
            </div>
            <div className="proxy-mode-select">
              <select
                aria-label="代理模式"
                value={draft.mode}
                disabled={disabled}
                onChange={(e) =>
                  dispatch({
                    type: "set-mode",
                    mode: e.currentTarget.value as ProxyMode,
                  })
                }
              >
                <option value="system">跟随系统</option>
                <option value="custom">手动配置</option>
                <option value="direct">不使用代理</option>
              </select>
            </div>
          </div>

          {draft.mode === "system" && (
            <div className="setting-row proxy-hint-row">
              <p className="quiet proxy-hint">
                自动读取系统代理设置，并遵循 HTTP_PROXY、HTTPS_PROXY、 ALL_PROXY
                和 NO_PROXY 环境变量。
              </p>
            </div>
          )}

          {draft.mode === "custom" && (
            <>
              <div className="setting-row proxy-field-row">
                <div className="field proxy-url-field">
                  <label className="field-label" htmlFor="proxy-url">
                    代理地址
                  </label>
                  <div className="secret-input">
                    <input
                      id="proxy-url"
                      type={showUrl ? "text" : "password"}
                      value={draft.url}
                      disabled={disabled}
                      placeholder="http://127.0.0.1:7890"
                      autoComplete="off"
                      spellCheck={false}
                      onChange={(e) =>
                        dispatch({
                          type: "set-url",
                          url: e.currentTarget.value,
                        })
                      }
                    />
                    <button
                      type="button"
                      className="icon-button"
                      aria-label={showUrl ? "隐藏代理地址" : "显示代理地址"}
                      onClick={() => setShowUrl((v) => !v)}
                    >
                      <Icon name={showUrl ? "eyeOff" : "eye"} />
                    </button>
                  </div>
                  <p className="quiet proxy-hint">
                    支持 http://、https://、socks5:// 和 socks5h://，
                    可包含认证信息（user:pass@host:port）。
                  </p>
                </div>
              </div>
              <div className="setting-row proxy-field-row">
                <div className="field proxy-no-proxy-field">
                  <label className="field-label" htmlFor="proxy-no-proxy">
                    绕过代理
                  </label>
                  <input
                    id="proxy-no-proxy"
                    type="text"
                    value={draft.noProxy}
                    disabled={disabled}
                    placeholder="localhost,127.0.0.1,*.local"
                    autoComplete="off"
                    spellCheck={false}
                    onChange={(e) =>
                      dispatch({
                        type: "set-no-proxy",
                        noProxy: e.currentTarget.value,
                      })
                    }
                  />
                  <p className="quiet proxy-hint">
                    逗号分隔的域名、IP 地址或 CIDR。
                  </p>
                </div>
              </div>
            </>
          )}

          {saveError && (
            <div role="alert" className="proxy-save-error error-text">
              {saveError}
            </div>
          )}

          <div className="setting-row proxy-save-row">
            <div />
            <button
              className="btn small"
              disabled={disabled || !isDirty}
              onClick={() => void handleSave()}
            >
              {saving ? <Icon name="spinner" /> : <Icon name="check" />}
              保存
            </button>
          </div>
        </>
      )}
    </section>
  );
}
