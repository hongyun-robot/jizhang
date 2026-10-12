import SelectionScope from './SelectionScope';
import ResponsiveSelect from './ResponsiveSelect';
import { QueryBoundary } from './QueryState';
import { useState } from 'react';
import {
  App,
  Button,
  Card,
  Form,
  Grid,
  Input,
  Modal,
  Popconfirm,
  Table,
  Tag,
} from 'antd';
import type { TableColumnsType } from 'antd';
import { PlusOutlined } from '@ant-design/icons';

import { errMsg } from '../api/client';
import { useUserMutations, useUsers } from '../api/users';
import type { Role, User } from '../api/types';
import { useAuth } from '../stores/auth';

/** 四档角色（与后端 model.Role 一一对应）。 */
const ROLE_OPTIONS: {
  value: Role;
  label: string;
  desc: string;
  color?: string;
}[] = [
  { value: 'admin', label: '管理员', desc: '管理账本设置与本人账务', color: 'geekblue' },
  {
    value: 'recorder',
    label: '记账员',
    desc: '记账并可改删本人账户内流水',
    color: 'cyan',
  },
  {
    value: 'member',
    label: '普通成员',
    desc: '记账，仅改删自己创建的',
    color: 'default',
  },
  {
    value: 'viewer',
    label: '只读成员',
    desc: '只读账务，可管理本人分类和账户',
    color: 'default',
  },
];
const ROLE_LABEL: Record<string, string> = Object.fromEntries(
  ROLE_OPTIONS.map((r) => [r.value, r.label])
);
const ROLE_COLOR: Record<string, string | undefined> = Object.fromEntries(
  ROLE_OPTIONS.map((r) => [r.value, r.color])
);

interface CreateValues {
  username: string;
  password: string;
  role: Role;
}

interface ResetValues {
  password: string;
  confirm: string;
}

/**
 * 用户管理（仅管理员，specs/identity/user-management，任务 2.5）：
 * 建号 / 改角色 / 停用恢复 / 重置密码。**不提供物理删除**——历史流水要保留原创建人。
 * 最后一个启用管理员的角色与状态被锁（后端也会拒，前端提前拦，避免点到才报错）。
 */
