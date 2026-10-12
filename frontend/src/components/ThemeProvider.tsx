import { useEffect } from 'react';
import { App as AntApp, ConfigProvider, theme } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import { UI_FONT, uiTokens } from '../theme/tokens';

import {
  applyTheme,
  resolve,
  useTheme,
  type ResolvedTheme,
} from '../stores/theme';

/**
 * 主题壳（任务 12.1 / specs/ui-shell 暗色模式）：
 * - 选择跟随系统时监听 prefers-color-scheme，系统切换即自动跟随；
 * - 生效主题写入 <html data-theme>（CSS 变量）并驱动 AntD darkAlgorithm；
 * - token 与设计变量同源：中性浅灰底板 / 墨绿主色 / 中性描边，避免 AntD 与自绘两套灰阶。
 */
export default function ThemeProvider({
  children,
}: {
  children?: React.ReactNode;
}) {
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
  const tokens = uiTokens(resolved);
  return (
    <ConfigProvider
      locale={zhCN}
      theme={{
        algorithm: dark ? theme.darkAlgorithm : theme.defaultAlgorithm,
        token: {
          colorPrimary: tokens['--primary-fill'],
          // 链接类按钮/链接统一主色（antd v6 默认蓝色会破坏单主色纪律）
          colorLink: tokens['--primary'],
          colorLinkHover: tokens['--link-hover'],
          colorLinkActive: tokens['--link-active'],
          borderRadius: 9,
          controlHeight: 34,
          fontSize: 14,
          fontFamily: UI_FONT,
          // 底板与自绘变量对齐（暗色不产生第二套灰阶）
          colorBgContainer: tokens['--panel'],
          colorBgLayout: tokens['--bg'],
          colorBorderSecondary: tokens['--line'],
          colorSplit: tokens['--line'],
          colorTextPlaceholder: tokens['--muted'],
        },
      }}
    >
      <AntApp>{children}</AntApp>
    </ConfigProvider>
  );
}
