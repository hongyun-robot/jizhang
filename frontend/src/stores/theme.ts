import { create } from 'zustand';

/** 主题选择：手动浅色 / 手动暗色 / 跟随系统（specs/ui-shell 暗色模式）。 */
export type ThemeMode = 'light' | 'dark' | 'system';
export type ResolvedTheme = 'light' | 'dark';

const STORAGE_KEY = 'jizhang-theme';

/** 当前系统配色（prefers-color-scheme）。 */
export function systemTheme(): ResolvedTheme {
  try {
    return window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  } catch {
    return 'light';
  }
}

/** 选择 → 生效主题（system 时看系统）。 */
export function resolve(mode: ThemeMode, sys: ResolvedTheme): ResolvedTheme {
  return mode === 'system' ? sys : mode;
}

/** 把生效主题落到 <html data-theme> 与 color-scheme（供 CSS 变量与原生控件跟随）。 */
export function applyTheme(t: ResolvedTheme): void {
  const el = document.documentElement;
  el.dataset.theme = t;
  el.style.colorScheme = t;
}

function loadMode(): ThemeMode {
  try {
    const v = localStorage.getItem(STORAGE_KEY);
    if (v === 'light' || v === 'dark' || v === 'system') return v;
  } catch {
    // localStorage 不可用（隐私模式等）→ 退化为跟随系统
  }
  return 'system';
}

interface ThemeState {
  /** 用户选择，持久化于 localStorage；默认跟随系统 */
  mode: ThemeMode;
  /** 系统当前配色（mode=system 时生效，随系统切换实时更新） */
  sys: ResolvedTheme;
  setMode: (m: ThemeMode) => void;
  setSys: (t: ResolvedTheme) => void;
}

export const useTheme = create<ThemeState>((set, get) => ({
  mode: loadMode(),
  sys: systemTheme(),
  setMode: (mode) => {
    try {
      localStorage.setItem(STORAGE_KEY, mode);
    } catch {
      // 忽略写入失败：本次会话内仍生效
    }
    set({ mode });
    applyTheme(resolve(mode, get().sys));
  },
  setSys: (sys) => {
    set({ sys });
    applyTheme(resolve(get().mode, sys));
  },
}));

/** 生效主题（组件用）：mode=system 时跟随系统，切换即重渲染。 */
export function useResolvedTheme(): ResolvedTheme {
  return useTheme((s) => resolve(s.mode, s.sys));
}

// 模块加载即落地一次（index.html 内联脚本已提前处理首屏闪烁，这里兜底非 HTML 入口）
applyTheme(resolve(useTheme.getState().mode, useTheme.getState().sys));
