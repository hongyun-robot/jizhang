import type { ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { Progress, Spin } from 'antd';

import type { BudgetProgress as BudgetProgressDto, BudgetView } from '../api/types';

/**
 * 预算进度（specs/budgets「进度与超支展示」）：
 * - 进度条展示已消耗比例；超支标红并显示超出金额。
 * - 只读场景（首页/统计页）只列已配置的预算；配置页传 showUnset 回填未设置行、
 *   inputFor 注入金额输入控件（同一套行结构，展示与配置不分家）。
 */
export default function BudgetProgress({
  view,
  loading,
  showUnset = false,
  showPeriod = false,
  configLink = false,
  inputFor,
}: {
  view?: BudgetView;
  loading?: boolean;
  showUnset?: boolean;
  showPeriod?: boolean;
  configLink?: boolean;
  inputFor?: (scope: 'total' | number) => ReactNode;
}) {
  if (!view) return loading ? <Spin size="small" /> : null;

  const rows: Array<{ scope: 'total' | number; name: string; p: BudgetProgressDto }> = [];
  if (view.total.budget || showUnset) {
    rows.push({ scope: 'total', name: '总预算', p: view.total });
  }
  for (const c of view.categories) {
    if (c.budget || showUnset) {
      rows.push({ scope: c.category_id, name: c.category_name, p: c });
    }
  }

  if (!rows.length) {
    return (
      <div className="budget-empty muted small">
        预算尚未配置
        {configLink && (
          <>
            {' · '}
            <Link to="/budgets">去配置</Link>
          </>
        )}
      </div>
    );
  }

  return (
    <div className="budget-list" data-period={view.period_key}>
      {showPeriod && <div className="muted small">账期 {view.period_key}</div>}
      {rows.map((r) => (
        <div className="budget-row" key={String(r.scope)} data-budget={r.scope}>
          <span className="budget-name">{r.name}</span>
          {inputFor?.(r.scope)}
          <div className="budget-bar">
            {r.p.budget ? (
              <>
                <Progress percent={r.p.percent} size="small" status={r.p.over ? 'exception' : undefined} />
                <div className="budget-meta">
                  <span>
                    ¥{r.p.consumed} / {r.p.budget}
                  </span>
                  {r.p.over && <span className="budget-over">已超支 ¥{r.p.over_amount}</span>}
                </div>
              </>
            ) : (
              <span className="muted small">未设置</span>
            )}
          </div>
        </div>
      ))}
    </div>
  );
}
