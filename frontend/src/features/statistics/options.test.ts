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
  it.each([-50, 0])('pins the asset total (%s) and sorts nonzero balances descending with zero balances last', (total) => {
    const tooltip = assetOptions(undefined, style).tooltip;
    if (!tooltip || Array.isArray(tooltip) || typeof tooltip.formatter !== 'function') {
      throw new Error('Asset tooltip must provide its formatter');
    }
    const params = [
      { seriesName: '总资产', value: total },
      { seriesName: '负债账户', value: -100 },
      { seriesName: '零余额', value: 0 },
      { seriesName: '大额<&账户>', value: 1200.5 },
      { seriesName: '同额账户', value: 1200.5 },
      { seriesName: '缺失估值', value: NaN },
      { seriesName: '空值账户', value: null },
      { seriesName: '负零余额', value: -0 },
    ].map((item, seriesIndex) => ({
      componentType: 'series',
      componentSubType: 'line',
      componentIndex: seriesIndex,
      seriesIndex,
      name: '11-01 <script>',
      dataIndex: 0,
      data: item.value,
      $vars: [],
      ...item,
    }));
    const original = [...params];
    const html = tooltip.formatter(params, 'test');
    if (typeof html !== 'string') throw new Error('Expected HTML asset tooltip');
    const positions = [
      '总资产', '大额&lt;&amp;账户&gt;', '同额账户', '负债账户', '缺失估值', '空值账户', '零余额', '负零余额',
    ].map((name) => html.indexOf(name));
    expect(positions.every((position) => position >= 0)).toBe(true);
    expect(positions).toEqual([...positions].sort((a, b) => a - b));
    expect(html).toContain('1,200.5');
    expect(html).toContain('11-01 &lt;script&gt;');
    expect(html).not.toContain('<script>');
    expect(html).not.toContain('大额<&账户>');
    expect(html).not.toContain('NaN');
    expect(params).toEqual(original);
  });
});
