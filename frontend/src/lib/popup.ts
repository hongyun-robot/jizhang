/**
 * 弹层挂载到触发器父级（specs/ui-shell 弹层挂载于触发器）：
 * 下拉/级联/日期面板随弹窗内容一起滚动，键盘弹出时不与触发器脱位。
 * 注意：容器需是定位上下文（.txn-form / .transfer-row / Form.Item 控件区已补 position:relative）。
 */
export const popupInParent = (node: HTMLElement): HTMLElement =>
  node.parentElement ?? document.body;
