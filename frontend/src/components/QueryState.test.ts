import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import QueryState from './QueryState';

describe('query presentation', () => {
  it('shows real initial errors and hides misleading empty/success content', () => {
    const html = renderToStaticMarkup(
      createElement(
        QueryState,
        { error: new Error('读取失败详情'), isEmpty: true, onRetry: () => {} },
        'cached amount'
      )
    );
    expect(html).toContain('读取失败详情');
    expect(html).toMatch(/重\s*试/);
    expect(html).not.toContain('cached amount');
    expect(html).not.toContain('暂无数据');
  });
  it('preserves known content when a background request fails', () => {
    const html = renderToStaticMarkup(
      createElement(
        QueryState,
        { error: new Error('网络错误'), hasData: true },
        '已知余额 100.00'
      )
    );
    expect(html).toContain('已知余额 100.00');
    expect(html).toContain('数据更新失败');
  });
  it('distinguishes successful empty data', () => {
    const html = renderToStaticMarkup(
      createElement(QueryState, { isEmpty: true })
    );
    expect(html).toContain('暂无数据');
    expect(html).not.toContain('失败');
  });
});
