import dayjs, { type Dayjs } from 'dayjs';

import { Segmented, DatePicker } from 'antd';
import { useEffect, useState } from 'react';

import type { DateRange } from '../api/types';

const { RangePicker } = DatePicker;

export type PresetKey = 'thisMonth' | 'lastMonth' | 'last7' | 'last30' | 'custom';

export function presetRange(key: Exclude<PresetKey, 'custom'>): DateRange {
  const now = dayjs();
  switch (key) {
    case 'thisMonth':
      return {
        start: now.startOf('month').format('YYYY-MM-DD'),
        end: now.endOf('month').format('YYYY-MM-DD'),
      };
    case 'lastMonth': {
      const p = now.subtract(1, 'month');
      return {
        start: p.startOf('month').format('YYYY-MM-DD'),
        end: p.endOf('month').format('YYYY-MM-DD'),
      };
    }
    case 'last7':
      return { start: now.subtract(6, 'day').format('YYYY-MM-DD'), end: now.format('YYYY-MM-DD') };
    case 'last30':
      return { start: now.subtract(29, 'day').format('YYYY-MM-DD'), end: now.format('YYYY-MM-DD') };
  }
}

function matchPreset(value: DateRange): Exclude<PresetKey, 'custom'> | 'custom' {
  const keys: Exclude<PresetKey, 'custom'>[] = ['thisMonth', 'lastMonth', 'last7', 'last30'];
  for (const k of keys) {
    const r = presetRange(k);
    if (r.start === value.start && r.end === value.end) return k;
  }
  return 'custom';
}

const PRESET_LABELS: Array<{ value: PresetKey; label: string }> = [
  { value: 'thisMonth', label: '本月' },
  { value: 'lastMonth', label: '上月' },
  { value: 'last7', label: '近7天' },
  { value: 'last30', label: '近30天' },
  { value: 'custom', label: '自定义' },
];

/**
 * 共享区间选择器：流水列表与统计页 MUST 使用同一组件与同一区间语义
 * （specs/transactions 自由日期区间筛选）。
 */
export default function PeriodPicker({
  value,
  onChange,
}: {
  value: DateRange;
  onChange: (v: DateRange) => void;
}) {
  const [preset, setPreset] = useState<PresetKey>(() => matchPreset(value));
  const [custom, setCustom] = useState<[Dayjs, Dayjs] | null>(null);

  // 外部值变化（如切回预设）时同步内部自定义状态
  useEffect(() => {
    setPreset(matchPreset(value));
  }, [value.start, value.end]);

  const handlePreset = (key: PresetKey) => {
    setPreset(key);
    if (key === 'custom') {
      setCustom([dayjs(value.start), dayjs(value.end)]);
      return;
    }
    onChange(presetRange(key));
  };

  return (
    <div className="period-picker">
      <Segmented
        size="small"
        value={preset}
        options={PRESET_LABELS}
        onChange={(v) => handlePreset(v as PresetKey)}
      />
      {preset === 'custom' && (
        <RangePicker
          size="small"
          allowClear={false}
          value={custom}
          onChange={(v) => {
            if (v && v[0] && v[1]) {
              const range = {
                start: v[0].format('YYYY-MM-DD'),
                end: v[1].format('YYYY-MM-DD'),
              };
              setCustom([v[0], v[1]]);
              onChange(range);
            }
          }}
        />
      )}
    </div>
  );
}
