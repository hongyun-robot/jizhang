import { useLayoutEffect, useRef } from 'react';

/** 金额展示：CNY 两位小数；支出红、收入绿（仅语义用色，界面主色保持青绿）。 */

export function fmtMoney(v: string | number | null | undefined): string {
  if (v === null || v === undefined) return '0.00';
  const n = typeof v === 'string' ? Number(v) : v;
  if (Number.isNaN(n)) return String(v);
  return n.toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

export default function Money({
  value,
  tone = 'plain',
  currency = true,
  className,
  animate = false,
}: {
  value: string | number | null | undefined;
  /** expense=红 income=green plain=继承文本色 */
  tone?: 'expense' | 'income' | 'plain';
  currency?: boolean;
  className?: string;
  /** 数字滚动入场（首页快照卡等关键数字用；默认关闭） */
  animate?: boolean;
}) {
  const n = Number(value ?? 0);
  // 零值不着语义色（0.00 显示红/绿会误导）
  const finalTone = Number.isNaN(n) || n === 0 ? 'plain' : tone;
  const cls = ['money', `money-${finalTone}`, className].filter(Boolean).join(' ');
  const sign = n < 0 ? '-' : '';
  const target = Number.isNaN(n) ? 0 : Math.abs(n);
  const body = fmtMoney(target);

  const numRef = useRef<HTMLSpanElement>(null);
  const shownRef = useRef(0);

  /**
   * 数字滚动：只写 DOM、不进 React 状态（避免每帧重渲染）。
   * animate 时 React 不渲染子节点（见下方 {animate ? null : body}），
   * 否则直接改 textContent 会拆掉 React 持有的文本节点，后续更新写进游离节点、界面卡死。
   * 数值中途被打断时，从「最后滚到的位置」续接。
   */
  useLayoutEffect(() => {
    if (!animate) return;
    const el = numRef.current;
    if (!el) return;
    const from = shownRef.current;
    if (from === target) {
      el.textContent = body;
      return;
    }
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    if (reduced) {
      shownRef.current = target;
      el.textContent = body;
      return;
    }
    const DURATION = 650;
    const t0 = performance.now();
    let raf = 0;
    const tick = (t: number) => {
      const p = Math.min(1, (t - t0) / DURATION);
      const eased = 1 - Math.pow(1 - p, 3);
      const v = from + (target - from) * eased;
      shownRef.current = v;
      el.textContent = fmtMoney(v);
      if (p < 1) {
        raf = requestAnimationFrame(tick);
      } else {
        shownRef.current = target;
        el.textContent = body;
      }
    };
    el.textContent = fmtMoney(from);
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [target, animate, body]);

  return (
    <span className={cls}>
      {sign}
      {currency ? '¥' : ''}
      <span ref={numRef}>{animate ? null : body}</span>
    </span>
  );
}
