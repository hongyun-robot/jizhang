import { describe, expect, it } from 'vitest';
import {
  pieOptions,
  trendOptions,
  assetOptions,
  type ChartStyle,
} from './options';
import { applyTheme, describeOption, DARK, LIGHT } from './theme';
import { uiTokens } from '../../theme/tokens';

const style: ChartStyle = {
  dark: false,
  isMobile: false,
  TEAL: '#0a7568',
  GREEN: '#0f7a52',
  RED: '#c0392b',
  PALETTE: ['#0a7568'],
  pieBorder: '#fff',
  prevBar: '#ddd',
};
describe('statistics projections', () => {
  it('keeps negative refunds in the legend while excluding them from the pie', () => {
    const option = pieOptions(
      {
        total: '80',
        items: [
          { id: 1, name: '购物', amount: '100', percent: 125 },
          { id: 2, name: '退款', amount: '-20', percent: -25 },
        ],
      },
      style
    );
    expect(option.legend).toMatchObject({ data: ['购物', '退款'] });
    expect(option.series).toMatchObject([
      { data: [{ name: '购物', value: 100 }] },
    ]);
    expect(pieOptions(undefined, style).series).toMatchObject([{ data: [] }]);
  });
  it('preserves the source amounts and account contribution in chart summaries', () => {
    const trend = trendOptions(
      {
        start: '2026-10-01',
        end: '2026-10-01',
        granularity: 'day',
        points: [
          {
            key: '2026-10-01',
            label: '10-01',
            income: '20',
            expense: '-10',
            balance: '30',
          },
        ],
      },
      style
    );
    expect(describeOption(trend)).toContain('支出：10-01 -10');
    expect(assetOptions(undefined, style).series).toMatchObject([
      { name: '总资产', data: [] },
    ]);
  });
  it('uses shared theme values and retains explicit option overrides', () => {
    expect(LIGHT.text).toBe(uiTokens('light')['--chart-text']);
    expect(DARK.tipBg).toBe(uiTokens('dark')['--chart-tip-bg']);
    expect(
      applyTheme(
        {
          textStyle: { color: '#123456' },
          series: [{ type: 'pie', label: { color: '#654321' } }],
        },
        DARK
      )
    ).toMatchObject({
      textStyle: { color: '#123456' },
      series: [{ label: { color: '#654321' } }],
    });
    expect(describeOption({})).toBe('图表');
  });
});
