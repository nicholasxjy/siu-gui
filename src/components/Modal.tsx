import { useEffect, useId, useRef } from "react";
import type { ReactNode } from "react";
import { Icon } from "../icons";
export function Modal({
  title,
  subtitle,
  onClose,
  busy = false,
  large = false,
  children,
  footer,
}: {
  title: string;
  subtitle?: string;
  onClose: () => void;
  busy?: boolean;
  large?: boolean;
  children: ReactNode;
  footer?: ReactNode;
}) {
  const ref = useRef<HTMLDivElement>(null),
    id = useId();
  useEffect(() => {
    const prior = document.activeElement as HTMLElement | null;
    const shell = document.querySelector<HTMLElement>(".app-content");
    if (shell) shell.inert = true;
    ref.current?.focus();
    return () => {
      if (shell) shell.inert = false;
      if (prior?.isConnected) prior.focus();
    };
  }, []);
  useEffect(() => {
    const key = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        if (!busy) onClose();
      } else if (event.key === "Tab") {
        const focusable = [
          ...ref.current!.querySelectorAll<HTMLElement>(
            'button,a,input,textarea,select,[tabindex="0"]',
          ),
        ].filter(
          (x) => !x.hasAttribute("disabled") && x.getClientRects().length,
        );
        const first = focusable[0],
          last = focusable.at(-1);
        if (
          event.shiftKey &&
          (document.activeElement === first ||
            document.activeElement === ref.current)
        ) {
          event.preventDefault();
          last?.focus();
        } else if (!event.shiftKey && document.activeElement === last) {
          event.preventDefault();
          first?.focus();
        }
      }
    };
    document.addEventListener("keydown", key, true);
    return () => document.removeEventListener("keydown", key, true);
  }, [busy, onClose]);
  return (
    <div
      className="overlay"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget && !busy) onClose();
      }}
    >
      <div
        ref={ref}
        role="dialog"
        aria-modal="true"
        aria-labelledby={id}
        tabIndex={-1}
        className={"modal" + (large ? " large-modal" : "")}
      >
        <div className="modal-head">
          <div>
            <h2 id={id}>{title}</h2>
            {subtitle ? <p>{subtitle}</p> : null}
          </div>
          <button
            className="icon-button"
            onClick={onClose}
            disabled={busy}
            aria-label="关闭对话框"
          >
            <Icon name="close" />
          </button>
        </div>
        <div className="modal-body">{children}</div>
        {footer ? <div className="modal-footer">{footer}</div> : null}
      </div>
    </div>
  );
}
export function Note({
  children,
  warning = false,
}: {
  children: ReactNode;
  warning?: boolean;
}) {
  return (
    <div className={"note" + (warning ? " warning-note" : "")}>
      <Icon name={warning ? "warning" : "info"} />
      <div>{children}</div>
    </div>
  );
}
