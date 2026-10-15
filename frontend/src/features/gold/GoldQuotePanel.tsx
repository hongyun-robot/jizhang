import { Alert, App, Button } from 'antd';
import dayjs from 'dayjs';
import { useGoldMutations, useGoldQuote } from '../../api/gold';
import { errMsg } from '../../api/client';
import { useEditorSession } from '../../hooks/useEditorSession';

export function quoteTime(value?: string | null) { return value ? dayjs(value).format('YYYY-MM-DD HH:mm:ss') : '尚未取得'; }
export default function GoldQuotePanel({ active = true }: { active?: boolean }) {
  const quote = useGoldQuote(active);
  const { refresh } = useGoldMutations();
  const { message } = App.useApp();
  const editor = useEditorSession(active, 'quote');
  const value = quote.data;
  const reload = async () => {
    const current = editor.capture();
    try {
      const result = await refresh.mutateAsync();
      if (!current()) return;
      if (result.error) message.warning(result.error);
      else if (result.last_attempt_at === value?.last_attempt_at) message.info(`报价已缓存，请在 ${quoteTime(result.next_refresh_at)} 后再次刷新`);
      else message.success('报价查询完成');
    } catch (error) { if (current()) message.error(errMsg(error)); }
  };
  return <section className="gold-quote" aria-label="建行积存金参考报价">
    <div className="gold-quote-head"><strong>建行积存金参考报价</strong><Button size="small" loading={refresh.isPending} disabled={refresh.isPending || value?.refreshing} onClick={() => void reload()}>手动刷新报价</Button></div>
    <div className="gold-quote-prices"><div><span>主动积存价（买入）</span><strong>{value?.quote?.buy_price ?? '待报价'}<small> 元/克</small></strong></div><div><span>赎回价（卖出）</span><strong>{value?.quote?.sell_price ?? '待报价'}<small> 元/克</small></strong></div></div>
    <p className="small muted">银行报价：{quoteTime(value?.quote?.quote_at)}<br />查询成功：{quoteTime(value?.last_success_at)}</p>
    <p className="small muted">每小时自动更新，共享缓存。仅供核对，登记金额以银行实际成交为准。</p>
    {value?.status === 'source_unchanged' && <p className="small muted">查询成功，银行源报价时间尚未更新。</p>}
    {(value?.error || value?.status === 'update_error' || quote.isError || !value?.quote) && <Alert type={value?.status === 'update_error' ? 'error' : 'warning'} title={value?.status === 'update_error' ? '报价已连续两小时未成功更新' : value?.error || (quote.isError ? errMsg(quote.error) : '尚无有效报价，仍可登记已完成交易；市值暂待报价。')} />}
  </section>;
}
