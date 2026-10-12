import {
  useEffect,
  useRef,
  useState,
  type ReactNode,
  type Ref,
  type CSSProperties,
  type AriaAttributes,
} from 'react';
import { Button, Drawer, Input } from 'antd';
import {
  CheckOutlined,
  DownOutlined,
  LoadingOutlined,
} from '@ant-design/icons';
import { useVisualViewportKeyboard } from '../hooks/useVisualViewportKeyboard';

export interface SheetOption<Value> {
  value: Value;
  label: ReactNode;
  disabled?: boolean;
  searchText?: string;
}

export function SelectionTrigger({
  id,
  label,
  placeholder,
  disabled,
  open,
  onClick,
  style,
  className = '',
  status,
  triggerRef,
  ariaLabel,
  describedBy,
  invalid,
  required,
}: {
  id?: string;
  label?: ReactNode;
  placeholder?: ReactNode;
  disabled?: boolean;
  open: boolean;
  onClick: () => void;
  style?: CSSProperties;
  className?: string;
  status?: 'error' | 'warning';
  triggerRef?: Ref<HTMLButtonElement>;
  ariaLabel?: string;
  describedBy?: string;
  invalid?: AriaAttributes['aria-invalid'];
  required?: AriaAttributes['aria-required'];
}) {
  return (
    <button
      type="button"
      id={id}
      ref={triggerRef}
      disabled={disabled}
      className={`selection-trigger ${className}${status ? ` is-${status}` : ''}`}
      style={style}
      aria-label={ariaLabel}
      aria-describedby={describedBy}
      aria-invalid={invalid ?? (status === 'error' || undefined)}
      aria-required={required}
      aria-haspopup="dialog"
      aria-expanded={open}
      onClick={onClick}
    >
      <span
        className={`selection-trigger-label${label == null ? ' is-placeholder' : ''}`}
      >
        {label ?? placeholder}
      </span>
      <DownOutlined aria-hidden />
    </button>
  );
}

