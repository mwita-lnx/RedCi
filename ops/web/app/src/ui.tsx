import { ReactNode, useEffect } from "react";
import clsx from "clsx";

export function Badge({ status }: { status: string }) {
  return <span className={clsx("badge", status)}>{status}</span>;
}

export function Spinner() {
  return <span className="spinner" />;
}

interface BtnProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: "primary" | "secondary" | "danger" | "ghost";
  size?: "sm" | "md";
  loading?: boolean;
}
export function Button({ variant = "primary", size = "md", loading, children, className, disabled, ...rest }: BtnProps) {
  return (
    <button
      className={clsx("btn", variant !== "primary" && variant, size === "sm" && "sm", className)}
      disabled={disabled || loading}
      {...rest}
    >
      {loading ? <Spinner /> : children}
    </button>
  );
}

export function Modal({ title, description, children, onClose }: {
  title: string; description?: string; children: ReactNode; onClose: () => void;
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onClose]);
  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <h3>{title}</h3>
          {description && <p>{description}</p>}
        </div>
        {children}
      </div>
    </div>
  );
}

export function PanelBox({ title, action, children }: { title?: string; action?: ReactNode; children: ReactNode }) {
  return (
    <section className="panel-box">
      {(title || action) && (
        <div className="pb-head">
          {title ? <h2>{title}</h2> : <span />}
          {action}
        </div>
      )}
      <div className="pb-body">{children}</div>
    </section>
  );
}

export function PageHeader({ title, sub, action }: { title: string; sub?: string; action?: ReactNode }) {
  return (
    <header className="page-header">
      <div>
        <h1>{title}</h1>
        {sub && <p className="sub">{sub}</p>}
      </div>
      {action}
    </header>
  );
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="empty">{children}</div>;
}

export function TableSkeleton({ cols = 4, rows = 4 }: { cols?: number; rows?: number }) {
  return (
    <table>
      <tbody>
        {Array.from({ length: rows }).map((_, i) => (
          <tr key={i}>
            {Array.from({ length: cols }).map((_, j) => (
              <td key={j}><div className="skeleton" style={{ height: 14, width: j === 0 ? "60%" : "40%" }} /></td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  );
}