export default function UserManager() {
  const { message } = App.useApp();
  const screens = Grid.useBreakpoint();
  const isMobile = !screens.md;
  const me = useAuth((s) => s.user);
  const users = useUsers();
  const { create, setRole, setStatus, resetPassword } = useUserMutations();

  const [createOpen, setCreateOpen] = useState(false);
  const [resetTarget, setResetTarget] = useState<User | null>(null);
  const [createForm] = Form.useForm<CreateValues>();
  const [resetForm] = Form.useForm<ResetValues>();

  const list = users.data ?? [];
  const activeAdmins = list.filter(
    (u) => u.role === 'admin' && u.status === 'active'
  ).length;

  /** 最后一个启用管理员：降级或停用都会让系统无人可管，前后端双重拦截。 */
  const isLastAdmin = (u: User) =>
    u.role === 'admin' && u.status === 'active' && activeAdmins <= 1;

  const doSetRole = async (u: User, role: Role) => {
    if (role === u.role) return;
    try {
      await setRole.mutateAsync({ id: u.id, role });
      message.success(`「${u.username}」已改为${ROLE_LABEL[role] ?? role}`);
    } catch (e) {
      message.error(errMsg(e));
    }
  };

  const doSetStatus = async (u: User, status: 'active' | 'disabled') => {
    try {
      await setStatus.mutateAsync({ id: u.id, status });
      message.success(
        status === 'disabled'
          ? `已停用「${u.username}」`
          : `已恢复「${u.username}」`
      );
    } catch (e) {
      message.error(errMsg(e));
    }
  };

  const submitCreate = async () => {
    const v = await createForm.validateFields();
    try {
      await create.mutateAsync({
        username: v.username,
        password: v.password,
        role: v.role,
      });
      message.success(`已创建「${v.username}」，可立即登录`);
      setCreateOpen(false);
    } catch (e) {
      message.error(errMsg(e));
    }
  };

  const submitReset = async () => {
    if (!resetTarget) return;
    const v = await resetForm.validateFields();
    try {
      await resetPassword.mutateAsync({
        id: resetTarget.id,
        password: v.password,
      });
      message.success(`「${resetTarget.username}」密码已重置，请转告新密码`);
      setResetTarget(null);
    } catch (e) {
      message.error(errMsg(e));
    }
  };

  /** 行操作：重置密码 + 停用/恢复（最后一个管理员被锁时直接禁用，不弹确认框）。 */
  const renderActions = (u: User) => {
    const isMe = u.id === me?.id;
    const locked = isLastAdmin(u);
    return (
      <>
        <Button
          type="link"
          size="small"
          disabled={locked}
          onClick={() => {
            setResetTarget(u);
            resetForm.resetFields();
          }}
        >
          重置密码
        </Button>
        {u.status === 'active' ? (
          locked ? (
            <Button type="link" size="small" disabled>
              停用
            </Button>
          ) : (
            <Popconfirm
              title={`停用「${u.username}」？`}
              description={
                isMe
                  ? '这是你自己，停用后立即退出登录'
                  : '该用户将无法登录，在线会话失效；历史流水保留'
              }
              okText="停用"
              okButtonProps={{ danger: true }}
              onConfirm={() => doSetStatus(u, 'disabled')}
            >
              <Button type="link" size="small" danger>
                停用
              </Button>
            </Popconfirm>
          )
        ) : (
          <Button
            type="link"
            size="small"
            onClick={() => doSetStatus(u, 'active')}
          >
            恢复
          </Button>
        )}
      </>
    );
  };

  const renderRole = (u: User) => (
    <ResponsiveSelect<Role>
      sheetTitle="角色"
      mobileSearch={false}
      size="small"
      value={u.role}
      disabled={isLastAdmin(u) || setRole.isPending}
      style={{ width: isMobile ? 140 : '100%' }}
      options={ROLE_OPTIONS.map((r) => ({
        value: r.value,
        label: r.label,
      }))}
      onChange={(v) => void doSetRole(u, v)}
    />
  );

  const columns: TableColumnsType<User> = [
    {
      title: '用户名',
      dataIndex: 'username',
      render: (v: string, u) => (
        <>
          {v}
          {u.id === me?.id && <Tag style={{ marginLeft: 8 }}>我</Tag>}
        </>
      ),
    },
    {
      title: '角色',
      dataIndex: 'role',
      width: 140,
      render: (_role: Role, u) => renderRole(u),
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 80,
      render: (s: string) =>
        s === 'active' ? <Tag color="success">启用</Tag> : <Tag>停用</Tag>,
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      width: 150,
      responsive: ['md'],
      render: (v: string) => v.slice(0, 10),
    },
    {
      title: '',
      key: 'actions',
      width: 170,
      render: (_: unknown, u: User) => renderActions(u),
    },
  ];

  return (
    <>
      <QueryBoundary query={users}>
        <Card
          size="small"
          className="panel panel--measure"
          title="用户管理"
          extra={
            <Button
              type="primary"
              size="small"
              icon={<PlusOutlined />}
              onClick={() => {
                createForm.resetFields();
                createForm.setFieldsValue({ role: 'member' });
                setCreateOpen(true);
              }}
            >
              新建用户
            </Button>
          }
        >
          {isMobile ? (
            <div className="acct-list">
              {list.map((u) => (
                <div className="acct-row" key={u.id}>
                  <div className="acct-main">
                    <div className="acct-name">
                      {u.username}
                      <Tag color={ROLE_COLOR[u.role]} style={{ marginLeft: 8 }}>
                        {ROLE_LABEL[u.role] ?? u.role}
                      </Tag>
                      {u.status === 'disabled' && <Tag>停用</Tag>}
                      {u.id === me?.id && <Tag>我</Tag>}
                    </div>
                    <div className="acct-meta">
                      创建于 {u.created_at.slice(0, 10)}
                    </div>
                  </div>
                  <div className="acct-right">
                    {renderRole(u)}
                    {renderActions(u)}
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <Table<User>
              rowKey="id"
              size="small"
              loading={users.isLoading}
              dataSource={list}
              columns={columns}
              pagination={false}
            />
          )}
          <div className="muted small" style={{ marginTop: 12 }}>
            系统不提供删除用户：历史流水需保留创建人，如需移除请用「停用」。重置密码会使该用户已登录的会话全部失效。
          </div>
        </Card>
      </QueryBoundary>

      {/* 新建用户 */}
      <SelectionScope active={createOpen}>
        <Modal
          open={createOpen}
          onCancel={() => setCreateOpen(false)}
          onOk={submitCreate}
          title="新建用户"
          okText="创建"
          confirmLoading={create.isPending}
          destroyOnHidden
        >
          <Form form={createForm} layout="vertical">
            <Form.Item
              name="username"
              label="用户名"
              rules={[
                { required: true, message: '请输入用户名' },
                { min: 2, max: 64, message: '2-64 个字符' },
              ]}
            >
              <Input placeholder="登录用，如：xiaoming" autoComplete="off" />
            </Form.Item>
            <Form.Item
              name="password"
              label="初始密码"
              rules={[
                { required: true, min: 6, max: 128, message: '至少 6 位' },
              ]}
            >
              <Input.Password
                placeholder="至少 6 位，首次登录后可重置"
                autoComplete="new-password"
              />
            </Form.Item>
            <Form.Item name="role" label="角色" rules={[{ required: true }]}>
              <ResponsiveSelect
                sheetTitle="角色"
                mobileSearch={false}
                active={createOpen}
                options={ROLE_OPTIONS.map((r) => ({
                  value: r.value,
                  label: r.label,
                }))}
              />
            </Form.Item>
          </Form>
          <div className="muted small">
            {ROLE_OPTIONS.map((r) => (
              <div key={r.value}>
                {r.label}：{r.desc}
              </div>
            ))}
          </div>
        </Modal>
      </SelectionScope>

      {/* 重置密码 */}
      <Modal
        open={!!resetTarget}
        onCancel={() => setResetTarget(null)}
        onOk={submitReset}
        title={resetTarget ? `重置「${resetTarget.username}」密码` : '重置密码'}
        okText="重置"
        confirmLoading={resetPassword.isPending}
        destroyOnHidden
      >
        <Form form={resetForm} layout="vertical">
          <Form.Item
            name="password"
            label="新密码"
            rules={[{ required: true, min: 6, max: 128, message: '至少 6 位' }]}
          >
            <Input.Password
              placeholder="至少 6 位"
              autoComplete="new-password"
            />
          </Form.Item>
          <Form.Item
            name="confirm"
            label="确认新密码"
            dependencies={['password']}
            rules={[
              { required: true, message: '请再次输入' },
              ({ getFieldValue }) => ({
                validator(_, v) {
                  if (!v || getFieldValue('password') === v)
                    return Promise.resolve();
                  return Promise.reject(new Error('两次输入不一致'));
                },
              }),
            ]}
          >
            <Input.Password
              placeholder="再输一次"
              autoComplete="new-password"
            />
          </Form.Item>
        </Form>
        <div className="muted small">
          重置后该用户已签发的 token 全部失效
          {resetTarget?.id === me?.id
            ? '——包括你自己，确认后需用新密码重新登录。'
            : '，需用新密码重新登录。'}
        </div>
      </Modal>
    </>
  );
}
