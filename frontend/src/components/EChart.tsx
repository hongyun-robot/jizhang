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
  text: '#4b473e',
  axis: '#7b766a',
  grid: 'rgba(38, 35, 28, 0.08)',
  axisLine: 'rgba(38, 35, 28, 0.16)',
  tipBg: 'rgba(255, 254, 250, 0.97)',
  tipBorder: 'rgba(38, 35, 28, 0.12)',
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

/** ECharts 轻封装：初始化/尺寸自适应/卸载释放；option 变化直接 setOption（跟随亮暗主题）。 */
export default function EChart({ option, height = 280 }: { option: EChartsOption; height?: number }) {
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

  return <div ref={domRef} style={{ width: '100%', height }} />;
}
