import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import App from './App';
import ThemeProvider from './components/ThemeProvider';
import './styles.css';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: 1, refetchOnWindowFocus: false },
  },
});

/**
 * 设计基调：中性 zinc 底 + 单一低饱和青绿主色（#0f766e）。
 * 红/绿仅作收支语义色（AntD success/error），不作品牌色。
 * 亮/暗双主题由 ThemeProvider 统一提供（任务 12.1）。
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
