import { useEffect, useState } from 'react';
import dayjs from 'dayjs';
import { Segmented, DatePicker } from 'antd';
import type { DateRange } from '../api/types';
import { matchPreset, presetRange, type PresetKey } from '../lib/dates';
export { presetRange } from '../lib/dates';
export type { PresetKey } from '../lib/dates';

const presets = [
  { value: 'thisMonth', label: '本月' },
  { value: 'lastMonth', label: '上月' },
  { value: 'last7', label: '近7天' },
  { value: 'last30', label: '近30天' },
  { value: 'custom', label: '自定义' },
];
export default function PeriodPicker({
  value,
  onChange,
}: {
  value: DateRange;
  onChange: (value: DateRange) => void;
}) {
  const [customOpen, setCustomOpen] = useState(false);
  useEffect(() => setCustomOpen(false), [value.start, value.end]);
  const preset = customOpen ? 'custom' : matchPreset(value);
  return (
    <div className="period-picker">
      <Segmented
        size="small"
        value={preset}
        options={presets}
        onChange={(selection) => {
          const key = selection as PresetKey;
          setCustomOpen(key === 'custom');
          if (key !== 'custom') onChange(presetRange(key));
        }}
      />
      {preset === 'custom' && (
        <DatePicker.RangePicker
          size="small"
          allowClear={false}
          value={[dayjs(value.start), dayjs(value.end)]}
          onChange={(range) => {
            if (range?.[0] && range[1])
              onChange({
                start: range[0].format('YYYY-MM-DD'),
                end: range[1].format('YYYY-MM-DD'),
              });
          }}
        />
      )}
    </div>
  );
}
