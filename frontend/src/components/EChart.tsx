import { useEffect, useMemo, useRef } from 'react';
import * as echarts from 'echarts';
import type { EChartsOption } from 'echarts';

import { useResolvedTheme } from '../stores/theme';

/** 暗色补色（任务 12.1）：echarts 画布不读 CSS 变量，深色底需显式给文字/网格/浮层用色。 */
const DARK_TEXT = '#e4e4e7';
const DARK_AXIS = '#a1a1aa';
const DARK_GRID = 'rgba(244, 244, 245, 0.08)';
const DARK_AXIS_LINE = 'rgba(244, 244, 245, 0.18)';
const DARK_TIP_BG = 'rgba(24, 24, 27, 0.96)';
const DARK_TIP_BORDER = 'rgba(244, 244, 245, 0.14)';

type AnyOpt = Record<string, any>;

/**
 * 在调用方 option 之上补暗色默认值：只改「颜色」，不动业务配置
 * （调用方显式给的颜色优先，font/formatter 等原样保留）。
 */
function applyDark(option: EChartsOption): EChartsOption {
  const o = option as unknown as AnyOpt;
  const out: AnyOpt = { ...o, textStyle: { color: DARK_TEXT, ...(o.textStyle ?? {}) } };

  if (o.legend && !Array.isArray(o.legend)) {
    out.legend = {
      ...o.legend,
      textStyle: { color: DARK_TEXT, fontSize: 11, ...((o.legend as AnyOpt).textStyle ?? {}) },
    };
  }

  if (o.tooltip && !Array.isArray(o.tooltip)) {
    const t = o.tooltip as AnyOpt;
    out.tooltip = {
      backgroundColor: DARK_TIP_BG,
      borderColor: DARK_TIP_BORDER,
      ...t,
      textStyle: { color: DARK_TEXT, ...(t.textStyle ?? {}) },
    };
  }

  const axis = (a: AnyOpt, isY: boolean): AnyOpt => {
    const next: AnyOpt = {
      ...a,
      axisLabel: { color: DARK_AXIS, ...(a.axisLabel ?? {}) },
      axisLine: {
        ...(a.axisLine ?? {}),
        lineStyle: { color: DARK_AXIS_LINE, ...((a.axisLine ?? {}).lineStyle ?? {}) },
      },
      nameTextStyle: { color: DARK_TEXT, ...(a.nameTextStyle ?? {}) },
    };
    if (isY) {
      next.splitLine = {
        ...(a.splitLine ?? {}),
        lineStyle: { color: DARK_GRID, ...((a.splitLine ?? {}).lineStyle ?? {}) },
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
        ? { ...s, label: { color: DARK_TEXT, ...(s.label ?? {}) } }
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

  const themed = useMemo(
    () => (mode === 'dark' ? applyDark(option) : option),
    [option, mode],
  );

  useEffect(() => {
    instRef.current?.setOption(themed, true);
  }, [themed]);

  return <div ref={domRef} style={{ width: '100%', height }} />;
}
