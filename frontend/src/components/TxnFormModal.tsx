import { useEffect, useMemo, useState } from 'react';
import dayjs, { type Dayjs } from 'dayjs';
import { Alert, App, Button, DatePicker, Grid, Input, Modal, Segmented, Select, Tag } from 'antd';

import { useAccounts, useCategoryForest, useCreateTxn, useUpdateTxn } from '../api/hooks';
import { rootsOfKind } from '../api/category';
import type { TxnKind, Transaction } from '../api/types';
import { errMsg } from '../api/client';
import { useAuth } from '../stores/auth';
import VoucherFormModal from './VoucherFormModal';
import VoucherPlatformInput from './VoucherPlatformInput';

const KIND_OPTIONS = [
  { value: 'expense', label: '支出' },
  { value: 'income', label: '收入' },
  { value: 'transfer', label: '转账' },
  { value: 'refund', label: '退款' },
];

const QUICK_AMOUNTS = [10, 20, 50, 100];

/** 行内「退款」入口的预填（specs/refunds 分类与账户继承 + 超额提示所需上下文）。 */
export interface TxnRefill {
  kind: TxnKind;
  related_txn_id?: number;
  account_id?: number;
  category_id?: number;
  /** 原支出金额（超额黄条计算） */
  related_amount?: string;
  /** 原支出已退累计 */
  refunded?: number;
  platform?: string;
}

interface Props {
  open: boolean;
  onClose: () => void;
  /** 传入流水则为编辑模式，否则为记一笔 */
  editing?: Transaction | null;
  /** 行内「退款」等入口的预填（仅新建模式生效） */
  refill?: TxnRefill | null;
}

/**
 * 记一笔表单（specs/transactions 记账四态 + ui-shell 数字键盘与常用金额快捷键）：
 * - 支出/收入/转账/退款四态；金额用系统数字键盘（inputMode=decimal）
 * - 支出/收入选两级分类（先选一级再选子分类，无子分类的一级可直选）；退款分类可留空
 * - 转账双账户且无分类；日期默认今天
 * - 退款可关联原支出（related_txn_id），累计超原支出时黄条提示、二次确认提交
 */
