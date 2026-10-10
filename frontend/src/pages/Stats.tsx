import { useMemo, useState } from 'react';
import { Card, Empty, Grid, InputNumber, Segmented, Spin } from 'antd';
import type { EChartsOption, SeriesOption } from 'echarts';
import dayjs from 'dayjs';

import {
  useBudgets,
  useStatsAssets,
  useStatsCategory,
  useStatsCompare,
  useStatsPeriods,
  useStatsTrend,
} from '../api/hooks';
import type { DateRange } from '../api/types';
import { useAuth } from '../stores/auth';
import { useResolvedTheme } from '../stores/theme';
import BudgetProgress from '../components/BudgetProgress';
import EChart from '../components/EChart';
import Money, { fmtMoney } from '../components/Money';
import PeriodPicker, { presetRange } from '../components/PeriodPicker';

/** 图表配色：单一墨绿主色 + 中性灰，红/绿仅作收支语义；暗色为同色相提亮版。 */
const PALETTE_LIGHT = ['#0a7568', '#065448', '#12958a', '#45b3a8', '#8ed0c9', '#8a9096', '#b5babf', '#dcdfe2'];
const PALETTE_DARK = ['#2dd4bf', '#14b8a6', '#0d9488', '#5eead4', '#99f6e4', '#a8a29e', '#d6d3d1', '#78716c'];

