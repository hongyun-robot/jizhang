import { useAuth } from '../stores/auth';

const resourceName = (resource: string) =>
  resource === 'txns' || resource === 'txns-inf' ? 'transactions' : resource;
export const queryKeys = {
  root: (resource: string, epoch: number) =>
    [resourceName(resource), epoch] as const,
  key: (resource: string, epoch: number, ...parts: unknown[]) =>
    [
      resourceName(resource),
      epoch,
      ...(resource === 'txns'
        ? ['list']
        : resource === 'txns-inf'
          ? ['infinite']
          : []),
      ...parts,
    ] as const,
};
export function useScope(): number {
  return useAuth((state) => state.epoch);
}
