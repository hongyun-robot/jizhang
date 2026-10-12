import SelectionScope from './SelectionScope';
import AccountSelect from './AccountSelect';
import { useVisualViewportKeyboard } from '../hooks/useVisualViewportKeyboard';
import { submissionGate } from '../lib/submission';
import { useRef as submissionRef } from 'react';
import { amountError } from '../lib/money';
import { accountOptions as buildAccountOptions } from '../domain/accounts';
import { useAuth } from '../stores/auth';
import { useLayoutEffect, useMemo, useRef, useState } from 'react';
import dayjs, { type Dayjs } from 'dayjs';
import {
  Alert,
  App,
  Checkbox,
  DatePicker,
  Form,
  Grid,
  Input,
  InputNumber,
  Modal,
  Switch,
  theme,
} from 'antd';
import { useAccounts } from '../api/accounts';
import { useCategoryForest } from '../api/categories';
import { useVoucherMutations } from '../api/vouchers';
import { rootsOfKind } from '../api/category';
import { errMsg } from '../api/client';
import type { Voucher, VoucherInput } from '../api/types';
import { popupInParent } from '../lib/popup';
import CategoryPicker from './CategoryPicker';
import PlatformInput from './PlatformInput';

export function voucherToday(): Dayjs {
  return dayjs(
    new Intl.DateTimeFormat('sv-SE', {
      timeZone: 'Asia/Shanghai',
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
    }).format(new Date())
  );
}
export function voucherInputOf(v: Voucher): VoucherInput {
  return {
    name: v.name,
    platform: v.platform,
    note: v.note,
    paid_amount: v.paid_amount,
    purchase_date: v.purchase_date.slice(0, 10),
    account_id: v.account_id,
    category_id: v.category_id,
    total_units: v.total_units,
    expires_on: v.expires_on?.slice(0, 10) ?? null,
    auto_refund_enabled: v.auto_refund_enabled,
  };
}
interface Fields {
  name: string;
  platform?: string;
  note?: string;
  paid_amount: string;
  purchase_date: Dayjs;
  account_id: number;
  category_id: number;
  total_units: number;
  expires_on: Dayjs | null;
  auto_refund_enabled: boolean;
  resume_auto_refund: boolean;
}

