import { useQueryClient, type QueryClient } from '@tanstack/react-query';
import { queryKeys, useScope } from './keys';

const views = {
  financial: [
    'txns',
    'accounts',
    'stats',
    'budgets',
    'liabilities',
    'vouchers',
    'notifications',
    'closings',
    'voucher-platforms',
    'funds',
    'gold',
  ],
  account: ['accounts', 'liabilities', 'stats', 'notifications', 'gold'],
  gold: ['gold', 'accounts', 'stats', 'txns', 'notifications'],
  fund: ['funds', 'accounts', 'stats', 'txns', 'notifications'],
  category: [
    'categories',
    'txns',
    'stats',
    'budgets',
    'recurring',
    'vouchers',
    'liabilities',
  ],
  closing: ['closings', 'audit', 'txns', 'stats', 'budgets', 'notifications', 'gold'],
};

export async function invalidateFinancialViews(
  client: QueryClient,
  epoch: number
): Promise<void> {
  await invalidateViews(client, epoch, 'financial');
}
export async function invalidateViews(
  client: QueryClient,
  epoch: number,
  event: keyof typeof views
): Promise<void> {
  await Promise.all(
    views[event].map((resource) =>
      client.invalidateQueries({ queryKey: queryKeys.root(resource, epoch) })
    )
  );
}
export function useInvalidation(
  event: keyof typeof views
): () => Promise<void> {
  const client = useQueryClient();
  const scope = useScope();
  return () => invalidateViews(client, scope, event);
}
export function useFinancialInvalidate(): () => Promise<void> {
  const client = useQueryClient();
  const scope = useScope();
  return () => invalidateFinancialViews(client, scope);
}
