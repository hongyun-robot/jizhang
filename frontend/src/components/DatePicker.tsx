import { useSyncExternalStore } from 'react';
import dayjs, { type Dayjs } from 'dayjs';
import {
  DatePicker as AntDatePicker,
  Grid,
  type DatePickerProps,
  type GetProps,
} from 'antd';

const touchQuery = '(hover: none) and (pointer: coarse)';
const subscribeToTouch = (notify: () => void) => {
  const query = window.matchMedia(touchQuery);
  query.addEventListener('change', notify);
  return () => query.removeEventListener('change', notify);
};
const isTouch = () => window.matchMedia(touchQuery).matches;

function useKeyboardFreeDateInput() {
  const touch = useSyncExternalStore(subscribeToTouch, isTouch, () => false);
  return !Grid.useBreakpoint().md || touch;
}

function DatePicker<ValueType = Dayjs, IsMultiple extends boolean = false>(
  props: DatePickerProps<ValueType, IsMultiple>
) {
  const inputReadOnly = useKeyboardFreeDateInput();
  return (
    <AntDatePicker<ValueType, IsMultiple>
      {...props}
      inputReadOnly={inputReadOnly || props.inputReadOnly}
    />
  );
}

function RangePicker(props: GetProps<typeof AntDatePicker.RangePicker>) {
  const inputReadOnly = useKeyboardFreeDateInput();
  return (
    <AntDatePicker.RangePicker
      {...props}
      inputReadOnly={inputReadOnly || props.inputReadOnly}
    />
  );
}

/** 字符串日期字段沿用 YYYY-MM-DD 值，和其他表单共享日历交互。 */
type DateInputProps = Omit<
  DatePickerProps<Dayjs, false>,
  'value' | 'defaultValue' | 'onChange'
> & {
  value?: string | null;
  onChange?: (value: string) => void;
};

export function DateInput({ value, onChange, ...props }: DateInputProps) {
  return (
    <DatePicker
      {...props}
      style={{ width: '100%', ...props.style }}
      value={value ? dayjs(value) : null}
      onChange={(date) => onChange?.(date?.format('YYYY-MM-DD') ?? '')}
    />
  );
}

export default Object.assign(DatePicker, { RangePicker });
