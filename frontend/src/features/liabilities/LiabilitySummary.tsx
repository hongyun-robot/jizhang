import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Button, Empty, Skeleton } from 'antd';
import { PlusOutlined, CreditCardOutlined } from '@ant-design/icons';
import { useLiabilities, useLiabilityTypes } from '../../api/liability';
import { QueryBoundary } from '../../components/QueryState';
import PersonalLiabilityAccountModal from '../../components/PersonalLiabilityAccountModal';
import Money from '../../components/Money';
import { sumMoney } from '../../lib/money';
import '../../styles/liabilities.css';
export default function LiabilitySummary() {
  const navigate = useNavigate();
  const list = useLiabilities();
  const types = useLiabilityTypes();
  const labels: Record<string, string> = Object.fromEntries(
    (types.data ?? []).map((item) => [item.code, item.name])
  );
  const [accountForm, setAccountForm] = useState(false);
  return (
    <div className="page">
      <div className="page-head">
        <div>
          <div className="eyebrow">MY CREDIT ACCOUNTS</div>
          <h2>我的负债账户</h2>
          <p className="small muted">
            仅本人可查看和管理，每个账户独立设置还款日期。
          </p>
        </div>
        <Button
          type="primary"
          icon={<PlusOutlined />}
          onClick={() => setAccountForm(true)}
        >
          新增负债账户
        </Button>
      </div>
      {accountForm && (
        <PersonalLiabilityAccountModal
          onClose={() => setAccountForm(false)}
          onCreated={(id) => navigate(`/liabilities/${id}`)}
        />
      )}
      <QueryBoundary query={list}>
        {!list.data ? (
          <Skeleton active />
        ) : list.data.length === 0 ? (
          <Empty description="尚未创建消费信用账户" />
        ) : (
          <>
            <div className="liability-summary">
              <div>
                <span>总欠款</span>
                <Money value={sumMoney(list.data.map((a) => a.total_debt))} />
              </div>
              <div>
                <span>最近应还合计</span>
                <Money value={sumMoney(list.data.map((a) => a.current_due))} />
              </div>
              <div>
                <span>逾期应还</span>
                <Money
                  value={sumMoney(list.data.map((a) => a.overdue))}
                  tone="expense"
                />
              </div>
            </div>
            <div className="liability-accounts">
              {list.data.map((a) => (
                <button
                  type="button"
                  className="liability-account-row"
                  key={a.account_id}
                  onClick={() => navigate(`/liabilities/${a.account_id}`)}
                >
                  <span>
                    <CreditCardOutlined /> <strong>{a.name}</strong>
                    <small>
                      {labels[a.liability_type] || '消费信用'} ·{' '}
                      {a.billing_enabled
                        ? a.due_date
                          ? `${a.due_date} 到期`
                          : '暂无已出账应还'
                        : '待配置账单规则'}
                    </small>
                  </span>
                  <span>
                    <small>
                      总欠款
                    </small>
                    <Money
                      value={a.total_debt}
                    />
                    <small>
                      本期应还 <Money value={a.current_due} />
                    </small>
                  </span>
                </button>
              ))}
            </div>
          </>
        )}
      </QueryBoundary>
    </div>
  );
}
