import { useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { App, Button, Form, Input } from 'antd';
import { LockOutlined, UserOutlined } from '@ant-design/icons';

import { errMsg } from '../api/client';
import { useAuth } from '../stores/auth';
import { session } from '../auth/session';
import { returnTarget } from '../navigation/routes';
import { BASENAME } from '../base';
import Brand from '../components/Brand';

/** 登录页左侧「账页」样张：装饰性账目行（aria-hidden），说明产品本身长什么样。 */
const SAMPLE_ROWS = [
  { label: '餐饮 / 午餐', value: '-¥32.00', tone: 'money-expense' },
  { label: '工资 / 月薪', value: '+¥18,600.00', tone: 'money-income' },
  { label: '交通 / 地铁', value: '-¥6.00', tone: 'money-expense' },
  { label: '结余 / 10月', value: '¥4,511.80', tone: '' },
];

export default function Login() {
  const navigate = useNavigate();
  const location = useLocation();
  // 静态 message 改走 AntApp 容器（与全站一致，暗色主题下样式正确）
  const { message } = App.useApp();
  const [loading, setLoading] = useState(false);

  const onFinish = async (values: { username: string; password: string }) => {
    setLoading(true);
    try {
      await session.login(values);
      const from = (location.state as { from?: unknown } | null)?.from;
      navigate(returnTarget(from, useAuth.getState().user?.role, BASENAME), {
        replace: true,
      });
    } catch (err) {
      message.error(errMsg(err));
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="login">
      <aside className="login-art">
        <div className="art-top">
          <span className="art-brand">
            <Brand size={34} />
          </span>
        </div>

        <div className="art-copy">
          <p className="art-title">
            一本全家
            <br />
            共用的账。
          </p>
          <p className="art-sub">
            收支流水、预算水位、周期记账与多账户余额，写在同一张账页上。
          </p>

          <div className="art-ledger" aria-hidden="true">
            {SAMPLE_ROWS.map((r) => (
              <div className="art-row" key={r.label}>
                <span className="art-row-label">{r.label}</span>
                <span className={`money ${r.tone}`.trim()}>{r.value}</span>
              </div>
            ))}
          </div>
        </div>

        <div className="art-foot">单账本 · 四档权限 · 暗色模式 · CSV 导出</div>
      </aside>

      <main className="login-panel">
        <div className="login-card">
          <div className="eyebrow">Sign in</div>
          <h1>登录</h1>
          <p className="login-note">账号由管理员统一创建，未开放注册。</p>

          <Form
            layout="vertical"
            onFinish={onFinish}
            size="large"
            requiredMark={false}
          >
            <Form.Item
              label="用户名"
              name="username"
              rules={[{ required: true, message: '请输入用户名' }]}
            >
              <Input
                prefix={<UserOutlined />}
                placeholder="用户名"
                autoComplete="username"
              />
            </Form.Item>
            <Form.Item
              label="密码"
              name="password"
              rules={[{ required: true, message: '请输入密码' }]}
            >
              <Input.Password
                prefix={<LockOutlined />}
                placeholder="密码"
                autoComplete="current-password"
              />
            </Form.Item>
            <Button type="primary" htmlType="submit" block loading={loading}>
              登录
            </Button>
          </Form>
        </div>
      </main>
    </div>
  );
}
