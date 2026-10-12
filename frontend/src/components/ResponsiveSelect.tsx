import { useEffect, useRef, useState, type ReactNode, type Ref } from 'react';
import { ConfigProvider, Grid, Select, type SelectProps } from 'antd';
import SelectionSheet, {
  SelectionTrigger,
  type SheetOption,
} from './SelectionSheet';
import { useSelectionActive } from './SelectionScope';

export type ResponsiveSelectProps<Value extends string | number> = Omit<
  SelectProps<Value>,
  'options' | 'value' | 'onChange' | 'onSelect' | 'allowClear'
> & {
  value?: Value | null;
  options?: SheetOption<Value>[];
  sheetTitle: string;
  mobileSearch?: boolean;
  active?: boolean;
  errorMessage?: ReactNode;
  onRetry?: () => void;
  onCreate?: (name: string) => void;
  suspended?: boolean;
  closeSignal?: number;
  ref?: Ref<HTMLButtonElement>;
} & (
    | {
        allowClear?: false;
        onChange?: (value: Value, option?: SheetOption<Value>) => void;
      }
    | {
        allowClear: NonNullable<SelectProps<Value>['allowClear']>;
        onChange?: (
          value: Value | undefined,
          option?: SheetOption<Value>
        ) => void;
      }
  );

export default function ResponsiveSelect<Value extends string | number>({
  sheetTitle,
  mobileSearch = true,
  active: requestedActive = true,
  errorMessage,
  onRetry,
  onCreate,
  suspended = false,
  closeSignal = 0,
  ref,
  options = [],
  ...props
}: ResponsiveSelectProps<Value>) {
  const active = useSelectionActive(requestedActive);
  const desktop = !!Grid.useBreakpoint().md;
  const { componentDisabled } = ConfigProvider.useConfig();
  const disabled = props.disabled ?? componentDisabled;
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const trigger = useRef<HTMLButtonElement>(null);
  const previousLabel = useRef<{ value: Value; label: ReactNode } | null>(null);
  const selected = options.find((option) => option.value === props.value);
  if (selected && props.value != null)
    previousLabel.current = { value: props.value, label: selected.label };
  const label =
    props.value == null
      ? undefined
      : (selected?.label ??
        (previousLabel.current?.value === props.value
          ? previousLabel.current.label
          : String(props.value)));
  const close = () => {
    setOpen(false);
    setQuery('');
  };
  const createName = query.trim();
  const createAction = onCreate && !disabled ? (
    <button type="button" className="selection-sheet-item is-create" onClick={() => onCreate(createName)}>
      {createName && Array.from(createName).length <= 64 ? `创建「${createName}」` : '新建账户'}
    </button>
  ) : undefined;
  useEffect(() => {
    if (!active || desktop || disabled) close();
  }, [active, desktop, disabled]);
  useEffect(close, [closeSignal]);
  if (desktop)
    return (
      <Select
        {...props}
        options={options}
        open={open && active && !suspended}
        onOpenChange={setOpen}
        onSearch={(search) => { setQuery(search); props.onSearch?.(search); }}
        popupRender={createAction ? (menu) => <>{menu}{createAction}</> : props.popupRender}
        onChange={(value, option) =>
          { props.onChange?.(value, Array.isArray(option) ? option[0] : option); close(); }
        }
      />
    );
  const search = query.trim().toLowerCase();
  const filtered = options.filter((option) =>
    (option.searchText ?? String(option.label ?? option.value))
      .toLowerCase()
      .includes(search)
  );
  return (
    <>
      <SelectionTrigger
        id={props.id}
        label={label}
        placeholder={props.placeholder ?? `选择${sheetTitle}`}
        disabled={disabled}
        open={open && active}
        onClick={() => {
          if (active) setOpen(true);
        }}
        style={props.style}
        className={props.className}
        status={
          props.status === 'error' || props.status === 'warning'
            ? props.status
            : undefined
        }
        ariaLabel={props['aria-label']}
        describedBy={props['aria-describedby']}
        invalid={props['aria-invalid']}
        required={props['aria-required']}
        triggerRef={(node) => {
          trigger.current = node;
          if (typeof ref === 'function') ref(node);
          else if (ref) ref.current = node;
        }}
      />
      <SelectionSheet<Value>
        open={open && active && !disabled && !suspended}
        title={`选择${sheetTitle}`}
        value={props.value ?? undefined}
        currentLabel={label}
        options={filtered}
        trigger={trigger}
        query={query}
        onSearch={mobileSearch ? setQuery : undefined}
        loading={props.loading}
        error={errorMessage}
        onRetry={onRetry}
        actions={createAction}
        onClose={close}
        allowClear={!!props.allowClear}
        onClear={() => {
          props.onClear?.();
          if (props.allowClear) props.onChange?.(undefined, undefined);
          close();
        }}
        onSelect={(value) => {
          const option = options.find((row) => row.value === value);
          if (!option || option.disabled || disabled) return;
          props.onChange?.(value, option);
          close();
        }}
      />
    </>
  );
}
