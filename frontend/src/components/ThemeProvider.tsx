import { useEffect } from 'react';
import { App as AntApp, ConfigProvider, theme } from 'antd';
import zhCN from 'antd/locale/zh_CN';

import { applyTheme, resolve, useTheme, type ResolvedTheme } from '../stores/theme';

const UI_FONT =
  "'Instrument Sans Variable', -apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', 'Noto Sans SC', sans-serif";

/**
 * 主题壳（任务 12.1 / specs/ui-shell 暗色模式）：
 * - 选择跟随系统时监听 prefers-color-scheme，系统切换即自动跟随；
 * - 生效主题写入 <html data-theme>（CSS 变量）并驱动 AntD darkAlgorithm；
 * - token 与设计变量同源：纸面底板 / 墨绿主色 / 暖灰描边，避免 AntD 与自绘两套灰阶。
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
          colorPrimary: dark ? '#14b8a6' : '#0d6e66',
          // 链接类按钮/链接统一主色（antd v6 默认蓝色会破坏单主色纪律）
          colorLink: dark ? '#2dd4bf' : '#0d6e66',
          colorLinkHover: dark ? '#5eead4' : '#0b5f58',
          colorLinkActive: dark ? '#99f6e4' : '#095049',
          borderRadius: 9,
          controlHeight: 34,
          fontSize: 14,
          fontFamily: UI_FONT,
          // 底板与自绘变量对齐（暗色不产生第二套灰阶）
          colorBgContainer: dark ? '#141716' : '#fffefa',
          colorBgLayout: dark ? '#0b0d0c' : '#f2f0ea',
          colorBorderSecondary: dark ? 'rgba(236,234,227,0.1)' : 'rgba(38,35,28,0.1)',
          colorSplit: dark ? 'rgba(236,234,227,0.1)' : 'rgba(38,35,28,0.1)',
          colorTextPlaceholder: dark ? '#918e85' : '#7b766a',
        },
      }}
    >
      <AntApp>{children}</AntApp>
    </ConfigProvider>
  );
}
