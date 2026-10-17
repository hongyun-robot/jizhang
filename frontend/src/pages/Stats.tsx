import { uiTokens } from '../theme/tokens';
import ChartDataTable from '../features/statistics/ChartDataTable';
import {
  pieOptions,
  trendOptions,
  comparisonOptions,
  assetOptions,
} from '../features/statistics/options';
import { useMemo, useState } from 'react';
import { Card, Grid, InputNumber, Segmented, Spin } from 'antd';

import dayjs from 'dayjs';

import { useBudgets } from '../api/budgets';
import {
  useStatsAssets,
  useStatsCategory,
  useStatsCompare,
  useStatsPeriods,
  useStatsTrend,
} from '../api/statistics';
import type { DateRange } from '../api/types';
import { useAuth } from '../stores/auth';
import { useResolvedTheme } from '../stores/theme';
import BudgetProgress from '../components/BudgetProgress';
import EChart from '../components/EChart';
import QueryState, { QueryBoundary } from '../components/QueryState';
import Money, { fmtMoney } from '../components/Money';
import PeriodPicker, { presetRange } from '../components/PeriodPicker';
import { sumMoney } from '../lib/money';

function pct(p: number | null): { text: string; cls: string } {
  if (p === null) return { text: '—', cls: 'pct-flat' };
  return {
    text: `${p > 0 ? '+' : ''}${p}%`,
    cls: p >= 0 ? 'pct-up' : 'pct-down',
  };
}

/** 环比幅度着色：goodWhenUp=true 时涨为绿（收入/结余），false 时涨为红（支出）。 */
function cmpPct(p: number | null, goodWhenUp: boolean) {
  const base = pct(p);
  if (p === null || p === 0) return { ...base, cls: 'pct-flat' };
  const good = goodWhenUp ? p > 0 : p < 0;
  return { ...base, cls: good ? 'pct-up' : 'pct-down' };
}

