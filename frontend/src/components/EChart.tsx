import {
  applyTheme,
  describeOption,
  LIGHT,
  DARK,
} from '../features/statistics/theme';
import { useEffect, useMemo, useRef } from 'react';
import * as echarts from 'echarts/core';
import { BarChart, LineChart, PieChart } from 'echarts/charts';
import {
  GridComponent,
  LegendComponent,
  LegendScrollComponent,
  TooltipComponent,
  TitleComponent,
} from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import type { EChartsOption } from 'echarts';

import { useResolvedTheme } from '../stores/theme';
import { isRecord } from '../lib/json';
echarts.use([
  BarChart,
  LineChart,
  PieChart,
  GridComponent,
  LegendComponent,
  LegendScrollComponent,
  TooltipComponent,
  TitleComponent,
  CanvasRenderer,
]);

/**
 * ECharts 轻封装：初始化/尺寸自适应/卸载释放；option 变化直接 setOption（跟随亮暗主题）。
 * 可及性：容器 role="img" + 由 option 生成的 aria-label（标题/图例/数据点摘要），
 * 屏幕阅读器可读图表语义；详情数据表由调用方（Stats）以 <details> 提供。
 */
export default function EChart({
  option,
  height = 280,
  ariaLabel,
  onLegendSelectionChange,
}: {
  option: EChartsOption;
  height?: number;
  /** 无障碍标签：调用方给业务语义（如「本月支出分类占比」）；缺省从 option 摘要生成 */
  ariaLabel?: string;
  /** 图例选择状态；应用新 option 重置图表时也同步通知。 */
  onLegendSelectionChange?: (selected: Record<string, boolean>) => void;
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

  const themed = useMemo(
    () => applyTheme(option, mode === 'dark' ? DARK : LIGHT),
    [option, mode]
  );

  useEffect(() => {
    const chart = instRef.current;
    if (!chart) return;
    chart.setOption(themed, true);
    if (!onLegendSelectionChange) return;

    const legend = Array.isArray(themed.legend) ? themed.legend[0] : themed.legend;
    onLegendSelectionChange(legend?.selected ?? {});
    const onSelect = (event: unknown) => {
      if (!isRecord(event) || !isRecord(event.selected)) return;
      const selected = Object.fromEntries(
        Object.entries(event.selected).map(([name, value]) => [name, value !== false])
      );
      onLegendSelectionChange(selected);
    };
    chart.on('legendselectchanged', onSelect);
    return () => {
      chart.off('legendselectchanged', onSelect);
    };
  }, [themed, onLegendSelectionChange]);

  const autoLabel = useMemo(() => describeOption(option), [option]);

  return (
    <div
      ref={domRef}
      role="img"
      aria-label={ariaLabel ?? autoLabel}
      style={{ width: '100%', height }}
    />
  );
}
