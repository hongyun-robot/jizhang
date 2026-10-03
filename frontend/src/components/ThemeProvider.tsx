import { useEffect } from 'react';
import { App as AntApp, ConfigProvider, theme } from 'antd';
import zhCN from 'antd/locale/zh_CN';

import { applyTheme, resolve, useTheme, type ResolvedTheme } from '../stores/theme';

/**
 * 主题壳（任务 12.1 / specs/ui-shell 暗色模式）：
 * - 选择跟随系统时监听 prefers-color-scheme，系统切换即自动跟随；
 * - 生效主题写入 <html data-theme>（CSS 变量）并驱动 AntD darkAlgorithm；
 * - 暗色下底板对齐设计变量（页面 #09090b / 面板 #18181b），主色仍为青绿系，
 *   仅文字级用色提亮为 #2dd4bf 保证对比度（同一色相，不引入第二主色）。
 */
export default function ThemeProvider({ children }: { children?: React.ReactNode }) {
  const mode = useTheme((s) => s.mode);
  const sys = useTheme((s) => s.sys);
  const resolved: ResolvedTheme = resolve(mode, sys);

  // 生效主题落地（data-theme + color-scheme）
  useEffect(() => {
    applyTheme(resolved);
  }, [resolved]);

  // 跟随系统：系统深浅色切换时实时跟进
  useEffect(() => {
    const mq = window.matchMedia('(prefers-color-scheme: dark)');
    const onChange = (e: MediaQueryListEvent) =>
      useTheme.getState().setSys(e.matches ? 'dark' : 'light');
    mq.addEventListener('change', onChange);
    return () => mq.removeEventListener('change', onChange);
  }, []);

  const dark = resolved === 'dark';
  return (
    <ConfigProvider
      locale={zhCN}
      theme={{
        algorithm: dark ? theme.darkAlgorithm : theme.defaultAlgorithm,
        token: {
          colorPrimary: '#0f766e',
          // 链接类按钮/链接统一主色（antd v6 默认蓝色会破坏单主色纪律）
          colorLink: dark ? '#2dd4bf' : '#0f766e',
          colorLinkHover: dark ? '#5eead4' : '#0d9488',
          colorLinkActive: dark ? '#14b8a6' : '#115e59',
          borderRadius: 8,
          fontFamily:
            "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Microsoft YaHei', sans-serif",
          ...(dark
            ? {
                // 与自绘 .panel 同底，避免 AntD 卡片/侧栏/下拉与页面两套灰阶
                colorBgContainer: '#18181b',
                colorBgLayout: '#09090b',
                colorBorderSecondary: 'rgba(244, 244, 245, 0.1)',
              }
            : {}),
        },
      }}
    >
      <AntApp>{children}</AntApp>
    </ConfigProvider>
  );
}
