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

// ---------- 券 ----------
export function useVoucherPlatforms(enabled: boolean) {
  const scope = useScope();
  return useQuery({
    queryKey: queryKeys.key('voucher-platforms', scope),
    queryFn: ({ signal }) =>
      unwrap<VoucherPlatform[]>('/voucher-platforms', signal),
    enabled,
    retry: false,
    staleTime: 0,
  });
}

export function useCreateVoucherPlatform() {
  const scope = useScope();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) =>
      voucherWrite<VoucherPlatform>('post', '/voucher-platforms', { name }),
    onSuccess: async (row) => {
      await qc.cancelQueries({
        queryKey: queryKeys.root('voucher-platforms', scope),
      });
      qc.setQueryData<VoucherPlatform[]>(
        queryKeys.key('voucher-platforms', scope),
        (rows = []) =>
          [...rows.filter((item) => item.id !== row.id), row].sort((a, b) =>
            a.name < b.name ? -1 : a.name > b.name ? 1 : a.id - b.id
          )
      );
      void qc.invalidateQueries({
        queryKey: queryKeys.root('voucher-platforms', scope),
      });
    },
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
