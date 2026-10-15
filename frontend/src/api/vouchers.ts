import { writeEmptyApi } from './request';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type {
  VoucherDetail,
  VoucherListResp,
  VoucherInput,
  VoucherAction,
  VoucherOperationInput,
  VoucherPlatform,
} from './types';
import { readApi as unwrap, writeApi as voucherWrite } from './request';
import { useFinancialInvalidate as useVoucherInvalidate } from './cache';
import { qs } from './query';
import { queryKeys, useScope } from './keys';
import { requestJson } from './request';
import { updatePlatformCache } from './platformCache';
import { useCatalogReorder } from './ordering';

export const useReorderVoucherPlatforms = () => useCatalogReorder('voucher-platforms');

// ---------- 券 ----------
export function useVoucherPlatforms(enabled: boolean) {
  const scope = useScope();
  return useQuery({
    queryKey: queryKeys.key('voucher-platforms', scope),
    queryFn: ({ signal }) => {
      const options = { signal, _epoch: scope };
      return requestJson<VoucherPlatform[]>('/voucher-platforms', options);
    },
    enabled,
    retry: false,
    staleTime: 0,
  });
}

export function useCreateVoucherPlatform() {
  const scope = useScope();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => {
      const options = { method: 'post', data: { name }, _epoch: scope };
      return requestJson<VoucherPlatform>('/voucher-platforms', options);
    },
    onSuccess: (row) => updatePlatformCache(qc, scope,
      (rows) => [...rows.filter((item) => item.id !== row.id), row]),
  });
}

export function useRenameVoucherPlatform() {
  const scope = useScope();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, name }: { id: number; name: string }) => {
      const options = { method: 'put', data: { name }, _epoch: scope };
      return requestJson<VoucherPlatform>(`/voucher-platforms/${id}`, options);
    },
    onSuccess: (row) => updatePlatformCache(qc, scope,
      (rows) => [...rows.filter((item) => item.id !== row.id), row]),
  });
}

export function useDeleteVoucherPlatform() {
  const scope = useScope();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => {
      const options = { method: 'delete', _epoch: scope };
      return requestJson.empty(`/voucher-platforms/${id}`, options);
    },
    onSuccess: (_, id) => updatePlatformCache(qc, scope,
      (rows) => rows.filter((item) => item.id !== id)),
  });
}

export function useVouchers(
  params: {
    keyword?: string;
    status?: string;
    page?: number;
    page_size?: number;
  } = {}
) {
  const scope = useScope();
  const url = `/vouchers${qs(params)}`;
  return useQuery({
    queryKey: queryKeys.key('vouchers', scope, url),
    queryFn: ({ signal }) => unwrap<VoucherListResp>(url, signal),
  });
}

export function useVoucherDetail(id: number | null) {
  const scope = useScope();
  return useQuery({
    queryKey: queryKeys.key('vouchers', scope, 'detail', id),
    queryFn: ({ signal }) => unwrap<VoucherDetail>(`/vouchers/${id}`, signal),
    enabled: id !== null,
  });
}

export function useEnsureVouchers() {
  const scope = useScope();
  const qc = useQueryClient();
  const done = useVoucherInvalidate();
  return useMutation({
    mutationFn: () =>
      voucherWrite<{ generated: number; failed: number }>(
        'post',
        '/vouchers/ensure'
      ),
    onSuccess: (result) => {
      if (result.generated > 0) return done();
      if (result.failed > 0)
        return qc.invalidateQueries({
          queryKey: queryKeys.root('vouchers', scope),
        });
    },
  });
}

export function useVoucherMutations() {
  const done = useVoucherInvalidate();
  const create = useMutation({
    mutationFn: (input: VoucherInput) =>
      voucherWrite<VoucherDetail>('post', '/vouchers', input),
    onSuccess: done,
  });
  const update = useMutation({
    mutationFn: ({ id, input }: { id: number; input: VoucherInput }) =>
      voucherWrite<VoucherDetail>('put', `/vouchers/${id}`, input),
    onSuccess: done,
  });
  const remove = useMutation({
    mutationFn: (id: number) => writeEmptyApi('delete', `/vouchers/${id}`),
    onSuccess: done,
  });
  const operate = useMutation({
    mutationFn: ({
      id,
      action,
      input,
    }: {
      id: number;
      action: Exclude<VoucherAction, 'purchase'>;
      input: VoucherOperationInput;
    }) =>
      voucherWrite<VoucherDetail>('post', `/vouchers/${id}/${action}`, input),
    onSuccess: done,
  });
  const undo = useMutation({
    mutationFn: ({ id, eventId }: { id: number; eventId: number }) =>
      voucherWrite<VoucherDetail>(
        'post',
        `/vouchers/${id}/events/${eventId}/undo`
      ),
    onSuccess: done,
  });
  return { create, update, remove, operate, undo };
}
