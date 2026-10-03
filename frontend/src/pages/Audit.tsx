import { useState } from 'react';
import { Card, Empty, Table, Tag } from 'antd';
import type { TableColumnsType } from 'antd';
import dayjs from 'dayjs';

import { useAuditLogs } from '../api/hooks';
import type { AuditItem } from '../api/types';

/** 动作标签（与后端 action 编码一一对应）。 */
const ACTION_LABEL: Record<string, { text: string; color?: string }> = {
  'period.close': { text: '封账', color: 'red' },
  'period.reopen': { text: '解封', color: 'green' },
};

/**
 * 审计日志查看页（仅管理员，specs/billing-periods「解封与审计日志」）：
 * 封账/解封留痕 —— 谁、何时、哪一期、动作。
 */
export default function Audit() {
  const [page, setPage] = useState(1);
  const logs = useAuditLogs(page);

  const columns: TableColumnsType<AuditItem> = [
    {
      title: '时间',
      dataIndex: 'created_at',
      width: '30%',
      render: (v: string) => dayjs(v).format('YYYY-MM-DD HH:mm'),
    },
    {
      title: '操作人',
      dataIndex: 'username',
      width: '25%',
      render: (v: string, r) => v || `#${r.user_id}`,
    },
    {
      title: '动作',
      dataIndex: 'action',
      width: '20%',
      render: (a: string) => {
        const m = ACTION_LABEL[a];
        return <Tag color={m?.color}>{m?.text ?? a}</Tag>;
      },
    },
    { title: '账期', dataIndex: 'period_key', width: '25%' },
  ];

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <div className="eyebrow">AUDIT · 封账与解封留痕</div>
          <h2>审计日志</h2>
        </div>
      </div>

      <Card size="small" className="panel">
        <Table<AuditItem>
          rowKey="id"
          size="small"
          columns={columns}
          dataSource={logs.data?.items}
          loading={logs.isLoading}
          locale={{
            emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无审计日志" />,
          }}
          pagination={{
            current: page,
            pageSize: logs.data?.page_size ?? 20,
            total: logs.data?.total ?? 0,
            onChange: setPage,
            showSizeChanger: false,
            hideOnSinglePage: true,
            size: 'small',
          }}
        />
      </Card>
    </div>
  );
}