/** 统计页（specs/statistics）：占比/趋势/环比/汇总/资产走势，共用区间选择器。 */
export default function Stats() {
  const screens = Grid.useBreakpoint();
  const isMobile = !screens.md;

  const [range, setRange] = useState<DateRange>(() => presetRange('thisMonth'));
  const [gran, setGran] = useState<'day' | 'week' | 'month'>('day');
  const [shareKind, setShareKind] = useState<'expense' | 'income'>('expense');
  const [shareSelection, setShareSelection] = useState<Record<string, boolean>>({});
  const [year, setYear] = useState<number>(() => dayjs().year());

  const category = useStatsCategory(range);
  const trend = useStatsTrend(range, gran);
  const compare = useStatsCompare(range);
  const periods = useStatsPeriods(year);
  const assets = useStatsAssets(range);
  // 预算按账期口径（非区间口径）：展示当前账期进度
  const budgets = useBudgets();
  const user = useAuth((s) => s.user);

  // 主题相关用色（任务 12.1）：画布不读 CSS 变量，需按生效主题取值
  const dark = useResolvedTheme() === 'dark';
  const tokens = uiTokens(dark ? 'dark' : 'light');
  const TEAL = tokens['--primary'];
  const GREEN = tokens['--income'];
  const RED = tokens['--expense'];
  const PALETTE = tokens['--chart-palette'].split(',');
  const pieBorder = tokens['--panel'];
  const prevBar = tokens['--chart-prev-bar'];

  const share =
    shareKind === 'expense' ? category.data?.expense : category.data?.income;
  // 非正净额默认不绘制，只有可绘制分类被取消选择才显示筛选合计。
  const hasHiddenCategories = share?.items.some(
    (item) => Number(item.amount) > 0 && shareSelection[item.name] === false
  );
  const selectedTotal = sumMoney(
    (share?.items ?? [])
      .filter((item) => shareSelection[item.name] !== false)
      .map((item) => item.amount)
  );
  const chartStyle = {
    dark,
    isMobile,
    TEAL,
    GREEN,
    RED,
    PALETTE,
    pieBorder,
    prevBar,
  };
  const pieOption = useMemo(
    () => pieOptions(share, chartStyle),
    [share, dark, isMobile, shareKind, range.start, range.end]
  );
  const trendOption = useMemo(
    () => trendOptions(trend.data, chartStyle),
    [trend.data, dark, isMobile]
  );
  const compareOption = useMemo(
    () => comparisonOptions(compare.data, chartStyle),
    [compare.data, dark, isMobile]
  );
  const assetOption = useMemo(
    () => assetOptions(assets.data, chartStyle),
    [assets.data, dark, isMobile]
  );

  const cur = compare.data?.current;
  const prev = compare.data?.previous;
  const ch = compare.data?.change;

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <div className="eyebrow">
            {`${range.start.replace(/-/g, '.')} – ${range.end.replace(/-/g, '.')}`}
          </div>
          <h2>统计</h2>
        </div>
      </div>

      {/* 共享区间选择器：与流水列表同一组件、同一区间语义 */}
      <PeriodPicker value={range} onChange={setRange} />

      <div className={`stats-grid${isMobile ? '' : ' two'}`}>
        {/* 1. 分类占比 */}
        <Card
          size="small"
          className="panel"
          title="分类占比"
          extra={
            <Segmented
              size="small"
              value={shareKind}
              options={[
                { value: 'expense', label: '支出' },
                { value: 'income', label: '收入' },
              ]}
              onChange={(v) => setShareKind(v as 'expense' | 'income')}
            />
          }
        >
          {/* 三态统一走 QueryState：请求失败显示错误态 + 重试，不落入「区间内无数据」空态 */}
          <QueryState
            loading={category.isLoading}
            error={category.error}
            hasData={category.data !== undefined}
            isEmpty={!share?.items.length}
            onRetry={() => void category.refetch()}
            emptyDescription="区间内无数据"
            skeleton={<Spin />}
          >
            <>
              <div className="share-total" aria-live="polite" aria-atomic="true">
                <span>合计 <Money value={share?.total} tone={shareKind} /></span>
                {hasHiddenCategories && (
                  <span>
                    当前显示合计 <Money value={selectedTotal} tone={shareKind} />
                  </span>
                )}
              </div>
              <EChart
                option={pieOption}
                height={260}
                ariaLabel={`${shareKind === 'expense' ? '支出' : '收入'}分类占比，合计 ${fmtMoney(share?.total)}${hasHiddenCategories ? `，当前显示合计 ${fmtMoney(selectedTotal)}` : ''}`}
                onLegendSelectionChange={setShareSelection}
              />
              <ChartDataTable
                label="查看数据表"
                head={['分类', '金额']}
                rows={(share?.items ?? []).map((i) => [
                  i.name,
                  fmtMoney(i.amount),
                ])}
              />
            </>
          </QueryState>
        </Card>

        {/* 2. 收支趋势 */}
        <Card
          size="small"
          className="panel"
          title="收支趋势"
          extra={
            <Segmented
              size="small"
              value={gran}
              options={[
                { value: 'day', label: '按天' },
                { value: 'week', label: '按周' },
                { value: 'month', label: '按月' },
              ]}
              onChange={(v) => setGran(v as 'day' | 'week' | 'month')}
            />
          }
        >
          <QueryState
            loading={trend.isLoading}
            error={trend.error}
            hasData={trend.data !== undefined}
            onRetry={() => void trend.refetch()}
            skeleton={<Spin />}
          >
            <EChart option={trendOption} height={260} ariaLabel="收支趋势" />
            <ChartDataTable
              label="查看数据表"
              head={['周期', '收入', '支出', '结余']}
              rows={(trend.data?.points ?? []).map((p) => [
                p.label,
                fmtMoney(p.income),
                fmtMoney(p.expense),
                fmtMoney(p.balance),
              ])}
            />
          </QueryState>
        </Card>

        {/* 3. 本期环比（上一同比窗口） */}
        <Card size="small" className="panel" title="本期环比">
          <QueryState
            loading={compare.isLoading}
            error={compare.error}
            hasData={compare.data !== undefined}
            onRetry={() => void compare.refetch()}
            skeleton={<Spin />}
          >
            <>
              <div className="cmp-meta muted small">
                {compare.data?.start} ~ {compare.data?.end} vs{' '}
                {compare.data?.previous_start} ~ {compare.data?.previous_end}
              </div>
              <EChart
                option={compareOption}
                height={200}
                ariaLabel="本期环比"
              />
              <ChartDataTable
                label="查看数据表"
                head={['指标', '本期', '上期']}
                rows={[
                  ['收入', fmtMoney(cur?.income), fmtMoney(prev?.income)],
                  ['支出', fmtMoney(cur?.expense), fmtMoney(prev?.expense)],
                  ['结余', fmtMoney(cur?.balance), fmtMoney(prev?.balance)],
                ]}
              />
              <div className="cmp-row">
                <span className={cmpPct(ch?.income_percent ?? null, true).cls}>
                  收入 {cmpPct(ch?.income_percent ?? null, true).text}
                </span>
                <span
                  className={cmpPct(ch?.expense_percent ?? null, false).cls}
                >
                  支出 {cmpPct(ch?.expense_percent ?? null, false).text}
                </span>
                <span className={cmpPct(ch?.balance_percent ?? null, true).cls}>
                  结余 {cmpPct(ch?.balance_percent ?? null, true).text}
                </span>
              </div>
              <div className="cmp-values muted small">
                本期 {fmtMoney(cur?.income)}/{fmtMoney(cur?.expense)} · 上期{' '}
                {fmtMoney(prev?.income)}/{fmtMoney(prev?.expense)}
              </div>
            </>
          </QueryState>
        </Card>

        {/* 4. 账期月度汇总 */}
        <Card
          size="small"
          className="panel"
          title="账期汇总"
          extra={
            <InputNumber
              size="small"
              min={2000}
              max={2100}
              value={year}
              onChange={(v) => v && setYear(Number(v))}
              style={{ width: 90 }}
            />
          }
        >
          <QueryState
            loading={periods.isLoading}
            error={periods.error}
            hasData={periods.data !== undefined}
            isEmpty={(periods.data?.periods ?? []).length === 0}
            onRetry={() => void periods.refetch()}
            emptyDescription="该年度暂无账期汇总"
          >
            <div className="period-table">
              <div className="period-head">
                <span>账期</span>
                <span>收入</span>
                <span>支出</span>
                <span>结余</span>
              </div>
              {(periods.data?.periods ?? []).map((p) => (
                <div className="period-line" key={p.period_key}>
                  <span>{p.period_key}</span>
                  <Money value={p.income} tone="income" currency={false} />
                  <Money value={p.expense} tone="expense" currency={false} />
                  <Money
                    value={p.balance}
                    tone={Number(p.balance) < 0 ? 'expense' : 'plain'}
                    currency={false}
                  />
                </div>
              ))}
            </div>
          </QueryState>
        </Card>

        {/* 5. 本期预算进度（与首页同数据源，specs/budgets；宽面板双列铺排，填满整行） */}
        <Card size="small" className="panel stats-wide" title="预算进度">
          <QueryBoundary query={budgets}>
            <BudgetProgress
              view={budgets.data}
              loading={budgets.isLoading}
              showPeriod
              configLink={user?.role === 'admin'}
            />
          </QueryBoundary>
        </Card>

        {/* 6. 资产走势（负债负贡献） */}
        <Card size="small" className="panel stats-wide" title="资产走势">
          <QueryState
            loading={assets.isLoading}
            error={assets.error}
            hasData={assets.data !== undefined}
            isEmpty={(assets.data?.points.length ?? 0) === 0}
            onRetry={() => void assets.refetch()}
            emptyDescription="区间内无数据"
            skeleton={<Spin />}
          >
            <>
              <EChart option={assetOption} height={280} />
              <ChartDataTable
                label="查看数据表"
                head={[
                  '日期',
                  '总资产',
                  ...(assets.data?.points.at(-1)?.accounts ?? []).map(
                    (account) => account.name
                  ),
                ]}
                rows={(assets.data?.points ?? []).map((point) => [
                  point.date,
                  point.total == null ? '报价不完整' : fmtMoney(point.total),
                  ...(assets.data?.points.at(-1)?.accounts ?? []).map(
                    (account) =>
                      point.accounts.find((entry) => entry.account_id === account.account_id)?.balance == null ? '报价不完整' : fmtMoney(point.accounts.find((entry) => entry.account_id === account.account_id)?.balance)
                  ),
                ])}
              />
              <div className="muted small">
                期末总资产 {assets.data?.ending_total == null ? <>估值不完整，已知小计 <Money value={assets.data?.points.at(-1)?.known_subtotal || '0'} /></> : <Money value={assets.data.ending_total} />}
              </div>
            </>
          </QueryState>
        </Card>
      </div>
    </div>
  );
}
