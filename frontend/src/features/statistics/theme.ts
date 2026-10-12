import type { EChartsOption } from 'echarts';
import { isRecord } from '../../lib/json';

import { uiTokens } from '../../theme/tokens';
export const UI_FONT = uiTokens('light')['--chart-font'];
function chartTheme(mode: 'light' | 'dark') {
  const tokens = uiTokens(mode);
  return {
    text: tokens['--chart-text'],
    axis: tokens['--chart-axis'],
    grid: tokens['--chart-grid'],
    axisLine: tokens['--chart-axis-line'],
    tipBg: tokens['--chart-tip-bg'],
    tipBorder: tokens['--chart-tip-border'],
  };
}
export const LIGHT = chartTheme('light');
export const DARK = chartTheme('dark');
function components<T>(
  value: T | T[] | undefined,
  apply: (value: T) => T
): T | T[] | undefined {
  return value === undefined
    ? undefined
    : Array.isArray(value)
      ? value.map(apply)
      : apply(value);
}
type XAxis = Exclude<NonNullable<EChartsOption['xAxis']>, unknown[]>;
type YAxis = Exclude<NonNullable<EChartsOption['yAxis']>, unknown[]>;
export function applyTheme(
  option: EChartsOption,
  palette: typeof LIGHT
): EChartsOption {
  const axis = <T extends XAxis | YAxis>(value: T, isY: boolean): T => ({
    ...value,
    axisLabel: {
      fontFamily: UI_FONT,
      color: palette.axis,
      fontSize: 11,
      ...value.axisLabel,
    },
    axisLine: {
      ...value.axisLine,
      lineStyle: { color: palette.axisLine, ...value.axisLine?.lineStyle },
    },
    nameTextStyle: {
      fontFamily: UI_FONT,
      color: palette.text,
      ...value.nameTextStyle,
    },
    ...(isY
      ? {
          splitLine: {
            ...value.splitLine,
            lineStyle: { color: palette.grid, ...value.splitLine?.lineStyle },
          },
        }
      : {}),
  });
  return {
    ...option,
    textStyle: {
      fontFamily: UI_FONT,
      color: palette.text,
      ...option.textStyle,
    },
    legend: components(option.legend, (legend) => ({
      ...legend,
      textStyle: {
        fontFamily: UI_FONT,
        color: palette.text,
        fontSize: 11,
        ...legend.textStyle,
      },
    })),
    tooltip: components(option.tooltip, (tooltip) => ({
      backgroundColor: palette.tipBg,
      borderColor: palette.tipBorder,
      borderWidth: 1,
      ...tooltip,
      textStyle: {
        fontFamily: UI_FONT,
        color: palette.text,
        ...tooltip.textStyle,
      },
    })),
    xAxis: components(option.xAxis, (value) => axis(value, false)),
    yAxis: components(option.yAxis, (value) => axis(value, true)),
    series: components(option.series, (series) => {
      switch (series.type) {
        case 'line':
          return {
            ...series,
            label: {
              fontFamily: UI_FONT,
              color: palette.text,
              ...series.label,
            },
          };
        case 'bar':
          return {
            ...series,
            label: {
              fontFamily: UI_FONT,
              color: palette.text,
              ...series.label,
            },
          };
        case 'pie':
          return {
            ...series,
            label: {
              fontFamily: UI_FONT,
              color: palette.text,
              ...series.label,
            },
          };
        default:
          return series;
      }
    }),
  };
}
export function describeOption(option: EChartsOption): string {
  const clip = (value: unknown) => String(value ?? '').slice(0, 24);
  const parts: string[] = [];
  const title = Array.isArray(option.title) ? option.title[0] : option.title;
  if (title?.text) parts.push(clip(title.text));
  const legend = Array.isArray(option.legend)
    ? option.legend[0]
    : option.legend;
  if (legend?.data?.length)
    parts.push(
      '图例：' +
        legend.data
          .map((value) =>
            typeof value === 'string' ? clip(value) : clip(value.name)
          )
          .join('、')
    );
  const axis = Array.isArray(option.xAxis) ? option.xAxis[0] : option.xAxis;
  const categories =
    axis && 'data' in axis && Array.isArray(axis.data) ? axis.data : [];
  const series = Array.isArray(option.series)
    ? option.series
    : option.series
      ? [option.series]
      : [];
  for (const item of series) {
    const name = clip(item.name ?? item.type ?? '数据');
    const data = 'data' in item && Array.isArray(item.data) ? item.data : [];
    if (!data.length) {
      parts.push(name);
      continue;
    }
    const points = data
      .slice(0, 8)
      .map(
        (value: unknown, index: number) =>
          `${clip(categories[index] ?? index + 1)} ${clip(isRecord(value) ? value.value : value)}`
      );
    parts.push(`${name}：${points.join('，')}${data.length > 8 ? ' 等' : ''}`);
  }
  return parts.join('；') || '图表';
}