function pct(p: number | null): { text: string; cls: string } {
  if (p === null) return { text: '—', cls: 'pct-flat' };
  return { text: `${p > 0 ? '+' : ''}${p}%`, cls: p >= 0 ? 'pct-up' : 'pct-down' };
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
  const TEAL = dark ? '#2dd4bf' : '#0a7568';
  const GREEN = dark ? '#4ade80' : '#0f7a52';
  const RED = dark ? '#f87171' : '#c0392b';
  const PALETTE = dark ? PALETTE_DARK : PALETTE_LIGHT;
  const pieBorder = dark ? '#141716' : '#ffffff';
  const prevBar = dark ? '#78716c' : '#d3d6d9';

  const share = shareKind === 'expense' ? category.data?.expense : category.data?.income;

  const pieOption = useMemo<EChartsOption>(() => {
    const items = share?.items ?? [];
    // 金额排行（specs/statistics 分类占比）：按金额降序；净额 ≤ 0 的分类只进图例不进饼图
    const ranked = [...items].sort((a, b) => Number(b.amount) - Number(a.amount));
    const amountOf = (name: string) => {
      const it = ranked.find((i) => i.name === name);
      if (!it) return '';
      const v = Number(it.amount);
      const body = Math.abs(v).toLocaleString('zh-CN', { maximumFractionDigits: 2 });
      return v < 0 ? ` −¥${body}` : ` ¥${body}`;
    };
    return {
      tooltip: { trigger: 'item', formatter: '{b}: ¥{c} ({d}%)' },
      legend: {
        type: 'scroll',
        bottom: 0,
        itemWidth: 10,
        itemHeight: 10,
        textStyle: { fontSize: 11 },
        // 图例含净额为负的分类，且带金额（负值如实显示，如「数码 −¥200」）
        data: ranked.map((i) => i.name),
        formatter: (name: string) => `${name}${amountOf(name)}`,
      },
      color: PALETTE,
      series: [
        {
          type: 'pie',
          radius: ['40%', '66%'],
          itemStyle: { borderColor: pieBorder, borderWidth: 2 },
          // 窄屏标签会互相压住，交给图例与 tooltip
          label: isMobile ? { show: false } : { fontSize: 11, formatter: '{b} {d}%' },
          labelLine: { show: !isMobile },
          // 饼图只绘制净额 > 0 的分类；净额 ≤ 0（如仅退款的分类）不参与绘制
          data: ranked
            .filter((i) => Number(i.amount) > 0)
            .map((i) => ({ name: i.name, value: Number(i.amount) })),
        },
      ],
    };
  }, [share, dark, pieBorder, PALETTE, isMobile]);

  const trendOption = useMemo<EChartsOption>(() => {
    const pts = trend.data?.points ?? [];
    return {
      tooltip: { trigger: 'axis' },
      legend: { top: 0, itemWidth: 12, textStyle: { fontSize: 11 } },
      grid: { left: 48, right: 12, top: 32, bottom: 24 },
      xAxis: { type: 'category', data: pts.map((p) => p.label), axisLabel: { fontSize: 10 } },
      yAxis: { type: 'value', axisLabel: { fontSize: 10 } },
      series: [
        { name: '收入', type: 'line', smooth: true, showSymbol: false, data: pts.map((p) => Number(p.income)), itemStyle: { color: GREEN } },
        { name: '支出', type: 'line', smooth: true, showSymbol: false, data: pts.map((p) => Number(p.expense)), itemStyle: { color: RED } },
        { name: '结余', type: 'line', smooth: true, showSymbol: false, data: pts.map((p) => Number(p.balance)), itemStyle: { color: TEAL }, lineStyle: { width: 2 } },
      ],
    };
  }, [trend.data, dark, TEAL, GREEN, RED]);

  const compareOption = useMemo<EChartsOption>(() => {
    const c = compare.data;
    const dims: Array<{ key: 'income' | 'expense' | 'balance'; label: string }> = [
      { key: 'income', label: '收入' },
      { key: 'expense', label: '支出' },
      { key: 'balance', label: '结余' },
    ];
    return {
      tooltip: { trigger: 'axis' },
      legend: { top: 0, textStyle: { fontSize: 11 } },
      grid: { left: 48, right: 12, top: 32, bottom: 24 },
      xAxis: { type: 'category', data: dims.map((d) => d.label) },
      yAxis: { type: 'value' },
      series: [
        { name: '本期', type: 'bar', barMaxWidth: 26, data: dims.map((d) => Number(c?.current[d.key] ?? 0)), itemStyle: { color: TEAL } },
        { name: '上期', type: 'bar', barMaxWidth: 26, data: dims.map((d) => Number(c?.previous[d.key] ?? 0)), itemStyle: { color: prevBar } },
      ],
    };
  }, [compare.data, dark, TEAL, prevBar]);

  const assetOption = useMemo<EChartsOption>(() => {
    const pts = assets.data?.points ?? [];
    const dates = pts.map((p) => p.date.slice(5)); // MM-DD
    const accountIds = pts.length
      ? pts[pts.length - 1].accounts.map((a) => a.account_id)
      : [];
    const names = new Map<number, string>();
    for (const a of pts[pts.length - 1]?.accounts ?? []) names.set(a.account_id, a.name);
    const series: SeriesOption[] = [
      {
        name: '总资产',
        type: 'line',
        data: pts.map((p) => Number(p.total)),
        lineStyle: { width: 2.5 },
        itemStyle: { color: TEAL },
        areaStyle: { opacity: 0.12 },
        showSymbol: false,
        smooth: true,
      },
    ];
    accountIds.forEach((id, idx) => {
      series.push({
        name: names.get(id) ?? `账户${id}`,
        type: 'line',
        data: pts.map((p) => Number(p.accounts.find((a) => a.account_id === id)?.balance ?? 0)),
        lineStyle: { width: 1.2 },
        itemStyle: { color: PALETTE[(idx + 1) % PALETTE.length] },
        showSymbol: false,
        smooth: true,
      } as SeriesOption);
    });
    return {
      tooltip: { trigger: 'axis' },
      legend: { type: 'scroll', top: 0, textStyle: { fontSize: 11 } },
      grid: { left: 48, right: 12, top: 32, bottom: 24 },
      xAxis: { type: 'category', data: dates, axisLabel: { fontSize: 10 } },
      yAxis: { type: 'value', axisLabel: { fontSize: 10 } },
      series,
    };
  }, [assets.data, dark, TEAL, PALETTE]);

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
          {category.isLoading ? (
            <Spin />
          ) : share && share.items.length ? (
            <>
              <div className="share-total">
                合计 <Money value={share.total} tone={shareKind} />
              </div>
              <EChart option={pieOption} height={260} />
            </>
          ) : (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="区间内无数据" />
          )}
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
          {trend.isLoading ? <Spin /> : <EChart option={trendOption} height={260} />}
        </Card>

        {/* 3. 本期环比（上一同比窗口） */}
        <Card size="small" className="panel" title="本期环比">
          {compare.isLoading ? (
            <Spin />
          ) : (
            <>
              <div className="cmp-meta muted small">
                {compare.data?.start} ~ {compare.data?.end} vs {compare.data?.previous_start} ~{' '}
                {compare.data?.previous_end}
              </div>
              <EChart option={compareOption} height={200} />
              <div className="cmp-row">
                <span className={cmpPct(ch?.income_percent ?? null, true).cls}>
                  收入 {cmpPct(ch?.income_percent ?? null, true).text}
                </span>
                <span className={cmpPct(ch?.expense_percent ?? null, false).cls}>
                  支出 {cmpPct(ch?.expense_percent ?? null, false).text}
                </span>
                <span className={cmpPct(ch?.balance_percent ?? null, true).cls}>
                  结余 {cmpPct(ch?.balance_percent ?? null, true).text}
                </span>
              </div>
              <div className="cmp-values muted small">
                本期 {fmtMoney(cur?.income)}/{fmtMoney(cur?.expense)} · 上期 {fmtMoney(prev?.income)}/
                {fmtMoney(prev?.expense)}
              </div>
            </>
          )}
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
        </Card>

        {/* 5. 本期预算进度（与首页同数据源，specs/budgets；宽面板双列铺排，填满整行） */}
        <Card size="small" className="panel stats-wide" title="预算进度">
          <BudgetProgress
            view={budgets.data}
            loading={budgets.isLoading}
            showPeriod
            configLink={user?.role === 'admin'}
          />
        </Card>

        {/* 6. 资产走势（负债负贡献） */}
        <Card size="small" className="panel stats-wide" title="资产走势">
          {assets.isLoading ? (
            <Spin />
          ) : (assets.data?.points.length ?? 0) > 0 ? (
            <>
              <EChart option={assetOption} height={280} />
              <div className="muted small">
                期末总资产 <Money value={assets.data?.ending_total ?? '0'} />
              </div>
            </>
          ) : (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="区间内无数据" />
          )}
        </Card>
      </div>
    </div>
  );
}
