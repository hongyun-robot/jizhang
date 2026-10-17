import { Button, Popconfirm } from 'antd';
import { useNavigate } from 'react-router-dom';
import type { Transaction, AccountType } from '../../api/types';
import type { SessionUser } from '../../domain/identity';
import { canModifyTransaction } from '../../domain/permissions';

export default function RecordActions({
  transaction: t,
  user,
  accountTypes,
  pending,
  onEdit: openEdit,
  onRefund: openRefund,
  onDelete: doDelete,
}: {
  transaction: Transaction;
  user: SessionUser | null;
  accountTypes: ReadonlyMap<number, AccountType>;
  pending: boolean;
  onEdit: (transaction: Transaction) => void;
  onRefund: (transaction: Transaction) => void;
  onDelete: (transaction: Transaction) => Promise<void>;
}) {
  const navigate = useNavigate();
  const canEdit = (transaction: Transaction) =>
    canModifyTransaction(user, transaction);
  if (t.wealth_cash_yield_id) return <span className="row-actions"><Button type="link" size="small" onClick={() => navigate(`/wealth-cash/${t.account_id}`)}>理财零钱</Button>{user?.id === t.created_by && !t.historical_readonly && ['admin', 'recorder', 'member'].includes(user.role) && <Button type="link" size="small" onClick={() => openEdit(t)}>修改收益</Button>}</span>;
  return (
    <>
      {t.liability_account_id &&
        !t.installment_plan_id &&
        !t.liability_term_id &&
        !t.liability_operation_id && (
          <Button
            type="link"
            size="small"
            onClick={() =>
              navigate(`/liabilities/${t.liability_account_id}?source=${t.id}`)
            }
          >
            账单
          </Button>
        )}
      {t.installment_plan_id ||
      t.liability_term_id ||
      t.liability_operation_id ? (
        <Button
          type="link"
          size="small"
          onClick={() =>
            navigate(
              `/liabilities/${t.liability_account_id ?? (t.kind === 'transfer' ? t.transfer_account_id : t.account_id)}?source=${t.id}`
            )
          }
        >
          查看负债记录
        </Button>
      ) : t.wealth_position_id ? <Button type="link" size="small" onClick={() => navigate(`/wealth-positions/${t.wealth_position_id}?event=${t.wealth_event_id}`)}>查看理财</Button> : t.gold_event_id ? <Button type="link" size="small" onClick={() => navigate(`/gold/${t.purpose === 'gold_purchase' ? t.transfer_account_id : t.account_id}?event=${t.gold_event_id}`)}>查看黄金</Button> : t.fund_position_id ? <Button type="link" size="small" onClick={() => navigate(`/fund-positions/${t.fund_position_id}`)}>查看基金</Button> : t.voucher_id ? (
        <Button
          type="link"
          size="small"
          onClick={() => navigate(`/vouchers?id=${t.voucher_id}`)}
        >
          查看券
        </Button>
      ) : canEdit(t) ? (
        <span className="row-actions">
          <Button type="link" size="small" onClick={() => openEdit(t)}>
            编辑
          </Button>
          {t.kind === 'expense' && (
            <Button type="link" size="small" onClick={() => openRefund(t)}>
              退款
            </Button>
          )}
          {t.kind === 'expense' &&
            accountTypes.get(t.account_id) === 'liability' && (
              <Button
                type="link"
                size="small"
                onClick={() =>
                  navigate(`/liabilities/${t.account_id}`, {
                    state: {
                      liabilityDraft: {
                        source_txn_id: t.id,
                        entry_date: t.entry_date,
                      },
                    },
                  })
                }
              >
                转分期
              </Button>
            )}
          <Popconfirm
            title="删除这笔流水？"
            onConfirm={() => doDelete(t)}
            okButtonProps={{ loading: pending, disabled: pending }}
          >
            <Button type="link" size="small" danger>
              删除
            </Button>
          </Popconfirm>
        </span>
      ) : null}
    </>
  );
}
