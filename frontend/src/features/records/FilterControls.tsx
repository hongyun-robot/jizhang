import ResponsiveSelect from '../../components/ResponsiveSelect';
import { Button, Input, InputNumber, Segmented, Tag } from 'antd';
import type { TxnKind } from '../../api/types';
import type { FilterState } from './filters';
import { categoryPath as catPathOf } from '../../domain/categories';
import PeriodPicker from '../../components/PeriodPicker';
import ResponsiveCategorySelect from '../../components/ResponsiveCategorySelect';
import { useEffect, useRef, useState } from 'react';
import { useNavigationType } from 'react-router-dom';
interface FilterControlsProps {
  filters: FilterState;
  patch: (p: Partial<FilterState>) => void;
  accountOptions: { value: number; label: string }[];
  catOptions: {
    value: number;
    label: string;
    children?: { value: number; label: string }[];
  }[];
  /** 传入即渲染「清除」按钮（移动端抽屉用；桌面 filter-bar 保持既有布局不加按钮）。 */
  onClear?: () => void;
  active?: boolean;
}
export default function FilterControls({
  filters,
  patch,
  accountOptions,
  catOptions,
  onClear,
  active = true,
}: FilterControlsProps) {
  const [keyword, setKeyword] = useState(filters.keyword);
  const navigation = useNavigationType();
  const pending = useRef(new Set<string>());
  const requested = useRef(filters.keyword);
  useEffect(() => {
    if (navigation !== 'POP' && pending.current.has(filters.keyword)) {
      if (filters.keyword === requested.current) pending.current.clear();
      return;
    }
    pending.current.clear();
    setKeyword(filters.keyword);
  }, [filters.keyword, navigation]);
  return (
    <>
      <PeriodPicker
        value={filters.range}
        onChange={(range) => patch({ range })}
      />
      <Segmented
        size="small"
        value={filters.kind}
        options={[
          { value: '', label: '全部' },
          { value: 'expense', label: '支出' },
          { value: 'income', label: '收入' },
          { value: 'transfer', label: '转账' },
          { value: 'refund', label: '退款' },
        ]}
        onChange={(v) => patch({ kind: v as TxnKind | '' })}
      />
      {filters.related_txn_id ? (
        <Tag
          color="green"
          onClose={() => patch({ related_txn_id: undefined })}
          closable
        >
          关联退款 #{filters.related_txn_id}
        </Tag>
      ) : null}
      <ResponsiveSelect
        sheetTitle="账户"
        active={active}
        size="small"
        allowClear
        placeholder="账户"
        style={{ minWidth: 110 }}
        value={filters.account_id}
        options={accountOptions}
        onChange={(v) => patch({ account_id: v })}
      />
      <ResponsiveCategorySelect
        active={active}
        value={catPathOf(catOptions, filters.category_id)}
        options={catOptions}
        onChange={(v) =>
          patch({
            category_id: v.length ? (v[v.length - 1] as number) : undefined,
          })
        }
      />
      <Input
        size="small"
        allowClear
        placeholder="备注 / 平台 / 商家"
        style={{ width: 130 }}
        value={keyword}
        onChange={(e) => {
          const value = e.target.value;
          requested.current = value;
          pending.current.add(value);
          setKeyword(value);
          patch({ keyword: value });
        }}
      />
      <span className="amount-range">
        <InputNumber
          size="small"
          placeholder="金额≥"
          style={{ width: 96 }}
          min={0}
          value={filters.amount_min ? Number(filters.amount_min) : null}
          onChange={(v) =>
            patch({
              amount_min: v !== null && v !== undefined ? String(v) : undefined,
            })
          }
        />
        <span className="sep">–</span>
        <InputNumber
          size="small"
          placeholder="金额≤"
          style={{ width: 96 }}
          min={0}
          value={filters.amount_max ? Number(filters.amount_max) : null}
          onChange={(v) =>
            patch({
              amount_max: v !== null && v !== undefined ? String(v) : undefined,
            })
          }
        />
      </span>
      {onClear ? (
        <Button size="small" onClick={onClear}>
          清除
        </Button>
      ) : null}
    </>
  );
}
