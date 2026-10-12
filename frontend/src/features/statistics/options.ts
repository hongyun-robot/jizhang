import type { EChartsOption, SeriesOption } from 'echarts';
import type {
  CategoryShareResp,
  TrendResp,
  CompareResp,
  AssetsResp,
} from '../../api/types';
export interface ChartStyle {
  dark: boolean;
  isMobile: boolean;
  TEAL: string;
  GREEN: string;
  RED: string;
  PALETTE: string[];
  pieBorder: string;
  prevBar: string;
}

export function pieOptions(
  data: CategoryShareResp['expense'] | undefined,
  style: ChartStyle
): EChartsOption {
  const { PALETTE, pieBorder, isMobile } = style;

  const items = data?.items ?? [];
  // 金额排行（specs/statistics 分类占比）：按金额降序；净额 ≤ 0 的分类只进图例不进饼图
  const ranked = [...items].sort((a, b) => Number(b.amount) - Number(a.amount));
  const amountOf = (name: string) => {
    const it = ranked.find((i) => i.name === name);
    if (!it) return '';
    const v = Number(it.amount);
    const body = Math.abs(v).toLocaleString('zh-CN', {
      maximumFractionDigits: 2,
    });
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
        label: isMobile
          ? { show: false }
          : { fontSize: 11, formatter: '{b} {d}%' },
        labelLine: { show: !isMobile },
        // 饼图只绘制净额 > 0 的分类；净额 ≤ 0（如仅退款的分类）不参与绘制
        data: ranked
          .filter((i) => Number(i.amount) > 0)
          .map((i) => ({ name: i.name, value: Number(i.amount) })),
      },
    ],
  };
}

export function trendOptions(
  data: TrendResp | undefined,
  style: ChartStyle
): EChartsOption {
  const { TEAL, GREEN, RED } = style;

  const pts = data?.points ?? [];
  return {
    tooltip: { trigger: 'axis' },
    legend: { top: 0, itemWidth: 12, textStyle: { fontSize: 11 } },
    grid: { left: 48, right: 12, top: 32, bottom: 24 },
    xAxis: {
      type: 'category',
      data: pts.map((p) => p.label),
      axisLabel: { fontSize: 10 },
    },
    yAxis: { type: 'value', axisLabel: { fontSize: 10 } },
    series: [
      {
        name: '收入',
        type: 'line',
        smooth: true,
        showSymbol: false,
        data: pts.map((p) => Number(p.income)),
        itemStyle: { color: GREEN },
      },
      {
        name: '支出',
        type: 'line',
        smooth: true,
        showSymbol: false,
        data: pts.map((p) => Number(p.expense)),
        itemStyle: { color: RED },
      },
      {
        name: '结余',
        type: 'line',
        smooth: true,
        showSymbol: false,
        data: pts.map((p) => Number(p.balance)),
        itemStyle: { color: TEAL },
        lineStyle: { width: 2 },
      },
    ],
  };
}

export function comparisonOptions(
  data: CompareResp | undefined,
  style: ChartStyle
): EChartsOption {
  const { TEAL, prevBar } = style;

  const c = data;
  const dims: Array<{ key: 'income' | 'expense' | 'balance'; label: string }> =
    [
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
      {
        name: '本期',
        type: 'bar',
        barMaxWidth: 26,
        data: dims.map((d) => Number(c?.current[d.key] ?? 0)),
        itemStyle: { color: TEAL },
      },
      {
        name: '上期',
        type: 'bar',
        barMaxWidth: 26,
        data: dims.map((d) => Number(c?.previous[d.key] ?? 0)),
        itemStyle: { color: prevBar },
      },
    ],
  };
}

export function assetOptions(
  data: AssetsResp | undefined,
  style: ChartStyle
): EChartsOption {
  const { TEAL, PALETTE } = style;

  const pts = data?.points ?? [];
  const dates = pts.map((p) => p.date.slice(5)); // MM-DD
  const accountIds = pts.length
    ? pts[pts.length - 1].accounts.map((a) => a.account_id)
    : [];
  const names = new Map<number, string>();
  for (const a of pts[pts.length - 1]?.accounts ?? [])
    names.set(a.account_id, a.name);
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
      data: pts.map((p) =>
        Number(p.accounts.find((a) => a.account_id === id)?.balance ?? 0)
      ),
      lineStyle: { width: 1.2 },
      itemStyle: { color: PALETTE[(idx + 1) % PALETTE.length] },
      showSymbol: false,
      smooth: true,
    });
  });
  return {
    tooltip: { trigger: 'axis' },
    legend: { type: 'scroll', top: 0, textStyle: { fontSize: 11 } },
    grid: { left: 48, right: 12, top: 32, bottom: 24 },
    xAxis: { type: 'category', data: dates, axisLabel: { fontSize: 10 } },
    yAxis: { type: 'value', axisLabel: { fontSize: 10 } },
    series,
  };
}
