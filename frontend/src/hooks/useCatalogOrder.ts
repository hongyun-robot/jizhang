import { useEffect, useRef, useState } from 'react';
import { useScope } from '../api/keys';
import { errMsg } from '../api/client';
import { useAuth } from '../stores/auth';
import { orderItems, sameOrder, type OrderInput, type OrderResult } from '../lib/ordering';

interface State { scope: string; ids: number[]; pending: boolean; error?: string; failed?: OrderInput }
export function useCatalogOrder<T extends { id: number }>(
  rows: T[], scopeName: string, save: (input: OrderInput) => Promise<OrderResult>, reload: () => Promise<unknown>
) {
  const epoch = useScope();
  const scope = `${epoch}:${scopeName}`;
  const [state, setState] = useState<State>();
  const latest = useRef({ rows, scope, save, reload, epoch });
  latest.current = { rows, scope, save, reload, epoch };
  const mounted = useRef(false);
  const flight = useRef(false);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const current = state?.scope === scope ? state : undefined;
  const serverIDs = rows.map((row) => row.id);
  const sameMembers = current && current.ids.length === serverIDs.length && current.ids.every((id) => serverIDs.includes(id));
  const ids = current?.pending || (current?.error && sameMembers) ? current.ids : serverIDs;
  const submit = async (input: OrderInput) => {
    if (flight.current) return;
    const started = latest.current;
    flight.current = true;
    setState({ scope: started.scope, ids: input.ordered_ids, pending: true });
    const valid = () => mounted.current && latest.current.scope === started.scope && useAuth.getState().epoch === started.epoch;
    try {
      const result = await started.save(input);
      if (valid()) setState({ scope: started.scope, ids: result.ordered_ids, pending: false });
    } catch (error) {
      if (valid()) setState({ scope: started.scope, ids: input.previous_ids, pending: false, error: errMsg(error), failed: input });
    } finally { flight.current = false; }
  };
  return {
    ids, rows: orderItems(rows, ids), busy: !!current?.pending,
    error: current?.error,
    commit: (next: number[]) => {
      const previous = latest.current.rows.map((row) => row.id);
      if (!sameOrder(next, previous)) void submit({ ordered_ids: next, previous_ids: previous, request_key: crypto.randomUUID() });
    },
    retry: () => { if (current?.failed) void submit(current.failed); },
    refresh: async () => {
      const started = latest.current;
      const result = await started.reload();
      if (mounted.current && latest.current.scope === started.scope && useAuth.getState().epoch === started.epoch && !(result && typeof result === 'object' && 'isError' in result && result.isError)) setState(undefined);
    },
  };
}
