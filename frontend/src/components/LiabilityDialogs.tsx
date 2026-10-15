import ResponsiveSelect from './ResponsiveSelect';
import { useVisualViewportKeyboard } from '../hooks/useVisualViewportKeyboard';
import { submissionGate } from '../lib/submission';
import { useRef as submissionRef } from 'react';
import RepaymentFields from '../features/liabilities/fields/RepaymentFields';
import AllocationFields from '../features/liabilities/fields/AllocationFields';
import ReconcileFields from '../features/liabilities/fields/ReconcileFields';
import InstallmentFields from '../features/liabilities/fields/InstallmentFields';
import AdjustmentFields from '../features/liabilities/fields/AdjustmentFields';
import SettlementFields from '../features/liabilities/fields/SettlementFields';
import ReverseFields from '../features/liabilities/fields/ReverseFields';
import type { FieldContext } from '../features/liabilities/fields/context';
import { liabilityClient } from '../api/liability-client';
import {
  allocationMode,
  liabilityCommand,
  settlementInput as makeSettlementInput,
  type LiabilityValues as Values,
} from '../features/liabilities/input';
import type { LiabilityDialog } from '../features/liabilities/dialog';
export type { LiabilityDialog } from '../features/liabilities/dialog';
import { canModifyOwnedRecord } from '../domain/permissions';
import { useEffect, useRef, useState } from 'react';
import dayjs from 'dayjs';
import {
  Alert,
  App,
  Button,
  Form,
  Input,
  InputNumber,
  Modal,
  Space,
  Table,
} from 'antd';
import { useAccounts } from '../api/accounts';
import { useCategoryForest } from '../api/categories';
import { rootsOfKind } from '../api/category';
import {
  useLiabilityWrite,
  useFeeTransactions,
  type LiabilityDetail,
  type SettlementPreview,
} from '../api/liability';
import { errMsg } from '../api/client';
import { useAuth } from '../stores/auth';
import {
  previewCents as cents,
  money,
  spreadMoney,
  previewSum as sumMoney,
} from '../lib/money';
import { plannedBills } from '../lib/liability';
import Money from './Money';
import { isAxiosError } from 'axios';

const titles: Record<LiabilityDialog['mode'], string> = {
  repay: '记录还款',
  allocation: '调整还款分配',
  reconcile: '核对账单',
  create_plan: '记录固定分期',
  edit_plan: '调整分期与费用',
  settle: '提前结清',
  repay_plan: '按期提前还款',
  reverse: '撤销并恢复记录',
};
import { amountRules } from '../features/liabilities/fields/amountRules';

