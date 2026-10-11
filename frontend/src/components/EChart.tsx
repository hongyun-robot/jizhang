import { useEffect, useMemo, useRef } from 'react';
import * as echarts from 'echarts';
import type { EChartsOption } from 'echarts';

import { useResolvedTheme } from '../stores/theme';

/**
 * 画布不读 CSS 变量：亮/暗两套文本、网格、浮层用色都显式给，
 * 并统一字体族（与页面同一套拉丁字体，数字才对得齐）。
 */
const UI_FONT =
  "'Instrument Sans Variable', -apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif";

const LIGHT = {
  text: '#4a4d52',
  axis: '#6b6f75',
  grid: 'rgba(20, 22, 24, 0.07)',
  axisLine: 'rgba(20, 22, 24, 0.15)',
  tipBg: 'rgba(255, 255, 255, 0.97)',
  tipBorder: 'rgba(20, 22, 24, 0.12)',
};

const DARK = {
  text: '#e4e4e7',
  axis: '#a1a1aa',
  grid: 'rgba(244, 244, 245, 0.08)',
  axisLine: 'rgba(244, 244, 245, 0.18)',
  tipBg: 'rgba(20, 23, 22, 0.96)',
  tipBorder: 'rgba(236, 234, 227, 0.14)',
};

type AnyOpt = Record<string, any>;
type Palette = typeof LIGHT;

/**
 * 在调用方 option 之上补主题默认值：只改「颜色与字体」，不动业务配置
 * （调用方显式给的颜色优先，formatter 等原样保留）。
 */
function applyTheme(option: EChartsOption, p: Palette): EChartsOption {
  const o = option as unknown as AnyOpt;
  const out: AnyOpt = {
    ...o,
    textStyle: { fontFamily: UI_FONT, color: p.text, ...(o.textStyle ?? {}) },
  };

  if (o.legend && !Array.isArray(o.legend)) {
    out.legend = {
      ...o.legend,
      textStyle: { fontFamily: UI_FONT, color: p.text, fontSize: 11, ...((o.legend as AnyOpt).textStyle ?? {}) },
    };
  }

  if (o.tooltip && !Array.isArray(o.tooltip)) {
    const t = o.tooltip as AnyOpt;
    out.tooltip = {
      backgroundColor: p.tipBg,
      borderColor: p.tipBorder,
      borderWidth: 1,
      ...t,
      textStyle: { fontFamily: UI_FONT, color: p.text, ...(t.textStyle ?? {}) },
    };
  }

  const axis = (a: AnyOpt, isY: boolean): AnyOpt => {
    const next: AnyOpt = {
      ...a,
      axisLabel: { fontFamily: UI_FONT, color: p.axis, fontSize: 11, ...(a.axisLabel ?? {}) },
      axisLine: {
        ...(a.axisLine ?? {}),
        lineStyle: { color: p.axisLine, ...((a.axisLine ?? {}).lineStyle ?? {}) },
      },
      axisTick: { ...(a.axisTick ?? {}) },
      nameTextStyle: { fontFamily: UI_FONT, color: p.text, ...(a.nameTextStyle ?? {}) },
    };
    if (isY) {
      next.splitLine = {
        ...(a.splitLine ?? {}),
        lineStyle: { color: p.grid, ...((a.splitLine ?? {}).lineStyle ?? {}) },
      };
    }
    return next;
  };

  if (Array.isArray(o.xAxis)) out.xAxis = o.xAxis.map((x: AnyOpt) => axis(x, false));
  else if (o.xAxis) out.xAxis = axis(o.xAxis, false);

  if (Array.isArray(o.yAxis)) out.yAxis = o.yAxis.map((y: AnyOpt) => axis(y, true));
  else if (o.yAxis) out.yAxis = axis(o.yAxis, true);

  if (Array.isArray(o.series)) {
    out.series = (o.series as AnyOpt[]).map((s) =>
      s && typeof s === 'object' && s.label
        ? { ...s, label: { fontFamily: UI_FONT, color: p.text, ...(s.label ?? {}) } }
        : s,
    );
  }

  return out as unknown as EChartsOption;
}

/**
 * ECharts 轻封装：初始化/尺寸自适应/卸载释放；option 变化直接 setOption（跟随亮暗主题）。
 * 可及性：容器 role="img" + 由 option 生成的 aria-label（标题/图例/数据点摘要），
 * 屏幕阅读器可读图表语义；详情数据表由调用方（Stats）以 <details> 提供。
 */
export default function EChart({
  option,
  height = 280,
  ariaLabel,
}: {
  option: EChartsOption;
  height?: number;
  /** 无障碍标签：调用方给业务语义（如「本月支出分类占比」）；缺省从 option 摘要生成 */
  ariaLabel?: string;
}) {
  const domRef = useRef<HTMLDivElement>(null);
  const instRef = useRef<echarts.ECharts | null>(null);
  const mode = useResolvedTheme();

  useEffect(() => {
    const el = domRef.current;
    if (!el) return;
    const chart = echarts.init(el);
    instRef.current = chart;
    const ro = new ResizeObserver(() => chart.resize());
    ro.observe(el);
    return () => {
      ro.disconnect();
      chart.dispose();
      instRef.current = null;
    };
  }, []);

  const themed = useMemo(() => applyTheme(option, mode === 'dark' ? DARK : LIGHT), [option, mode]);

  useEffect(() => {
    instRef.current?.setOption(themed, true);
  }, [themed]);

  const autoLabel = useMemo(() => describeOption(option), [option]);

  return <div ref={domRef} role="img" aria-label={ariaLabel ?? autoLabel} style={{ width: '100%', height }} />;
}

/** 从 option 摘要生成图表 aria-label：标题 + 图例 + 各系列数据点（文本截断防过长）。 */
function describeOption(option: EChartsOption): string {
  const o = option as unknown as AnyOpt;
  const clip = (s: unknown) => String(s ?? '').slice(0, 24);
  const parts: string[] = [];
  if (o.title?.text) parts.push(clip(o.title.text));
  if (Array.isArray(o.legend?.data) && o.legend.data.length) {
    parts.push(`图例：${o.legend.data.map(clip).join('、')}`);
  }
  const series = Array.isArray(o.series) ? o.series : o.series ? [o.series] : [];
  const cats = Array.isArray(o.xAxis?.data) ? o.xAxis.data : [];
  for (const s of series as AnyOpt[]) {
    if (!s) continue;
    const name = clip(s.name ?? s.type ?? '数据');
    const data = Array.isArray(s.data) ? s.data : [];
    if (data.length) {
      const points = cats.length
        ? data.slice(0, 8).map((v: unknown, i: number) => `${clip(cats[i] ?? i)} ${typeof v === 'object' ? clip((v as AnyOpt)?.value) : clip(v)}`)
        : data.slice(0, 8).map((v: unknown, i: number) => `${i + 1} ${typeof v === 'object' ? clip((v as AnyOpt)?.value) : clip(v)}`);
      parts.push(`${name}：${points.join('，')}${data.length > 8 ? ' 等' : ''}`);
    } else {
      parts.push(name);
    }
  }
  return parts.join('；') || '图表';
}
