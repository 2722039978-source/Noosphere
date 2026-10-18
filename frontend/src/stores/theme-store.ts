import { create } from "zustand";

export type Theme = "dark" | "light";

const STORAGE_KEY = "noosphere-theme";

function readInitialTheme(): Theme {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved === "light" || saved === "dark") return saved;
  } catch {
    /* localStorage 不可用时沿用档案界面的浅色主题 */
  }
  return "light";
}

function applyTheme(theme: Theme) {
  document.documentElement.classList.toggle("light", theme === "light");
  document.documentElement.classList.toggle("dark", theme === "dark");
}

interface ThemeState {
  theme: Theme;
  toggle: () => void;
  setTheme: (t: Theme) => void;
}

export const useThemeStore = create<ThemeState>((set, get) => ({
  theme: readInitialTheme(),
  toggle: () => get().setTheme(get().theme === "dark" ? "light" : "dark"),
  setTheme: (t) => {
    try {
      localStorage.setItem(STORAGE_KEY, t);
    } catch {
      /* ignore */
    }
    applyTheme(t);
    set({ theme: t });
  },
}));

// 挂载时立即应用主题 (与 index.html 内联脚本一致, 避免闪烁)
applyTheme(useThemeStore.getState().theme);
