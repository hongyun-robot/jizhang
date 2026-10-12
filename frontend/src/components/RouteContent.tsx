import { Component, Suspense, type ReactNode } from 'react';
import { Alert, Button, Skeleton } from 'antd';

class PageError extends Component<
  { children: ReactNode },
  { failed: boolean }
> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  render() {
    if (this.state.failed)
      return (
        <div className="page" role="alert">
          <Alert
            type="error"
            title="页面加载失败"
            description="请恢复网络后重新加载当前页面。"
            action={
              <Button onClick={() => window.location.reload()}>重新加载</Button>
            }
          />
        </div>
      );
    return this.props.children;
  }
}
export default function RouteContent({ children }: { children: ReactNode }) {
  return (
    <PageError>
      <Suspense
        fallback={
          <div className="page" role="status">
            <Skeleton active />
          </div>
        }
      >
        {children}
      </Suspense>
    </PageError>
  );
}
