import { Alert } from 'antd';

import type { FieldContext } from './context';
export default function ReverseFields({ context }: { context: FieldContext }) {
  const { reason } = context;
  return (
    <>
      <Alert
        type="warning"
        title="撤销后恢复真实欠款与未来计划"
        description="相关资金流水必须仍在开放账期，且你有权修改全部关联记录。历史操作会保留。"
      />
      {reason(true)}
    </>
  );
}
