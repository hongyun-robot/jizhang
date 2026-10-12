import type { ReactNode } from 'react';
import { Alert, Button, Empty, Skeleton } from 'antd';

import { errMsg } from '../api/client';

interface Props {
  loading?: boolean;
  /** truthy（react-query 的 error 对象 / 字符串）即进入错误态 */
  error?: Error | string | null;
  /** Successfully loaded content remains visible if background updates fail. */
  hasData?: boolean;
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
  hasData = false,
  isEmpty,
  onRetry,
  emptyDescription = '暂无数据',
  skeleton,
  children,
}: Props) {
  if (loading && !hasData) {
    return (
      <>
        {skeleton ?? <Skeleton active paragraph={{ rows: 3 }} title={false} />}
      </>
    );
  }
  if (error) {
    const notice = (
      <div className="query-state" role="alert">
        <Alert
          type="error"
          showIcon
          title={hasData ? '数据更新失败' : '数据加载失败'}
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
    return hasData ? (
      <>
        {notice}
        {isEmpty ? (
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description={emptyDescription}
          />
        ) : (
          children
        )}
      </>
    ) : (
      notice
    );
  }
  if (isEmpty) {
    return (
      <Empty
        image={Empty.PRESENTED_IMAGE_SIMPLE}
        description={emptyDescription}
      />
    );
  }
  return <>{children}</>;
}

export function QueryBoundary({
  query,
  children,
}: {
  query: {
    data: unknown;
    error: Error | null;
    isLoading: boolean;
    refetch: () => unknown;
  };
  children: ReactNode;
}) {
  return (
    <QueryState
      loading={query.isLoading}
      error={query.error}
      hasData={query.data !== undefined}
      onRetry={() => void query.refetch()}
    >
      {children}
    </QueryState>
  );
}
