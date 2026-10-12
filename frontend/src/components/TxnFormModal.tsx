import SelectionScope from './SelectionScope';
import AccountSelect from './AccountSelect';
import { submissionGate } from '../lib/submission';
import { useEditorSession } from '../hooks/useEditorSession';
import { useRef as submissionRef } from 'react';
import CategoryPicker from './CategoryPicker';
import { accountOptions as buildAccountOptions } from '../domain/accounts';
import { amountError } from '../lib/money';
import { useEffect, useMemo, useState } from 'react';
import dayjs, { type Dayjs } from 'dayjs';
import {
  Alert,
  App,
  Button,
  DatePicker,
  Grid,
  Input,
  Modal,
  Segmented,
} from 'antd';

import { useAccounts } from '../api/accounts';
import { useCategoryForest } from '../api/categories';
import { useCreateTxn, useUpdateTxn } from '../api/transactions';
import { rootsOfKind } from '../api/category';
import type { TxnKind, Transaction } from '../api/types';
import { errMsg } from '../api/client';
import { useAuth } from '../stores/auth';
import { canRecord } from '../domain/permissions';
import { useVisualViewportKeyboard } from '../hooks/useVisualViewportKeyboard';
import VoucherFormModal from './VoucherFormModal';
import PlatformInput from './PlatformInput';
import { useNavigate } from 'react-router-dom';
import { useLiabilityDetail } from '../api/liability';
import { plannedBills } from '../lib/liability';
import { previewCents as cents, money } from '../lib/money';
import { popupInParent } from '../lib/popup';

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
  refunded?: string;
  platform?: string;
  installment_plan_id?: number;
  liability_account_id?: number;
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
export default function TxnFormModal({
  open,
  onClose,
  editing,
  refill,
}: Props) {
  const submitGate = submissionRef(submissionGate());
  const [creatingAccount, setCreatingAccount] = useState(false);
  const editor = useEditorSession(
    open,
    editing
      ? `edit:${editing.id}`
      : refill
        ? `refill:${refill.kind}:${refill.related_txn_id ?? 0}`
        : 'new'
  );
  const close = () => {
    editor.close();
    onClose();
  };

  const { message } = App.useApp();
  const navigate = useNavigate();
  const screens = Grid.useBreakpoint();
  const isMobile = !screens.md;
  useVisualViewportKeyboard(open);

  const accounts = useAccounts();
  const forest = useCategoryForest();
  const createTxn = useCreateTxn();
  const updateTxn = useUpdateTxn();
  const user = useAuth((s) => s.user);
  // 退款是入账动作，只读角色（viewer）不显示该选项（specs/refunds 权限约束）
  const kindOptions =
    user?.role === 'viewer'
      ? KIND_OPTIONS.filter((o) => o.value !== 'refund')
      : KIND_OPTIONS;

  const [kind, setKind] = useState<TxnKind>('expense');
  const [amount, setAmount] = useState('');
  const [accountId, setAccountId] = useState<number | undefined>();
  const [transferId, setTransferId] = useState<number | undefined>();
  const [categoryId, setCategoryId] = useState<number | undefined>();
  const [date, setDate] = useState<Dayjs>(() => dayjs());
  const [note, setNote] = useState('');
  const [platform, setPlatform] = useState('');
  const [creatingPlatform, setCreatingPlatform] = useState(false);
  // 退款关联上下文：关联 id / 原支出金额 / 已退累计 / 超额二次确认
  const [relatedId, setRelatedId] = useState<number | undefined>();
  const [relatedAmount, setRelatedAmount] = useState<string | undefined>();
  const [refundedBase, setRefundedBase] = useState('0');
  const [overAck, setOverAck] = useState(false);
  const [voucherOpen, setVoucherOpen] = useState(false);
  // 内联校验：提交失败后才提示（specs/ui-shell 记账表单内联校验），Alert 与字段错误态同步展示
  const [showErrors, setShowErrors] = useState(false);
  const creditAccount = (accounts.data ?? []).find(
    (a) =>
      a.id === (kind === 'transfer' ? transferId : accountId) &&
      a.type === 'liability'
  );
  const liability = useLiabilityDetail(creditAccount?.id ?? 0);
  const managed =
    !!editing &&
    !!(
      editing.installment_plan_id ||
      editing.liability_term_id ||
      editing.liability_operation_id
    );
  const cycle = plannedBills(
    liability.data?.rules ?? [],
    date.format('YYYY-MM-DD'),
    1
  )[0];

  // 打开时初始化（编辑回填 / 行内退款预填 / 新建默认今天）
  useEffect(() => {
    if (!open) return;
    setOverAck(false);
    setShowErrors(false);
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
      setRefundedBase('0');
    } else if (refill) {
      setKind(refill.kind);
      setAmount('');
      setAccountId(refill.account_id);
      setTransferId(undefined);
      setCategoryId(refill.category_id);
      setDate(dayjs());
      setNote('');
      setPlatform(refill.platform ?? '');
      setRelatedId(refill.related_txn_id);
      setRelatedAmount(refill.related_amount);
      setRefundedBase(refill.refunded ?? '0');
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
      setRefundedBase('0');
    }
  }, [open, editing, refill]);

  const roots = useMemo(
    () => rootsOfKind(forest.data, kind === 'income' ? 'income' : 'expense'),
    [forest.data, kind]
  );

  const setQuick = (v: number) => {
    setAmount(money(cents(amount) + BigInt(v) * 100n));
  };

  const validate = (): string | null => {
    if (Array.from(platform.trim()).length > 128)
      return '平台 / 商家名称最多 128 字';
    if (amountError(amount)) {
      return '请输入大于 0 的金额，最多两位小数';
    }
    if (!accountId) return '请选择账户';
    if (creditAccount && !creditAccount.billing_enabled)
      return '请在我的负债账户中设置此账户的账单日和还款日';
    if (kind === 'transfer') {
      if (!transferId) return '请选择转入账户';
      if (transferId === accountId) return '转出与转入账户不能相同';
    } else if (kind !== 'refund' && !categoryId) {
      return '请选择分类';
    }
    return null;
  };

  // 提交后才展示的内联错误：Alert 文案 + 逐字段错误态（status="error"）
  const fieldErr = showErrors ? validate() : null;
  const amountInvalid = !!amountError(amount);

  // 超额黄条：累计退款（已退 + 本次）超过原支出（specs/refunds 超额仅提示不拦截）
  const overTotal =
    relatedId && relatedAmount && cents(amount) > 0n
      ? cents(String(refundedBase)) + cents(amount)
      : null;
  const isOver =
    overTotal !== null &&
    relatedAmount !== undefined &&
    overTotal > cents(relatedAmount);

  const submit = () =>
    submitGate.current(async () => {
      const isCurrent = editor.capture();
      if (!isCurrent()) return;
      if (!canRecord(user?.role)) return;
      if (creatingPlatform || creatingAccount || createTxn.isPending || updateTxn.isPending)
        return;
      if (managed) return;
      const err = validate();
      if (err) {
        setShowErrors(true); // 内联提示出错项，表单不关闭
        return;
      }
      if (isOver && !overAck) {
        setOverAck(true);
        message.warning(
          `累计退款 ¥${money(overTotal!)} 已超过原支出 ¥${relatedAmount}，如确认请再次点击提交`
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
        ...(kind === 'refund' && relatedId
          ? { related_txn_id: relatedId }
          : {}),
        entry_date: date.format('YYYY-MM-DD'),
        note: note || undefined,
        platform: platform.trim(),
      };
      try {
        if (editing) {
          await updateTxn.mutateAsync({ id: editing.id, input });
          if (!isCurrent()) return;
          message.success('已保存修改');
        } else {
          await createTxn.mutateAsync(input);
          if (!isCurrent()) return;
          message.success('已记一笔');
        }
        close();
        if (
          kind === 'refund' &&
          refill?.installment_plan_id &&
          refill.liability_account_id
        ) {
          message.info('退款已入账，请核对剩余分期期次；手续费按实际退款处理');
          navigate(
            `/liabilities/${refill.liability_account_id}?plan=${refill.installment_plan_id}`
          );
        }
      } catch (e) {
        if (isCurrent()) message.error(errMsg(e));
      }
    });

  const saving = createTxn.isPending || updateTxn.isPending;
  const accountOptions = buildAccountOptions(accounts.data, {
    ownerId: user?.id,
    history: !!editing,
    selectedIds: [accountId, transferId].flatMap((id) =>
      id === undefined ? [] : [id]
    ),
  });
  const startPlan = () => {
    const error = validate();
    if (error) {
      setShowErrors(true);
      return;
    }
    close();
    navigate(`/liabilities/${creditAccount!.id}`, {
      state: {
        liabilityDraft: {
          amount,
          entry_date: date.format('YYYY-MM-DD'),
          category_id: categoryId,
          note,
          platform: platform.trim(),
          source_txn_id: editing?.id,
        },
      },
    });
  };

  return (
    <>
      <SelectionScope active={open}>
        <Modal
          open={open}
          onCancel={close}
          title={editing ? '编辑流水' : '记一笔'}
          width={isMobile ? '100%' : 520}
          style={
            isMobile
              ? { top: 0, maxWidth: '100%', paddingBottom: 0 }
              : undefined
          }
          styles={{
            body: {
              maxHeight: `calc(100dvh - ${isMobile ? 160 : 240}px)`,
              overflowY: 'auto',
            },
          }}
          footer={[
            <Button key="cancel" onClick={close}>
              取消
            </Button>,
            <Button
              key="ok"
              type="primary"
              loading={saving}
              disabled={creatingPlatform || creatingAccount || managed}
              onClick={submit}
            >
              {editing ? '保存' : '记账'}
            </Button>,
          ]}
        >
          <div className="txn-form">
            {/* body 顶部受控错误 Alert：提交未通过 validate() 时出现，修好后自动消失，Modal 不关闭 */}
            {fieldErr && (
              <Alert
                type="error"
                showIcon
                message={fieldErr}
                className="txn-form-error"
              />
            )}
            {managed && (
              <Alert
                type="info"
                title="此流水由负债计划管理"
                description={
                  <Button
                    type="link"
                    onClick={() => {
                      close();
                      navigate(
                        `/liabilities/${editing!.liability_account_id ?? editing!.account_id}`
                      );
                    }}
                  >
                    查看计划、费用与修正入口
                  </Button>
                }
              />
            )}
            {!editing && user?.role !== 'viewer' && (
              <Button
                onClick={() => {
                  setVoucherOpen(true);
                  close();
                }}
              >
                购券：先付款，使用时计消费
              </Button>
            )}
            <Segmented
              block
              value={kind}
              options={kindOptions}
              onChange={(v) => {
                setKind(v as TxnKind);
                setCategoryId(undefined);
                setOverAck(false);
                if (v !== 'refund') {
                  setRelatedId(undefined);
                  setRelatedAmount(undefined);
                  setRefundedBase('0');
                }
              }}
            />

            {/* 金额：大字 + 系统数字键盘 + 常用金额快捷键 */}
            <div
              className={`amount-row${showErrors && amountInvalid ? ' amount-error' : ''}`}
            >
              <span className="amount-symbol">¥</span>
              <Input
                value={amount}
                onChange={(e) =>
                  setAmount(e.target.value.replace(/[^\d.]/g, ''))
                }
                inputMode="decimal"
                placeholder="0.00"
                size="large"
                className="amount-input"
                status={showErrors && amountInvalid ? 'error' : undefined}
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
              <CategoryPicker
                key={String(open) + String(editing?.id) + kind}
                roots={roots}
                value={categoryId}
                onChange={setCategoryId}
                allowEmpty={kind === 'refund'}
                error={showErrors && kind !== 'refund' && !categoryId}
                heading={kind === 'refund' ? '分类（可留空）' : '分类'}
              />
            )}

            {creditAccount && (
              <Alert
                type={creditAccount.billing_enabled ? 'info' : 'warning'}
                title={
                  creditAccount.billing_enabled
                    ? kind === 'transfer'
                      ? '转入负债账户会记录还款'
                      : kind === 'refund'
                        ? '同账户退款优先抵扣原未还本金'
                        : '消费时计支出，下月还款记转账'
                    : '此账户尚未配置账单规则'
                }
                description={
                  <>
                    {cycle && kind === 'expense' && (
                      <p>
                        归属 {cycle.close} 账单，{cycle.due}{' '}
                        到期。还款本金不会再计支出。
                      </p>
                    )}
                    {kind === 'transfer' && (
                      <p>
                        先还最早到期账单，超额保留待抵扣，可在负债详情调整分配。
                      </p>
                    )}
                    {!managed &&
                      kind === 'expense' &&
                      creditAccount.billing_enabled &&
                      user?.role !== 'viewer' && (
                        <Button onClick={startPlan}>
                          {editing
                            ? '将这笔消费转为固定分期'
                            : '填写期次和手续费，记固定分期'}
                        </Button>
                      )}
                  </>
                }
              />
            )}

            <div className="txn-platform-field">
              <label className="pick-label" htmlFor="txn-platform">
                平台 / 商家
              </label>
              <PlatformInput
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
                <AccountSelect
                  onCreatingChange={setCreatingAccount}
            sheetTitle="转出账户"
                  active={open}
                  showSearch
                  optionFilterProp="label"
                  placeholder="转出账户"
                  options={accountOptions}
                  value={accountId}
                  onChange={setAccountId}
                  style={{ flex: 1 }}
                  status={showErrors && !accountId ? 'error' : undefined}
                  getPopupContainer={popupInParent}
                />
                <span className="transfer-arrow">→</span>
                <AccountSelect
                  onCreatingChange={setCreatingAccount}
            sheetTitle="转入账户"
                  active={open}
                  showSearch
                  optionFilterProp="label"
                  placeholder="转入账户"
                  options={accountOptions.filter((o) => o.value !== accountId)}
                  value={transferId}
                  onChange={setTransferId}
                  style={{ flex: 1 }}
                  status={showErrors && !transferId ? 'error' : undefined}
                  getPopupContainer={popupInParent}
                />
              </div>
            ) : (
              <AccountSelect
                onCreatingChange={setCreatingAccount}
            sheetTitle="账户"
                active={open}
                showSearch
                optionFilterProp="label"
                placeholder="账户"
                options={accountOptions}
                value={accountId}
                onChange={setAccountId}
                style={{ width: '100%' }}
                status={showErrors && !accountId ? 'error' : undefined}
                getPopupContainer={popupInParent}
              />
            )}

            {/* 退款关联信息 + 超额黄条（累计可超原支出，仅提示需二次确认） */}
            {kind === 'refund' && relatedId && (
              <div className="refund-related">
                已关联原支出 #{relatedId}
                {relatedAmount
                  ? `（原 ¥${relatedAmount}，已退 ¥${refundedBase}）`
                  : ''}
              </div>
            )}
            {kind === 'refund' &&
              isOver &&
              relatedAmount !== undefined &&
              overTotal !== null && (
                <Alert
                  type="warning"
                  showIcon
                  className="refund-over"
                  message={`累计退款 ¥${money(overTotal)} 超过原支出 ¥${relatedAmount}（本次 ¥${amount}），如确认请再次点击「${editing ? '保存' : '记账'}」`}
                />
              )}

            <DatePicker
              value={date}
              onChange={(v) => v && setDate(v)}
              style={{ width: '100%' }}
              allowClear={false}
              getPopupContainer={popupInParent}
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
      </SelectionScope>
      <VoucherFormModal
        open={voucherOpen}
        onClose={() => setVoucherOpen(false)}
      />
    </>
  );
}
