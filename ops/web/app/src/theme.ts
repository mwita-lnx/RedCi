import { useSyncExternalStore } from "react";

// Theme: manual toggle, default dark, remembered in localStorage.
export type Theme = "dark" | "light";
const KEY = "redci-theme";

export function initTheme() {
  const t = (localStorage.getItem(KEY) as Theme) || "dark";
  apply(t);
}

function apply(t: Theme) {
  document.documentElement.setAttribute("data-theme", t);
}

const listeners = new Set<() => void>();
function emit() { listeners.forEach((l) => l()); }

export function getTheme(): Theme {
  return (document.documentElement.getAttribute("data-theme") as Theme) || "dark";
}

export function toggleTheme() {
  const next: Theme = getTheme() === "dark" ? "light" : "dark";
  localStorage.setItem(KEY, next);
  apply(next);
  emit();
}

// Hook so components re-render on theme change.
export function useTheme(): Theme {
  return useSyncExternalStore(
    (cb) => { listeners.add(cb); return () => listeners.delete(cb); },
    getTheme,
    () => "dark"
  );
}
