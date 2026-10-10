import type { ReactNode } from 'react';
import { Alert, Button, Empty, Skeleton } from 'antd';

import { errMsg } from '../api/client';

interface Props {
  loading?: boolean;
  /** truthy（react-query 的 error 对象 / 字符串）即进入错误态 */
  error?: unknown;
  isEmpty?: boolean;
  onRetry?: () => void;
  emptyDescription?: ReactNode;
  /** 加载态渲染内容，默认行骨架 */
  skeleton?: ReactNode;
  /** 三态都未命中时渲染的正常内容 */
  children?: ReactNode;
}

/**
 * 数据三态（specs/ui-shell 数据加载错误态）：loading / error / empty 统一出口。
 * 关键约束：请求失败 MUST 走错误态 + 重试，不得落入「暂无数据」空态。
 */
export default function QueryState({
  loading,
  error,
  isEmpty,
  onRetry,
  emptyDescription = '暂无数据',
  skeleton,
  children,
}: Props) {
  if (loading) {
    return <>{skeleton ?? <Skeleton active paragraph={{ rows: 3 }} title={false} />}</>;
  }
  if (error) {
    return (
      <div className="query-state" role="alert">
        <Alert
          type="error"
          showIcon
          message="数据加载失败"
          description={errMsg(error)}
          action={
            onRetry ? (
              <Button size="small" onClick={onRetry}>
                重试
              </Button>
            ) : undefined
          }
        />
      </div>
    );
  }
  if (isEmpty) {
    return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={emptyDescription} />;
  }
  return <>{children}</>;
}
