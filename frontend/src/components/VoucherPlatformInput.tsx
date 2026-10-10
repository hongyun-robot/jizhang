import { useEffect, useRef, useState } from 'react';
import { App, AutoComplete, Button, Input } from 'antd';
import { DownOutlined, LoadingOutlined } from '@ant-design/icons';
import { useCreateVoucherPlatform, useVoucherPlatforms } from '../api/hooks';
import { errMsg } from '../api/client';

interface Props {
  id?: string;
  value?: string;
  onChange?: (value: string) => void;
  active: boolean;
  disabled?: boolean;
  onCreatingChange: (creating: boolean) => void;
}
interface Option {
  value: string;
  label: string;
  create?: boolean;
  disabled?: boolean;
}

export default function VoucherPlatformInput({
  id, value = '', onChange, active, disabled, onCreatingChange,
}: Props) {
  const { message } = App.useApp();
  const platforms = useVoucherPlatforms(active);
  const create = useCreateVoucherPlatform();
  const [expanded, setExpanded] = useState(false);
  const [search, setSearch] = useState<string | null>(null);
  const [popupWidth, setPopupWidth] = useState(0);
  const container = useRef<HTMLDivElement>(null);
  const generation = useRef(0);
  const currentValue = useRef(value);
  const inFlight = useRef(false);
  currentValue.current = value;

  useEffect(() => {
    generation.current++;
    setExpanded(false);
    setSearch(null);
    return () => { generation.current++; };
  }, [active]);
  useEffect(() => {
    onCreatingChange(active && create.isPending);
  }, [active, create.isPending, onCreatingChange]);
  useEffect(() => {
    if (!active || !container.current) return;
    const element = container.current;
    const measure = () => setPopupWidth(element.clientWidth);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, [active]);

  const change = (next: string) => {
    currentValue.current = next;
    onChange?.(next);
  };
  const name = value.trim();
  const rows = platforms.data ?? [];
  const query = (search ?? '').trim().toLowerCase();
  const options: Option[] = rows
    .filter((row) => row.name.toLowerCase().includes(query))
    .map((row) => ({ value: row.name, label: row.name }));
  if (name && Array.from(name).length <= 128 && !rows.some((row) => row.name === name)) {
    options.push({
      value: name,
      label: create.isPending ? `正在创建「${name}」…` : `创建「${name}」`,
      create: true,
      disabled: create.isPending,
    });
  }
  if (!options.length) {
    options.push({
      value: '',
      label: platforms.isFetching
        ? '正在加载选项…'
        : name && Array.from(name).length > 128
          ? '名称最多 128 字'
          : '输入平台 / 商家名称可创建',
      disabled: true,
    });
  }

  const select = async (selected: string, option: Option) => {
    setExpanded(false);
    setSearch(null);
    if (!option.create || inFlight.current) return;
    const requestGeneration = generation.current;
    const requestValue = currentValue.current;
    inFlight.current = true;
    try {
      const row = await create.mutateAsync(selected);
      if (generation.current === requestGeneration && currentValue.current === requestValue) {
        change(row.name);
        message.success('平台 / 商家已创建');
      }
    } catch (err) {
      if (generation.current === requestGeneration) message.error(errMsg(err));
    } finally {
      inFlight.current = false;
    }
  };

  return (
    <div
      className="voucher-platform-input"
      ref={container}
      onKeyDownCapture={(event) => {
        // 组件库会用 Tab 确认高亮项；这里 Tab 只移动焦点，不能创建选项。
        if (event.nativeEvent.isComposing || event.keyCode === 229) {
          event.stopPropagation();
        } else if (event.key === 'Tab') {
          event.stopPropagation();
          setExpanded(false);
        }
      }}
    >
      <AutoComplete<string, Option>
        id={id}
        value={value}
        onChange={change}
        options={options}
        popupMatchSelectWidth={popupWidth || true}
        open={active && expanded}
        onOpenChange={setExpanded}
        onFocus={() => setExpanded(true)}
        showSearch={{
          filterOption: false,
          onSearch: (next) => { setSearch(next); setExpanded(true); },
        }}
        onSelect={(selected, option) => { void select(selected, option); }}
        onInputKeyDown={(event) => {
          if (event.key === 'Enter' && expanded && !event.nativeEvent.isComposing) {
            event.preventDefault();
          }
        }}
        defaultActiveFirstOption={false}
        disabled={disabled}
        style={{ width: '100%' }}
        classNames={{ popup: { root: 'voucher-platform-dropdown' } }}
      >
        <Input
          allowClear
          placeholder="选择或输入平台 / 商家"
          suffix={
            create.isPending ? <LoadingOutlined /> : (
              <DownOutlined
                aria-label="展开平台 / 商家选项"
                onMouseDown={(event) => event.preventDefault()}
                onClick={() => { setSearch(null); setExpanded(!expanded); }}
                style={{ cursor: 'pointer' }}
              />
            )
          }
        />
      </AutoComplete>
      {platforms.isError && (
        <div role="status" className="muted">
          选项加载失败，可直接输入。
          <Button type="link" size="small" onClick={() => { void platforms.refetch(); }}>
            重试
          </Button>
        </div>
      )}
    </div>
  );
}