export default function SelectionSheet<Value extends string | number>({
  open,
  title,
  value,
  currentLabel,
  options,
  onSelect,
  onClose,
  query = '',
  onSearch,
  searchPlaceholder = '搜索选项',
  loading = false,
  error,
  onRetry,
  allowClear,
  onClear,
  busy = false,
  actions,
  navigation,
  trigger,
  className = '',
  emptyText,
}: {
  open: boolean;
  title: string;
  value?: Value;
  currentLabel?: ReactNode;
  options: SheetOption<Value>[];
  onSelect: (value: Value) => void;
  onClose: () => void;
  query?: string;
  onSearch?: (query: string) => void;
  searchPlaceholder?: string;
  loading?: boolean;
  error?: ReactNode;
  onRetry?: () => void;
  allowClear?: boolean;
  onClear?: () => void;
  busy?: boolean;
  actions?: ReactNode;
  navigation?: ReactNode;
  trigger?: { current: HTMLButtonElement | null };
  className?: string;
  emptyText?: ReactNode;
}) {
  useVisualViewportKeyboard(open);
  const heading = useRef<HTMLSpanElement>(null);
  const wasOpen = useRef(false);
  const [inputFocused, setInputFocused] = useState(false);
  const [viewport, setViewport] = useState({
    height: window.innerHeight,
    top: 0,
  });
  useEffect(() => {
    const closing = wasOpen.current && !open;
    wasOpen.current = open;
    if (!closing) return;
    const frame = requestAnimationFrame(() => {
      const button = trigger?.current;
      if (
        button?.isConnected &&
        button.getClientRects().length &&
        !button.disabled
      )
        button.focus({ preventScroll: true });
    });
    return () => cancelAnimationFrame(frame);
  }, [open, trigger]);
  useEffect(() => {
    if (!open) {
      setInputFocused(false);
      return;
    }
    let frame = 0;
    const measure = () => {
      frame = 0;
      setViewport({
        height: window.visualViewport?.height ?? window.innerHeight,
        top: window.visualViewport?.offsetTop ?? 0,
      });
    };
    const schedule = () => {
      if (!frame) frame = requestAnimationFrame(measure);
    };
    measure();
    window.addEventListener('resize', schedule);
    window.visualViewport?.addEventListener('resize', schedule);
    window.visualViewport?.addEventListener('scroll', schedule);
    return () => {
      cancelAnimationFrame(frame);
      window.removeEventListener('resize', schedule);
      window.visualViewport?.removeEventListener('resize', schedule);
      window.visualViewport?.removeEventListener('scroll', schedule);
    };
  }, [open]);
  const countHeight =
    138 +
    (onSearch ? 52 : 0) +
    (currentLabel != null ? 32 : 0) +
    options.length * 48 +
    (actions ? 96 : 0) +
    (navigation ? 44 : 0);
  const height = Math.max(
    120,
    Math.min(
      viewport.height - 12,
      inputFocused
        ? viewport.height - 12
        : Math.min(viewport.height * 0.64, countHeight)
    )
  );
  return (
    <Drawer
      open={open}
      title={
        <span ref={heading} tabIndex={-1}>
          {title}
        </span>
      }
      placement="bottom"
      size={height}
      push={false}
      destroyOnHidden
      rootClassName={`selection-sheet ${className}`}
      onClose={onClose}
      rootStyle={{ top: viewport.top, bottom: 'auto', height: viewport.height }}
      focusable={{ trap: true, focusTriggerAfterClose: false }}
      afterOpenChange={(visible) => {
        if (visible) heading.current?.focus({ preventScroll: true });
        else if (
          trigger?.current?.isConnected &&
          trigger.current.getClientRects().length &&
          !trigger.current.disabled
        )
          trigger.current.focus({ preventScroll: true });
      }}
      onKeyDown={(event) => {
        // Portaled events otherwise reach the parent form / modal.
        event.stopPropagation();
        if (event.key === 'Escape') {
          event.preventDefault();
          onClose();
        }
        if (event.key === 'Enter' && event.target instanceof HTMLInputElement)
          event.preventDefault();
      }}
    >
      {currentLabel != null && (
        <div className="selection-current">当前选择：{currentLabel}</div>
      )}
      {onSearch && (
        <Input
          allowClear
          value={query}
          placeholder={searchPlaceholder}
          aria-label={searchPlaceholder}
          onChange={(event) => onSearch(event.target.value)}
          onFocus={() => setInputFocused(true)}
          onBlur={() => setInputFocused(false)}
        />
      )}
      {error && (
        <div role="status" className="selection-error">
          {error}
          {onRetry && (
            <Button type="link" onClick={onRetry}>
              重试
            </Button>
          )}
        </div>
      )}
      {navigation}
      <div className="selection-sheet-list">
        {allowClear && value != null && (
          <button
            type="button"
            className="selection-sheet-item"
            disabled={busy}
            onClick={onClear}
          >
            清空{title.replace(/^选择/, '')}
          </button>
        )}
        {loading && (
          <div className="selection-sheet-empty" role="status">
            <LoadingOutlined /> 正在加载选项…
          </div>
        )}
        {options.map((option) => (
          <button
            type="button"
            key={`${typeof option.value}:${option.value}`}
            disabled={busy || option.disabled}
            className={`selection-sheet-item${option.value === value ? ' is-selected' : ''}`}
            aria-pressed={option.value === value}
            onClick={() => onSelect(option.value)}
          >
            <span>{option.label}</span>
            {option.value === value && <CheckOutlined aria-hidden />}
          </button>
        ))}
        {!options.length && !loading && !error && (
          <div className="selection-sheet-empty" role="status">
            {emptyText ?? (query.trim() ? '无匹配选项' : '暂无可选项')}
          </div>
        )}
        {actions && <div className="selection-sheet-actions">{actions}</div>}
      </div>
    </Drawer>
  );
}
