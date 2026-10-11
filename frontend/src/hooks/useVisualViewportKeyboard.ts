import { useEffect } from 'react';

/**
 * 移动端软键盘避让（specs/transactions 软键盘避让）。
 *
 * 监听 `visualViewport.resize/scroll`，把「键盘遮挡高度」以 `--kb-inset` 写到
 * `document.documentElement`：`innerHeight - (visualViewport.height + offsetTop)`。
 * 键盘收起/视口恢复时归 0；`active` 关闭（弹窗关闭）或组件卸载时清理复位，避免残留。
 *
 * rAF 合帧：visualViewport 事件在键盘弹出过程中高频触发，逐帧写会引发布局抖动，
 * 合帧后每帧至多写一次；失活时取消未执行的帧。
 *
 * @param active 弹窗打开时传 true，监听并写入；false 时移除监听并复位变量
 */
export function useVisualViewportKeyboard(active = true) {
  useEffect(() => {
    if (!active) return;
    const vv = window.visualViewport;
    const root = document.documentElement;
    let raf = 0;

    const apply = () => {
      raf = 0;
      if (!vv) return;
      const inset = Math.max(0, Math.round(window.innerHeight - (vv.height + vv.offsetTop)));
      root.style.setProperty('--kb-inset', `${inset}px`);
    };
    const schedule = () => {
      if (!raf) raf = requestAnimationFrame(apply);
    };

    if (vv) {
      vv.addEventListener('resize', schedule);
      vv.addEventListener('scroll', schedule);
      apply(); // 挂载即同步一次（例如键盘已在、组件后挂载的场景）
    }
    return () => {
      if (raf) cancelAnimationFrame(raf);
      vv?.removeEventListener('resize', schedule);
      vv?.removeEventListener('scroll', schedule);
      root.style.removeProperty('--kb-inset');
    };
  }, [active]);
}
