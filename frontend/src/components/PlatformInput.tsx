import { useEffect, useRef, useState } from 'react';
import { App, AutoComplete, Button, Drawer, Grid, Input } from 'antd';
import { DownOutlined, LoadingOutlined } from '@ant-design/icons';
import { useCreateVoucherPlatform, useVoucherPlatforms } from '../api/vouchers';
import type { VoucherPlatform } from '../api/types';
import { errMsg } from '../api/client';
import { popupInParent } from '../lib/popup';

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

export default function PlatformInput({
  id,
  value = '',
  onChange,
  active,
  disabled,
  onCreatingChange,
}: Props) {
  const { message } = App.useApp();
  const screens = Grid.useBreakpoint();
  // md 起为桌面：保留下拉框；移动端改用底部浮层，绕开下拉定位在软键盘/滚动下的错乱。
  const isDesktop = !!screens.md;
  const platforms = useVoucherPlatforms(active);
  const create = useCreateVoucherPlatform();
  const [expanded, setExpanded] = useState(false);
  const [search, setSearch] = useState<string | null>(null);
  const [popupWidth, setPopupWidth] = useState(0);
  // 底部浮层内的搜索输入（移动端）：null 表示未聚焦，不弹键盘
  const [sheetSearch, setSheetSearch] = useState('');
  const [sheetOpen, setSheetOpen] = useState(false);
  const container = useRef<HTMLDivElement>(null);
  const generation = useRef(0);
  const currentValue = useRef(value);
  const inFlight = useRef(false);
  currentValue.current = value;

  useEffect(() => {
    generation.current++;
    setExpanded(false);
    setSearch(null);
    return () => {
      generation.current++;
    };
  }, [active]);
  useEffect(() => {
    onCreatingChange(active && create.isPending);
  }, [active, create.isPending, onCreatingChange]);
  // 浮层关闭时复位搜索词，下次打开是干净列表
  useEffect(() => {
    if (!sheetOpen) setSheetSearch('');
  }, [sheetOpen]);
  // 桌面端下拉宽度对齐触发器：测容器宽
  useEffect(() => {
    if (!isDesktop || !active || !container.current) return;
    const element = container.current;
    const measure = () => setPopupWidth(element.clientWidth);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, [isDesktop, active]);

  const change = (input: string | undefined) => {
    const next = input ?? '';
    currentValue.current = next;
    onChange?.(next);
  };
  const name = value.trim();
  const rows = platforms.data ?? [];
  // 搜索词：桌面端跟随下拉的 search（创建走输入框原值 name），移动端跟随浮层搜索框原值
  const activeSearch = isDesktop ? search : sheetOpen ? sheetSearch : null;
  const query = (activeSearch ?? '').trim().toLowerCase();
  // 移动端的"新建"候选词：浮层搜索框的原始输入（保留大小写），桌面端用输入框原值
  const rawCreate = isDesktop ? name : sheetOpen ? sheetSearch.trim() : '';
  const matched: VoucherPlatform[] = rows.filter((row) =>
    row.name.toLowerCase().includes(query)
  );
  const canCreate =
    !!rawCreate &&
    Array.from(rawCreate).length <= 128 &&
    !rows.some((row) => row.name === rawCreate);
  // 过滤后无匹配也无创建项时，才回落到提示占位
  const noMatch = !matched.length && !canCreate;

  const options: Option[] = matched.map((row) => ({
    value: row.name,
    label: row.name,
  }));
  if (canCreate) {
    options.push({
      value: rawCreate,
      label: create.isPending
        ? `正在创建「${rawCreate}」…`
        : `创建「${rawCreate}」`,
      create: true,
      disabled: create.isPending,
    });
  }
  if (noMatch) {
    options.push({
      value: '',
      label: platforms.isFetching
        ? '正在加载选项…'
        : Array.from(rawCreate).length > 128
          ? '名称最多 128 字'
          : '输入平台 / 商家名称可创建',
      disabled: true,
    });
  }

  // 选中现有项（不触发创建）
  const pick = (selected: string) => {
    change(selected);
    setSheetOpen(false);
    setSheetSearch('');
  };

  // 创建新平台：桌面端/移动端共用一套防抖与幂等守卫
  const select = async (selected: string, option: Option) => {
    setExpanded(false);
    setSearch(null);
    if (!option.create || inFlight.current) return;
    const requestGeneration = generation.current;
    const requestValue = currentValue.current;
    inFlight.current = true;
    try {
      const row = await create.mutateAsync(selected);
      if (
        generation.current === requestGeneration &&
        currentValue.current === requestValue
      ) {
        change(row.name);
        message.success('平台 / 商家已创建');
        setSheetOpen(false);
        setSheetSearch('');
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
      {isDesktop ? (
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
            onSearch: (next) => {
              setSearch(next);
              setExpanded(true);
            },
          }}
          onSelect={(selected, option) => {
            void select(selected, option);
          }}
          onInputKeyDown={(event) => {
            if (
              event.key === 'Enter' &&
              expanded &&
              !event.nativeEvent.isComposing
            ) {
              event.preventDefault();
            }
          }}
          defaultActiveFirstOption={false}
          disabled={disabled}
          style={{ width: '100%' }}
          getPopupContainer={popupInParent}
          classNames={{ popup: { root: 'voucher-platform-dropdown' } }}
        >
          <Input
            allowClear
            placeholder="选择或输入平台 / 商家"
            suffix={
              create.isPending ? (
                <LoadingOutlined />
              ) : (
                <DownOutlined
                  aria-label="展开平台 / 商家选项"
                  onMouseDown={(event) => event.preventDefault()}
                  onClick={() => {
                    setSearch(null);
                    setExpanded(!expanded);
                  }}
                  style={{ cursor: 'pointer' }}
                />
              )
            }
          />
        </AutoComplete>
      ) : (
        // 移动端：只读触发器 + 底部浮层。触发器不弹键盘，选/建都在浮层内完成，
        // 浮层是固定覆盖层，不受软键盘与滚动容器位移影响。
        <>
          <button
            type="button"
            id={id}
            className="voucher-platform-trigger"
            disabled={disabled}
            aria-haspopup="dialog"
            onClick={() => {
              if (active) setSheetOpen(true);
            }}
          >
            <span
              className={
                value
                  ? 'voucher-platform-trigger-value'
                  : 'voucher-platform-trigger-placeholder'
              }
            >
              {value || '选择或输入平台 / 商家'}
            </span>
            <DownOutlined className="voucher-platform-trigger-arrow" />
          </button>
          <Drawer
            title="选择平台 / 商家"
            placement="bottom"
            size="64vh"
            open={sheetOpen}
            onClose={() => setSheetOpen(false)}
            className="voucher-platform-sheet"
          >
            <Input
              autoFocus
              allowClear
              value={sheetSearch}
              onChange={(e) => setSheetSearch(e.target.value)}
              placeholder="搜索，或输入新名称创建"
              disabled={create.isPending}
            />
            <div className="voucher-platform-sheet-list">
              {value && (
                <button
                  type="button"
                  className="voucher-platform-sheet-item"
                  onClick={() => pick('')}
                >
                  清空平台 / 商家
                </button>
              )}
              {options.map((option) =>
                option.disabled ? (
                  <div
                    key="empty"
                    className="voucher-platform-sheet-empty"
                    role="status"
                  >
                    {option.label}
                  </div>
                ) : (
                  <button
                    key={option.create ? 'create' : option.value}
                    type="button"
                    className={`voucher-platform-sheet-item${option.create ? ' is-create' : ''}`}
                    aria-label={option.label}
                    disabled={create.isPending}
                    onClick={() => {
                      if (option.create) void select(option.value, option);
                      else pick(option.value);
                    }}
                  >
                    {option.create && (
                      <LoadingOutlined className="voucher-platform-sheet-item-icon" />
                    )}
                    {option.label}
                  </button>
                )
              )}
            </div>
          </Drawer>
        </>
      )}
      {platforms.isError && (
        <div role="status" className="muted">
          选项加载失败，可直接输入。
          <Button
            type="link"
            size="small"
            onClick={() => {
              void platforms.refetch();
            }}
          >
            重试
          </Button>
        </div>
      )}
    </div>
  );
}