export default function VoucherFormModal({
  open,
  onClose,
  editing,
}: {
  open: boolean;
  onClose: () => void;
  editing?: Voucher | null;
}) {
  useVisualViewportKeyboard(open);
  const submitGate = submissionRef(submissionGate());
  const [creatingAccount, setCreatingAccount] = useState(false);

  const { message } = App.useApp();
  const user = useAuth((state) => state.user);
  const { token } = theme.useToken();
  const screens = Grid.useBreakpoint();
  const [form] = Form.useForm<Fields>();
  const accounts = useAccounts();
  const forest = useCategoryForest();
  const mutations = useVoucherMutations();
  const expires = Form.useWatch('expires_on', form);
  const autoEnabled = Form.useWatch('auto_refund_enabled', form);
  const hadExpiry = useRef(false);
  const attempt = useRef<string | null>(null);
  const [creatingPlatform, setCreatingPlatform] = useState(false);
  const financialLocked = !!editing && !editing.can_edit_purchase;

  const initialized = useRef<number | undefined>(undefined);
  useLayoutEffect(() => {
    if (!open) {
      initialized.current = undefined;
      return;
    }
    const key = editing?.id ?? 0;
    if (initialized.current === key) return;
    initialized.current = key;
    form.resetFields();
    attempt.current = null;
    hadExpiry.current = !!editing?.expires_on;
    form.setFieldsValue(
      editing
        ? {
            ...voucherInputOf(editing),
            purchase_date: dayjs(editing.purchase_date.slice(0, 10)),
            expires_on: editing.expires_on
              ? dayjs(editing.expires_on.slice(0, 10))
              : null,
            resume_auto_refund: false,
          }
        : {
            name: '',
            paid_amount: '',
            purchase_date: voucherToday(),
            total_units: 1,
            expires_on: null,
            auto_refund_enabled: false,
            resume_auto_refund: false,
          }
    );
  }, [open, editing, form]);

  const accountOptions = buildAccountOptions(accounts.data, {
    ownerId: user?.id,
    history: !!editing,
    selectedId: editing?.account_id,
  });
  const categoryRoots = useMemo(
    () => rootsOfKind(forest.data, 'expense'),
    [forest.data]
  );

  const submit = () =>
    submitGate.current(async () => {
      if (creatingPlatform || creatingAccount) return;
      let values: Fields;
      try {
        values = await form.validateFields();
      } catch {
        return;
      }
      const input: VoucherInput = {
        name: values.name,
        platform: values.platform ?? '',
        note: values.note,
        paid_amount: String(values.paid_amount),
        purchase_date: values.purchase_date.format('YYYY-MM-DD'),
        account_id: values.account_id,
        category_id: values.category_id,
        total_units: values.total_units,
        expires_on: values.expires_on?.format('YYYY-MM-DD') ?? null,
        auto_refund_enabled: values.auto_refund_enabled,
        ...(values.resume_auto_refund ? { resume_auto_refund: true } : {}),
      };
      const requestKey =
        attempt.current ?? (attempt.current = crypto.randomUUID());
      try {
        if (editing)
          await mutations.update.mutateAsync({ id: editing.id, input });
        else
          await mutations.create.mutateAsync({
            ...input,
            request_key: requestKey,
          });
        message.success(
          editing ? '券信息已保存' : '购券已记录，使用时再计消费'
        );
        onClose();
      } catch (err) {
        message.error(errMsg(err));
      }
    });
  const saving = mutations.create.isPending || mutations.update.isPending;

  return (
    <SelectionScope active={open}>
      <Modal
        open={open}
        onCancel={onClose}
        title={editing ? '编辑券' : '登记购券'}
        zIndex={token.zIndexPopupBase + 200}
        width={screens.md ? 560 : '100%'}
        style={
          screens.md ? undefined : { top: 16, maxWidth: 'calc(100% - 24px)' }
        }
        styles={{
          body: { maxHeight: 'calc(100dvh - 160px)', overflowY: 'auto' },
        }}
        onOk={submit}
        confirmLoading={saving}
        okButtonProps={{ disabled: creatingPlatform || creatingAccount }}
        okText={editing ? '保存' : '登记购券'}
        cancelText="取消"
        forceRender
      >
        <Form
          form={form}
          layout="vertical"
          onSubmitCapture={(event) => {
            event.preventDefault();
            void submit();
          }}
          onValuesChange={(changed) => {
            if ('expires_on' in changed) {
              if (!changed.expires_on)
                form.setFieldsValue({
                  auto_refund_enabled: false,
                  resume_auto_refund: false,
                });
              else if (!hadExpiry.current)
                form.setFieldValue('auto_refund_enabled', true);
              hadExpiry.current = !!changed.expires_on;
            }
            if (changed.auto_refund_enabled === false)
              form.setFieldValue('resume_auto_refund', false);
          }}
          className="voucher-form"
        >
          <Form.Item
            name="name"
            label="券名称"
            rules={[
              { required: true, whitespace: true, message: '请填写券名称' },
            ]}
          >
            <Input placeholder="例如：双人餐券 / 5 次洗车券" maxLength={128} />
          </Form.Item>
          <Form.Item
            name="platform"
            label="平台 / 商家"
            rules={[
              {
                validator: (_, value?: string) =>
                  Array.from((value ?? '').trim()).length <= 128
                    ? Promise.resolve()
                    : Promise.reject(new Error('平台 / 商家名称最多 128 字')),
              },
            ]}
          >
            <PlatformInput
              key={editing?.id ?? 'new'}
              active={open}
              disabled={saving}
              onCreatingChange={setCreatingPlatform}
            />
          </Form.Item>
          {financialLocked && (
            <Alert
              type="info"
              showIcon
              title="已有核销记录或购买期已封账，购买金额等信息暂不可修改。有效期和自动退款设置仍可调整。"
            />
          )}
          <div className="voucher-form-grid">
            <Form.Item
              name="paid_amount"
              label="实付金额"
              rules={[
                { required: true, message: '请填写实付金额' },
                {
                  validator: (_, value) =>
                    !amountError(String(value))
                      ? Promise.resolve()
                      : Promise.reject(new Error('金额须大于 0，最多两位小数')),
                },
              ]}
            >
              <Input
                prefix="¥"
                inputMode="decimal"
                placeholder="0.00"
                disabled={financialLocked}
              />
            </Form.Item>
            <Form.Item
              name="total_units"
              label="可用次数"
              rules={[{ required: true, message: '请填写次数' }]}
            >
              <InputNumber
                min={1}
                max={2147483647}
                precision={0}
                style={{ width: '100%' }}
                disabled={financialLocked}
              />
            </Form.Item>
            <Form.Item
              name="purchase_date"
              label="购买日期"
              rules={[{ required: true, message: '请选择购买日期' }]}
            >
              <DatePicker
                allowClear={false}
                style={{ width: '100%' }}
                disabled={financialLocked}
                getPopupContainer={popupInParent}
              />
            </Form.Item>
            <Form.Item
              name="account_id"
              label="支付账户"
              rules={[{ required: true, message: '请选择支付账户' }]}
            >
              <AccountSelect
                onCreatingChange={setCreatingAccount}
            sheetTitle="支付账户"
                active={open}
                options={accountOptions}
                placeholder="选择账户"
                disabled={financialLocked}
                getPopupContainer={popupInParent}
              />
            </Form.Item>
          </div>
          <Form.Item
            name="category_id"
            label="消费分类"
            rules={[{ required: true, message: '请选择消费分类' }]}
          >
            <CategoryPicker roots={categoryRoots} disabled={financialLocked} />
          </Form.Item>
          <Form.Item
            name="expires_on"
            label="有效期至（可选）"
            dependencies={['purchase_date']}
            rules={[
              {
                validator: (_, value: Dayjs | null) =>
                  value &&
                  value.isBefore(form.getFieldValue('purchase_date'), 'day')
                    ? Promise.reject(new Error('有效期不能早于购买日'))
                    : Promise.resolve(),
              },
            ]}
            extra="有效期当天仍可使用，按北京时间次日零点后处理。"
          >
            <DatePicker
              style={{ width: '100%' }}
              getPopupContainer={popupInParent}
            />
          </Form.Item>
          <Form.Item label="到期自动退款" className="voucher-switch">
            <Form.Item
              name="auto_refund_enabled"
              valuePropName="checked"
              noStyle
            >
              <Switch disabled={!expires} />
            </Form.Item>
            <span className="muted">只退未用部分，并自动恢复支付账户余额</span>
          </Form.Item>
          {editing?.auto_refund_suspended && (
            <Alert
              type="info"
              showIcon
              title="自动退款已暂停"
              description="撤销后会保持暂停，确认后可重新启用。"
            />
          )}
          {editing?.auto_refund_suspended && autoEnabled && (
            <Form.Item name="resume_auto_refund" valuePropName="checked">
              <Checkbox>确认重新启用到期自动退款</Checkbox>
            </Form.Item>
          )}
          <Form.Item name="note" label="备注">
            <Input.TextArea rows={2} maxLength={255} />
          </Form.Item>
          <div className="muted">
            购买时只扣余额；核销按实付成本计消费。自动退款按填写的规则记账。
          </div>
        </Form>
      </Modal>
    </SelectionScope>
  );
}