export default function LiabilityDialogs({
  detail: d,
  dialog,
  onClose,
  onAddExpense,
}: {
  detail: LiabilityDetail;
  dialog: LiabilityDialog | null;
  onClose: () => void;
  onAddExpense: () => void;
}) {
  useVisualViewportKeyboard(true);
  const submitGate = submissionRef(submissionGate());
  const attempt = useRef<{ fingerprint?: string; key?: string }>({});
  const [creatingAccount, setCreatingAccount] = useState(false);
  const [creatingPlatform, setCreatingPlatform] = useState(false);
  // This component is keyed by requestKey. Refreshes replace query data but must
  // not change this editor's diff baseline, operation mode or registered fields.
  const editingBaseline = useRef(d).current;
  const operationDetail = dialog?.mode === 'edit_plan' ? editingBaseline : d;

  const [form] = Form.useForm<Values>();
  const values = Form.useWatch([], form) as Values | undefined;
  const accounts = useAccounts();
  const cats = useCategoryForest();
  const write = useLiabilityWrite();
  const { message } = App.useApp();
  const user = useAuth((s) => s.user);
  const [preview, setPreview] = useState<SettlementPreview | null>(null);
  const [previewing, setPreviewing] = useState(false);
  const [feeDates, setFeeDates] = useState<Record<number, string>>({});
  const plan = operationDetail.plans.find((p) => p.id === dialog?.planId);
  const bill = d.statements.find((b) => b.id === dialog?.billId);
  const feeCandidates = useFeeTransactions(
    plan?.fee_account_id,
    dialog?.mode === 'edit_plan'
  );
  const editable = (id: number) => canModifyOwnedRecord(user, id);
  const assets = (accounts.data ?? [])
    .filter((a) => a.type === 'asset' && (!a.asset_class || a.asset_class === 'cash') && a.status === 'active')
    .map((a) => ({ value: a.id, label: a.name }));
  const feeAccounts = [
    { value: d.account.id, label: `${d.account.name} · 计入欠款` },
    ...assets,
  ];
  const categories = rootsOfKind(cats.data, 'expense');
  const firstCategory = categories[0]?.children?.[0]?.id ?? categories[0]?.id;
  const bills = d.statements.map((b) => ({
    value: b.id,
    label: `${b.kind === 'settlement' ? '结清单' : '账单'} ${b.cycle_end} · ${b.due_date} 到期${b.issued_at ? '' : ' · 未出账'}`,
  }));
  const payableBills = bills.filter(
    (b) => d.statements.find((x) => x.id === b.value)?.issued_at
  );
  const sourceExpense = d.transactions.find(
    (t) => t.id === values?.source_txn_id
  );
  const availableExpenses = d.transactions.filter(
    (t) =>
      t.kind === 'expense' &&
      (!t.purpose || t.purpose === 'regular') &&
      !t.installment_plan_id &&
      !t.liability_term_id &&
      !t.liability_operation_id &&
      t.account_id === d.account.id &&
      editable(t.created_by)
  );
  const originalRemaining = sourceExpense
    ? sumMoney(
        d.items
          .filter((i) => i.source_id === sourceExpense.id && !i.plan_id)
          .map((i) => i.remaining)
      )
    : (values?.amount ?? '0');
  const count = values?.term_count ?? 3;
  const principalParts = spreadMoney(
    originalRemaining,
    count,
    true,
    d.account.principal_remainder ?? 'last'
  );
  const cycleRows = plannedBills(
    d.rules,
    values?.first_statement_date?.format('YYYY-MM-DD') ||
      values?.entry_date?.format('YYYY-MM-DD') ||
      dayjs().format('YYYY-MM-DD'),
    count
  );
  const feeInput = values?.fee_input ?? '0';
  const feeMode = values?.fee_mode ?? 'per_term';
  const feeParts =
    feeMode === 'total_spread'
      ? spreadMoney(feeInput, count)
      : Array.from({ length: Math.min(120, Math.max(0, count)) }, (_, i) =>
          feeMode === 'upfront' && i > 0 ? '0.00' : feeInput
        );
  const schedule = principalParts.map((amount, i) => ({
    key: i,
    number: i + 1,
    amount,
    fee: feeParts[i] ?? '0',
    close: cycleRows[i]?.close ?? '',
    due: cycleRows[i]?.due ?? '',
    feeDate:
      feeDates[i] ||
      (feeMode === 'upfront' && i === 0
        ? values?.fee_date?.format('YYYY-MM-DD')
        : cycleRows[i]?.close) ||
      '',
  }));

  useEffect(() => {
    if (!dialog) return;
    form.resetFields();
    setPreview(null);
    setFeeDates({});
    const firstAsset = assets[0]?.value;
    const initial: Values = {
      entry_date: dayjs(),
      mode: 'auto',
      targets: [],
      moves: [],
      term_count: 3,
      fee_mode: 'per_term',
      fee_input: '0',
      fee_account_id: d.account.id,
      from_account_id: firstAsset,
      first_statement_date: dayjs(),
      fee_date: dayjs(),
      reason: '',
      fee_category_id: firstCategory,
      category_id: firstCategory,
    };
    if (dialog.mode === 'create_plan' && dialog.draft) {
      Object.assign(initial, dialog.draft, {
        entry_date: dayjs(dialog.draft.entry_date),
        first_statement_date: dayjs(dialog.draft.entry_date),
      });
    }
    if (dialog.mode === 'repay')
      initial.amount =
        cents(d.summary.overdue) > 0n
          ? d.summary.overdue
          : d.summary.current_due;
    if (dialog.mode === 'edit_plan' && plan) {
      initial.fee_account_id = plan.fee_account_id;
      initial.fee_category_id = plan.fee_category_id;
    }
    if (dialog.mode === 'allocation') {
      const credit = d.credits.find((c) => c.source_id === dialog.txnId);
      if (credit) {
        initial.mode = allocationMode(credit.mode);
        try {
          initial.targets = JSON.parse(credit.targets);
        } catch {
          initial.targets = [];
        }
      }
    }
    if (dialog.mode === 'reconcile') {
      initial.reference_amount = bill?.reference_amount ?? undefined;
      initial.complete = true;
    }
    if (plan) {
      initial.version = dialog.version ?? plan.version;
      initial.fee_category_id = plan.fee_category_id;
      if (dialog.mode === 'settle' || dialog.mode === 'repay_plan') {
        initial.repayment_end = dialog.mode === 'settle' ? 0 : plan.terms.find((t) => cents(t.remaining) > 0n)?.number;
      }
      if (dialog.mode === 'edit_plan')
        initial.terms = plan.terms.map((t) => ({
          id: t.id,
          remaining_principal: t.remaining,
          statement_id: t.statement_id,
          fee_amount: t.fee_amount,
          fee_date: dayjs(t.fee_date),
        }));
    }
    form.setFieldsValue(initial);
    // Reset once for an explicit user action; keep the request key and draft on retry.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dialog, form]);
  useEffect(() => {
    if (!dialog) return;
    if (firstCategory) {
      if (!form.getFieldValue('category_id'))
        form.setFieldValue('category_id', firstCategory);
      if (!form.getFieldValue('fee_category_id'))
        form.setFieldValue('fee_category_id', firstCategory);
    }
    if (assets.length && !form.getFieldValue('from_account_id'))
      form.setFieldValue('from_account_id', assets[0].value);
  }, [dialog, cats.data, accounts.data, form]);

  const settlementInput = (value: Values) => {
    if (dialog?.mode !== 'settle' && dialog?.mode !== 'repay_plan') throw new Error('请选择分期还款操作');
    return makeSettlementInput(dialog, value, operationDetail);
  };
  const previewGeneration = useRef(0);
  const loadPreview = async () => {
    if (!plan || previewing) return;
    const generation = previewGeneration.current;
    setPreviewing(true);
    try {
      const values = await form.validateFields();
      const result = await liabilityClient.preview(
        plan.id,
        settlementInput(values),
        dialog?.mode === 'repay_plan' ? 'repay_plan' : 'settle'
      );
      if (generation !== previewGeneration.current) return;
      setPreview(result);
      form.setFieldsValue({ version: result.version, quote_signature: result.quote_signature, total_amount: result.total_amount });
    } catch (error) {
      message.error(errMsg(error));
    } finally {
      setPreviewing(false);
    }
  };
  const submit = () =>
    submitGate.current(async () => {
      if (creatingAccount || creatingPlatform) return;
      if (!dialog || write.isPending) return;
      try {
        const v = await form.validateFields();
        if ((dialog.mode === 'settle' || dialog.mode === 'repay_plan') && !preview) {
          message.info('请先核对本次还款金额');
          return;
        }
        const feeDatesInput =
          Object.keys(feeDates).length && v.fee_mode !== 'upfront'
            ? schedule.map((row) => row.feeDate)
            : undefined;
        const command = liabilityCommand(operationDetail, dialog, v, feeDatesInput);
        if (command.mode === 'settle' || command.mode === 'repay_plan') {
          const fingerprint = JSON.stringify(command.input);
          if (attempt.current.fingerprint !== fingerprint) {
            attempt.current = {
              fingerprint,
              key: attempt.current.fingerprint ? crypto.randomUUID() : dialog.requestKey,
            };
          }
          command.input.request_key = attempt.current.key ?? dialog.requestKey;
        }
        await write.mutateAsync(command);
        message.success('已保存，账单与资金记录已更新');
        onClose();
      } catch (e) {
        if (e && typeof e === 'object' && 'errorFields' in e) return;
        message.error(errMsg(e));
        if ((dialog.mode === 'settle' || dialog.mode === 'repay_plan') && isAxiosError(e) && e.response?.status === 409) {
          previewGeneration.current++;
          setPreview(null);
        }
      }
    });
  if (!dialog) return null;
  const allocatingCredit = d.credits.find((c) => c.source_id === dialog.txnId);
  let previewLeft = cents(
    dialog.mode === 'allocation' ? allocatingCredit?.amount : values?.amount
  );
  const repaymentRows: { key: number; bill: string; amount: string }[] = [];
  let allocationError = '';
  const eligible = d.statements
    .filter((b) => !!b.issued_at)
    .sort((a, b) => a.due_date.localeCompare(b.due_date) || a.id - b.id);
  const available = new Map(
    eligible.map((b) => [
      b.id,
      cents(b.remaining) +
        (allocatingCredit
          ? d.allocations
              .filter(
                (a) =>
                  a.credit_event_id === allocatingCredit.id &&
                  d.items.find((i) => i.id === a.debt_item_id)?.statement_id ===
                    b.id
              )
              .reduce((sum, a) => sum + cents(a.amount), 0n)
          : 0n),
    ])
  );
  const targets =
    values?.mode === 'manual'
      ? (values.targets ?? [])
      : eligible.map((b) => ({
            statement_id: b.id,
            amount: money(available.get(b.id) ?? 0n),
          }));
  for (const target of targets) {
    const b = eligible.find((x) => x.id === target.statement_id);
    const left = available.get(target.statement_id) ?? 0n;
    let allocated = cents(target.amount);
    if (
      values?.mode === 'manual' &&
      (allocated > previewLeft || allocated > left || allocated < 0n)
    )
      allocationError = '分配金额不能超过本次还款或账单未还额';
    if (values?.mode !== 'manual')
      allocated = allocated < previewLeft ? allocated : previewLeft;
    if (b && allocated > 0n) {
      repaymentRows.push({
        key: repaymentRows.length,
        bill: `${b.cycle_end} · ${b.due_date} 到期`,
        amount: money(allocated),
      });
      previewLeft -= allocated;
      available.set(b.id, left - allocated);
    }
  }
  const reason = (required = false) => (
    <Form.Item
      name="reason"
      label="核对 / 操作说明"
      rules={
        required
          ? [{ required: true, message: '请填写核对依据或操作原因' }]
          : undefined
      }
    >
      <Input.TextArea maxLength={255} rows={2} />
    </Form.Item>
  );
  const allocationFields = (
    <>
      <Form.Item name="mode" label="还款分配">
        <ResponsiveSelect
          sheetTitle="还款分配"
          mobileSearch={false}
          options={[
            { value: 'auto', label: '先还最早到期账单' },
            { value: 'manual', label: '手动指定账单' },
          ]}
        />
      </Form.Item>
      {values?.mode === 'manual' && (
        <Form.List name="targets">
          {(fields, { add, remove }) => (
            <>
              {fields.map((f) => (
                <Space
                  key={f.key}
                  align="baseline"
                  className="liability-form-row"
                >
                  <Form.Item
                    name={[f.name, 'statement_id']}
                    rules={[{ required: true }]}
                  >
                    <ResponsiveSelect
                      sheetTitle="指定账单"
                      aria-label="指定账单"
                      placeholder="指定账单"
                      options={payableBills}
                      style={{ minWidth: 190 }}
                    />
                  </Form.Item>
                  <Form.Item
                    name={[f.name, 'amount']}
                    rules={[{ required: true }, ...amountRules]}
                  >
                    <Input
                      aria-label="分配金额"
                      placeholder="金额"
                      inputMode="decimal"
                    />
                  </Form.Item>
                  <Button onClick={() => remove(f.name)}>移除</Button>
                </Space>
              ))}
              <Button onClick={() => add()}>添加账单分配</Button>
            </>
          )}
        </Form.List>
      )}
      <p className="small muted">
        还款金额须全部用于实际欠款。分期必须连同手续费整期还清，本金还款不计入收支和预算。
      </p>
      {allocationError ? (
        <Alert type="warning" title={allocationError} />
      ) : (
        <>
          <Table
            size="small"
            pagination={false}
            dataSource={repaymentRows}
            columns={[
              { title: '本次分配预览', dataIndex: 'bill' },
              {
                title: '抵扣金额',
                render: (_, r) => <Money value={r.amount} />,
              },
            ]}
          />
          <p>
            {previewLeft > 0n ? '金额超过当前账单可偿付范围；提前偿还未来期次请从分期计划操作。' : '提交时核对最新账单及完整分期期次金额。'}
          </p>
        </>
      )}
    </>
  );
  const context: FieldContext = {
    saving: write.isPending,
    setCreatingAccount,
    setCreatingPlatform,
    d,
    dialog,
    plan,
    bill,
    values,
    form,
    assets,
    feeAccounts,
    categories,
    bills,
    availableExpenses,
    sourceExpense,
    originalRemaining,
    feeMode,
    feeParts,
    schedule,
    setFeeDates,
    reason,
    allocationFields,
    onAddExpense,
    editable,
    feeCandidates,
    preview,
    previewing,
    loadPreview,
  };
  return (
    <Modal
      open
      title={titles[dialog.mode]}
      onCancel={onClose}
      onOk={submit}
      confirmLoading={write.isPending}
      okButtonProps={{ disabled: creatingAccount || creatingPlatform || ((dialog.mode === 'settle' || dialog.mode === 'repay_plan') && (!preview || previewing)) }}
      width={
        dialog.mode === 'create_plan' || dialog.mode === 'edit_plan' ? 820 : 620
      }
      styles={{ body: { maxHeight: '65dvh', overflowY: 'auto' } }}
      okText={dialog.mode === 'settle' ? '确认结清' : dialog.mode === 'repay_plan' ? '确认还款' : '保存'}
    >
      <Form
        name="liability-operation"
        form={form}
        layout="vertical"
        disabled={write.isPending}
        onValuesChange={() => {
          if (dialog.mode === 'settle' || dialog.mode === 'repay_plan') {
            previewGeneration.current++;
            setPreview(null);
          }
        }}
      >
        <Form.Item name="version" hidden>
          <InputNumber />
        </Form.Item>
        <Form.Item name="quote_signature" hidden><Input /></Form.Item>
        <Form.Item name="total_amount" hidden><Input /></Form.Item>
        {dialog.mode === 'repay' && <RepaymentFields context={context} />}
        {dialog.mode === 'allocation' && <AllocationFields context={context} />}
        {dialog.mode === 'reconcile' && <ReconcileFields context={context} />}
        {dialog.mode === 'create_plan' && (
          <InstallmentFields context={context} />
        )}
        {dialog.mode === 'edit_plan' && plan && (
          <AdjustmentFields context={context} />
        )}
        {(dialog.mode === 'settle' || dialog.mode === 'repay_plan') && plan && (
          <SettlementFields context={context} />
        )}
        {dialog.mode === 'reverse' && <ReverseFields context={context} />}
      </Form>
    </Modal>
  );
}
