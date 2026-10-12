import { useEffect, useRef, useState } from 'react';
import { ConfigProvider } from 'antd';
import type { Account } from '../api/types';
import { useScope } from '../api/keys';
import AccountEditor from './AccountEditor';
import ResponsiveSelect, { type ResponsiveSelectProps } from './ResponsiveSelect';
import { useSelectionActive } from './SelectionScope';

type Props = ResponsiveSelectProps<number> & {
  assetOnly?: boolean;
  onCreatingChange?: (creating: boolean) => void;
};
export default function AccountSelect({ assetOnly, onCreatingChange, ...props }: Props) {
  const active = useSelectionActive(props.active ?? true);
  const { componentDisabled } = ConfigProvider.useConfig();
  const disabled = props.disabled ?? componentDisabled;
  const scope = useScope();
  const current = useRef({ value: props.value, scope, active, disabled, assetOnly });
  current.current = { value: props.value, scope, active, disabled, assetOnly };
  const [creation, setCreation] = useState<{ name: string; value: typeof props.value; scope: number }>();
  const [created, setCreated] = useState<{ account: Account; scope: number }>();
  const [closeSignal, setCloseSignal] = useState(0);
  useEffect(() => {
    if (!active || disabled || creation?.scope !== scope) setCreation(undefined);
  }, [active, disabled, scope, creation?.scope]);
  useEffect(() => { setCreated(undefined); }, [scope]);
  useEffect(() => {
    if (created && props.options?.some((option) => option.value === created.account.id)) setCreated(undefined);
  }, [created, props.options]);
  const options = [...(props.options ?? [])];
  if (created?.scope === scope && (!assetOnly || created.account.type === 'asset') && !options.some((option) => option.value === created.account.id))
    options.push({ value: created.account.id, label: created.account.name });
  return <>
    <ResponsiveSelect<number>
      {...props}
      disabled={disabled}
      options={options}
      suspended={!!creation}
      closeSignal={closeSignal}
      onCreate={active && !disabled ? (name) => setCreation({ name: Array.from(name).slice(0, 64).join(''), value: props.value, scope }) : undefined}
    />
    {creation && <AccountEditor
      initialName={creation.name}
      assetOnly={assetOnly}
      onPending={onCreatingChange}
      onClose={() => setCreation(undefined)}
      onCreated={(account) => {
        if (current.current.scope !== creation.scope || current.current.value !== creation.value || !current.current.active || current.current.disabled || (current.current.assetOnly && account.type !== 'asset')) return;
        setCreated({ account, scope: creation.scope });
        setCloseSignal((value) => value + 1);
        props.onChange?.(account.id, { value: account.id, label: account.name });
        setCreation(undefined);
      }}
    />}
  </>;
}
