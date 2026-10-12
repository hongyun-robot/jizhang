import { useVisualViewportKeyboard } from '../hooks/useVisualViewportKeyboard';
import { popupInParent } from '../lib/popup';
import { submissionGate } from '../lib/submission';
import { useRef as submissionRef } from 'react';
import { useEffect, useRef } from 'react';
import dayjs from 'dayjs';
import {
  Alert,
  App,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Switch,
} from 'antd';
import { useAccountMutations } from '../api/accounts';
import { useLiabilityTypes, type LiabilityDetail } from '../api/liability';
import { errMsg } from '../api/client';
import { plannedBills, ruleError } from '../lib/liability';

interface Values {
  name: string;
  liability_type: string;
  statement_day: number;
  repayment_day: number;
  repayment_month_offset: number;
  status: boolean;
}
export default function PersonalLiabilityAccountModal({
  detail,
  onClose,
  onCreated,
}: {
  detail?: LiabilityDetail;
  onClose: () => void;
  onCreated: (id: number) => void;
}) {
  useVisualViewportKeyboard(true);
  const submitGate = submissionRef(submissionGate());
  const [form] = Form.useForm<Values>();
  const values = Form.useWatch([], form) as Values | undefined;
  const types = useLiabilityTypes();
  const { create, update } = useAccountMutations();
  const { message } = App.useApp();
  const last = detail?.rules.at(-1);
  const initialized = useRef<number | null>(null);
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  useEffect(() => {
    const key = detail?.account.id ?? 0;
    if (initialized.current !== key) {
      initialized.current = key;
      form.resetFields();
      form.setFieldsValue({
        name: detail?.account.name ?? '',
        liability_type: detail?.account.liability_type || types.data?.[0]?.code,
        statement_day: last?.statement_day ?? 31,
        repayment_day: last?.repayment_day ?? 10,
        repayment_month_offset: last?.repayment_month_offset ?? 1,
        status: detail?.account.status !== 'disabled',
      });
    }
    if (
      !form.getFieldValue('liability_type') &&
      !form.isFieldTouched('liability_type') &&
      types.data?.[0]
    )
      form.setFieldValue('liability_type', types.data[0].code);
  }, [detail, types.data, form, last]);
  const invalid = ruleError(
    values?.statement_day,
    values?.repayment_day,
    values?.repayment_month_offset
  );
  const today = dayjs().format('YYYY-MM-DD');
  const current = plannedBills(detail?.rules ?? [], today, 1)[0];
  const effective =
    detail?.account.billing_enabled && current
      ? dayjs(current.close).add(1, 'day').format('YYYY-MM-DD')
      : today;
  const preview =
    !invalid && values
      ? plannedBills(
          [
            {
              id: 0,
              version: 1,
              effective_from: effective,
              statement_day: values.statement_day,
              repayment_day: values.repayment_day,
              repayment_month_offset: values.repayment_month_offset,
            },
          ],
          effective,
          3
        )
      : [];
  const submit = () =>
    submitGate.current(async () => {
      const key = initialized.current;
      try {
        const v = await form.validateFields();
        const error = ruleError(
          v.statement_day,
          v.repayment_day,
          v.repayment_month_offset
        );
        if (error) {
          message.warning(error);
          return;
        }
        const input = {
          name: v.name,
          type: 'liability' as const,
          opening_balance: '0',
          liability_type: v.liability_type,
          statement_day: v.statement_day,
          repayment_day: v.repayment_day,
          repayment_month_offset: v.repayment_month_offset,
          status: v.status ? ('active' as const) : ('disabled' as const),
          sort: detail?.account.sort ?? 10,
        };
        if (
          detail?.account.billing_enabled &&
          last &&
          last.statement_day === v.statement_day &&
          last.repayment_day === v.repayment_day &&
          last.repayment_month_offset === v.repayment_month_offset &&
          detail.account.liability_type === v.liability_type
        ) {
          Object.assign(input, {
            statement_day: undefined,
            repayment_month_offset: undefined,
            repayment_day: detail.account.repayment_day ?? undefined,
          });
        }
        const account = detail
          ? await update.mutateAsync({ id: detail.account.id, input })
          : await create.mutateAsync(input);
        if (!alive.current || initialized.current !== key) return;
        message.success(detail ? '此账户设置已更新' : '已创建我的负债账户');
        onClose();
        if (!detail) onCreated(account.id);
      } catch (e) {
        if (e && typeof e === 'object' && 'errorFields' in e) return;
        message.error(errMsg(e));
      }
    });
  return (
    <Modal
      open
      title={detail ? '账户设置' : '新增负债账户'}
      onCancel={onClose}
      onOk={submit}
      okText="保存"
      confirmLoading={create.isPending || update.isPending}
      styles={{ body: { maxHeight: '65dvh', overflowY: 'auto' } }}
    >
      <p className="small muted">
        归属于当前登录账号。同一类型可以创建多个账户，分别设置账单日和还款日。
      </p>
      <Form name="personal-liability" form={form} layout="vertical">
        <Form.Item
          name="name"
          label="账户名称"
          rules={[{ required: true, max: 64 }]}
        >
          <Input placeholder="如：招商信用卡（尾号1234）、我的花呗" />
        </Form.Item>
        <Form.Item
          name="liability_type"
          label="消费信用类型"
          rules={[{ required: true }]}
        >
          <Select
            loading={types.isLoading}
            options={(types.data ?? []).map((t) => ({
              value: t.code,
              label: t.name,
            }))}
            getPopupContainer={popupInParent}
          />
        </Form.Item>
        <Form.Item
          name="statement_day"
          label="账单日（1–31）"
          rules={[{ required: true }]}
        >
          <InputNumber min={1} max={31} style={{ width: '100%' }} />
        </Form.Item>
        <Form.Item
          name="repayment_month_offset"
          label="还款月份"
          rules={[{ required: true }]}
        >
          <Select
            options={[
              { value: 0, label: '出账当月' },
              { value: 1, label: '出账次月' },
            ]}
            getPopupContainer={popupInParent}
          />
        </Form.Item>
        <Form.Item
          name="repayment_day"
          label="还款日（1–31）"
          rules={[{ required: true }]}
        >
          <InputNumber min={1} max={31} style={{ width: '100%' }} />
        </Form.Item>
        {detail && (
          <Form.Item name="status" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
        )}
      </Form>
      {invalid ? (
        <Alert type="warning" title={invalid} />
      ) : (
        <p className="small muted">
          {preview.map((p) => `${p.close} 出账 → ${p.due} 还款`).join('；')}
          。短月份取月底。
          {detail?.account.billing_enabled &&
            `当前和历史账单保留，新日期自 ${effective} 生效。`}
        </p>
      )}
    </Modal>
  );
}
