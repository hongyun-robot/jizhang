import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClientProvider } from '@tanstack/react-query';
import { queryClient } from './api/queryClient';

import '@fontsource-variable/instrument-sans';
import '@fontsource/instrument-serif/400.css';

import App from './App';
import ThemeProvider from './components/ThemeProvider';
import './styles.css';


/**
 * 设计基调（纸面账簿）：
 * - 暖白纸底 + 单一墨绿主色（#0d6e66），红/绿只作收支语义，不作品牌色；
 * - Instrument Sans 走拉丁与数字（tabular-nums 对齐），中文回退系统黑体；
 *   Instrument Serif 只用于展示型数字（首页结余、金额输入）；
 * - 亮/暗双主题由 ThemeProvider 统一提供（specs/ui-shell 暗色模式）。
 */
createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <App />
      </ThemeProvider>
    </QueryClientProvider>
  </StrictMode>,
);