export default function TxnFormModal({ open, onClose, editing, refill }: Props) {
  const { message } = App.useApp();
  const screens = Grid.useBreakpoint();
  const isMobile = !screens.md;

  const accounts = useAccounts();
  const forest = useCategoryForest();
  const createTxn = useCreateTxn();
  const updateTxn = useUpdateTxn();
  const user = useAuth((s) => s.user);
  // 退款是入账动作，只读角色（viewer）不显示该选项（specs/refunds 权限约束）
  const kindOptions =
    user?.role === 'viewer' ? KIND_OPTIONS.filter((o) => o.value !== 'refund') : KIND_OPTIONS;

  const [kind, setKind] = useState<TxnKind>('expense');
  const [amount, setAmount] = useState('');
  const [accountId, setAccountId] = useState<number | undefined>();
  const [transferId, setTransferId] = useState<number | undefined>();
  const [categoryId, setCategoryId] = useState<number | undefined>();
  const [activeRootId, setActiveRootId] = useState<number | undefined>();
  const [date, setDate] = useState<Dayjs>(() => dayjs());
  const [note, setNote] = useState('');
  const [platform, setPlatform] = useState('');
  const [creatingPlatform, setCreatingPlatform] = useState(false);
  // 退款关联上下文：关联 id / 原支出金额 / 已退累计 / 超额二次确认
  const [relatedId, setRelatedId] = useState<number | undefined>();
  const [relatedAmount, setRelatedAmount] = useState<string | undefined>();
  const [refundedBase, setRefundedBase] = useState(0);
  const [overAck, setOverAck] = useState(false);
  const [voucherOpen, setVoucherOpen] = useState(false);

  // 打开时初始化（编辑回填 / 行内退款预填 / 新建默认今天）
  useEffect(() => {
    if (!open) return;
    setOverAck(false);
    if (editing) {
      setKind(editing.kind);
      setAmount(editing.amount);
      setAccountId(editing.account_id);
      setTransferId(editing.transfer_account_id ?? undefined);
      setCategoryId(editing.category_id ?? undefined);
      setDate(dayjs(editing.entry_date));
      setNote(editing.note ?? '');
      setPlatform(editing.platform ?? '');
      setRelatedId(editing.related_txn_id ?? undefined);
      setRelatedAmount(undefined);
      setRefundedBase(0);
    } else if (refill) {
      setKind(refill.kind);
      setAmount('');
      setAccountId(refill.account_id);
      setTransferId(undefined);
      setCategoryId(refill.category_id);
      setActiveRootId(undefined);
      setDate(dayjs());
      setNote('');
      setPlatform(refill.platform ?? '');
      setRelatedId(refill.related_txn_id);
      setRelatedAmount(refill.related_amount);
      setRefundedBase(refill.refunded ?? 0);
    } else {
      setKind('expense');
      setAmount('');
      setAccountId(undefined);
      setTransferId(undefined);
      setCategoryId(undefined);
      setDate(dayjs());
      setNote('');
      setPlatform('');
      setRelatedId(undefined);
      setRelatedAmount(undefined);
      setRefundedBase(0);
    }
  }, [open, editing, refill]);

  const activeAccounts = useMemo(
    () => (accounts.data ?? []).filter((a) => a.status === 'active'),
    [accounts.data],
  );

  const roots = useMemo(
    () => rootsOfKind(forest.data, kind === 'income' ? 'income' : 'expense'),
    [forest.data, kind],
  );

  // 当前一级分类：由已选子分类推导，否则取第一个
  const activeRoot = useMemo(() => {
    if (activeRootId) return roots.find((r) => r.id === activeRootId) ?? roots[0];
    if (categoryId) {
      const parent = roots.find((r) => r.children?.some((c) => c.id === categoryId));
      if (parent) return parent;
      const self = roots.find((r) => r.id === categoryId);
      if (self) return self;
    }
    return roots[0];
  }, [roots, activeRootId, categoryId]);

  const leaves = useMemo(() => {
    if (!activeRoot) return [];
    return activeRoot.children?.length ? activeRoot.children : [activeRoot];
  }, [activeRoot]);

  const pickRoot = (id: number) => {
    setActiveRootId(id);
    const root = roots.find((r) => r.id === id);
    if (root && !root.children?.length) setCategoryId(root.id);
  };

  const setQuick = (v: number) => {
    const base = Number(amount) || 0;
    setAmount(String(Number((base + v).toFixed(2))));
  };

  const validate = (): string | null => {
    if (Array.from(platform.trim()).length > 128) return '平台 / 商家名称最多 128 字';
    if (!/^\d+(\.\d{1,2})?$/.test(amount) || Number(amount) <= 0) {
      return '请输入大于 0 的金额，最多两位小数';
    }
    if (!accountId) return '请选择账户';
    if (kind === 'transfer') {
      if (!transferId) return '请选择转入账户';
      if (transferId === accountId) return '转出与转入账户不能相同';
    } else if (kind !== 'refund' && !categoryId) {
      return '请选择分类';
    }
    return null;
  };

  // 超额黄条：累计退款（已退 + 本次）超过原支出（specs/refunds 超额仅提示不拦截）
  const overTotal =
    relatedId && relatedAmount && Number(amount) > 0
      ? refundedBase + Number(amount)
      : null;
  const isOver = overTotal !== null && relatedAmount !== undefined && overTotal > Number(relatedAmount);

  const submit = async () => {
    if (creatingPlatform || createTxn.isPending || updateTxn.isPending) return;
    const err = validate();
    if (err) {
      message.warning(err);
      return;
    }
    if (isOver && !overAck) {
      setOverAck(true);
      message.warning(
        `累计退款 ¥${overTotal} 已超过原支出 ¥${relatedAmount}，如确认请再次点击提交`,
      );
      return;
    }
    const input = {
      kind,
      amount,
      account_id: accountId!,
      ...(kind === 'transfer'
        ? { transfer_account_id: transferId }
        : { category_id: categoryId }),
      ...(kind === 'refund' && relatedId ? { related_txn_id: relatedId } : {}),
      entry_date: date.format('YYYY-MM-DD'),
      note: note || undefined,
      platform: platform.trim(),
    };
    try {
      if (editing) {
        await updateTxn.mutateAsync({ id: editing.id, input });
        message.success('已保存修改');
      } else {
        await createTxn.mutateAsync(input);
        message.success('已记一笔');
      }
      onClose();
    } catch (e) {
      message.error(errMsg(e));
    }
  };

  const saving = createTxn.isPending || updateTxn.isPending;
  const accountOptions = activeAccounts.map((a) => ({
    value: a.id,
    label: `${a.name}${a.type === 'liability' ? '（信用）' : ''}`,
  }));

  return (
    <>
    <Modal
      open={open}
      onCancel={onClose}
      title={editing ? '编辑流水' : '记一笔'}
      width={isMobile ? '100%' : 520}
      style={isMobile ? { top: 0, maxWidth: '100%', paddingBottom: 0 } : undefined}
      styles={{ body: { maxHeight: `calc(100dvh - ${isMobile ? 160 : 240}px)`, overflowY: 'auto' } }}
      footer={[
        <Button key="cancel" onClick={onClose}>
          取消
        </Button>,
        <Button key="ok" type="primary" loading={saving} disabled={creatingPlatform} onClick={submit}>
          {editing ? '保存' : '记账'}
        </Button>,
      ]}
    >
      <div className="txn-form">
        {!editing && user?.role !== 'viewer' && <Button onClick={() => { setVoucherOpen(true); onClose(); }}>购券：先付款，使用时计消费</Button>}
        <Segmented
          block
          value={kind}
          options={kindOptions}
          onChange={(v) => {
            setKind(v as TxnKind);
            setCategoryId(undefined);
            setActiveRootId(undefined);
            setOverAck(false);
            if (v !== 'refund') {
              setRelatedId(undefined);
              setRelatedAmount(undefined);
              setRefundedBase(0);
            }
          }}
        />

        {/* 金额：大字 + 系统数字键盘 + 常用金额快捷键 */}
        <div className="amount-row">
          <span className="amount-symbol">¥</span>
          <Input
            value={amount}
            onChange={(e) => setAmount(e.target.value.replace(/[^\d.]/g, ''))}
            inputMode="decimal"
            placeholder="0.00"
            size="large"
            className="amount-input"
            autoFocus
            // 金额框回车即记账：桌面端免去一次「再找按钮」的视线跳跃
            onPressEnter={submit}
          />
        </div>
        <div className="quick-amounts">
          {QUICK_AMOUNTS.map((v) => (
            <Button key={v} size="small" onClick={() => setQuick(v)}>
              +{v}
            </Button>
          ))}
          <Button size="small" onClick={() => setAmount('')}>
            清零
          </Button>
        </div>

        {/* 分类（转账无分类；退款分类可留空） */}
        {kind !== 'transfer' && roots.length > 0 && (
          <div className="cat-pick">
            <div className="pick-label">分类{kind === 'refund' ? '（可留空）' : ''}</div>
            <div className="cat-roots">
              {roots.map((r) => (
                <Tag.CheckableTag
                  key={r.id}
                  checked={activeRoot?.id === r.id}
                  onChange={() => pickRoot(r.id)}
                >
                  {r.name}
                </Tag.CheckableTag>
              ))}
            </div>
            {leaves.length > 0 && <div className="pick-label">子分类</div>}
            <div className="cat-leaves">
              {leaves.map((c) => (
                <Tag.CheckableTag
                  key={c.id}
                  checked={categoryId === c.id}
                  onChange={() => setCategoryId(c.id)}
                >
                  {c.name}
                </Tag.CheckableTag>
              ))}
              {kind === 'refund' && (
                <Tag.CheckableTag
                  checked={!categoryId}
                  onChange={() => {
                    setCategoryId(undefined);
                    setActiveRootId(undefined);
                  }}
                >
                  未分类
                </Tag.CheckableTag>
              )}
            </div>
          </div>
        )}

        <div className="txn-platform-field">
          <label className="pick-label" htmlFor="txn-platform">平台 / 商家</label>
          <VoucherPlatformInput
            key={editing?.id ?? 'new'}
            id="txn-platform"
            value={platform}
            onChange={setPlatform}
            active={open}
            disabled={saving}
            onCreatingChange={setCreatingPlatform}
          />
        </div>

        {/* 账户 */}
        {kind === 'transfer' ? (
          <div className="transfer-row">
            <Select
              placeholder="转出账户"
              options={accountOptions}
              value={accountId}
              onChange={setAccountId}
              style={{ flex: 1 }}
            />
            <span className="transfer-arrow">→</span>
            <Select
              placeholder="转入账户"
              options={accountOptions.filter((o) => o.value !== accountId)}
              value={transferId}
              onChange={setTransferId}
              style={{ flex: 1 }}
            />
          </div>
        ) : (
          <Select
            placeholder="账户"
            options={accountOptions}
            value={accountId}
            onChange={setAccountId}
            style={{ width: '100%' }}
          />
        )}

        {/* 退款关联信息 + 超额黄条（累计可超原支出，仅提示需二次确认） */}
        {kind === 'refund' && relatedId && (
          <div className="refund-related">
            已关联原支出 #{relatedId}
            {relatedAmount ? `（原 ¥${relatedAmount}，已退 ¥${refundedBase}）` : ''}
          </div>
        )}
        {kind === 'refund' && isOver && relatedAmount !== undefined && overTotal !== null && (
          <Alert
            type="warning"
            showIcon
            className="refund-over"
            message={`累计退款 ¥${overTotal} 超过原支出 ¥${relatedAmount}（本次 ¥${amount}），如确认请再次点击「${editing ? '保存' : '记账'}」`}
          />
        )}

        <DatePicker
          value={date}
          onChange={(v) => v && setDate(v)}
          style={{ width: '100%' }}
          allowClear={false}
        />

        <Input.TextArea
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder="备注（可选）"
          maxLength={255}
          rows={2}
        />
      </div>
    </Modal>
    <VoucherFormModal open={voucherOpen} onClose={() => setVoucherOpen(false)} />
    </>
  );
}
