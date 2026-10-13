import { useQuery } from '@tanstack/react-query';
import type {
  AssetsResp,
  CategoryShareResp,
  CompareResp,
  DateRange,
  PeriodSummaryResp,
  TrendResp,
} from './types';
import { readApi as unwrap } from './request';
import { qs } from './query';
import { queryKeys, useScope } from './keys';

export function useStatsCategory(range: DateRange) {
  const scope = useScope();
  const url = `/stats/category${qs({ start_date: range.start, end_date: range.end })}`;
  return useQuery({
    queryKey: queryKeys.key('stats', scope, url),
    queryFn: ({ signal }) => unwrap<CategoryShareResp>(url, signal),
  });
}

export function useStatsTrend(range: DateRange, granularity: string) {
  const scope = useScope();
  const url = `/stats/trend${qs({ start_date: range.start, end_date: range.end, granularity })}`;
  return useQuery({
    queryKey: queryKeys.key('stats', scope, url),
    queryFn: ({ signal }) => unwrap<TrendResp>(url, signal),
  });
}

export function useStatsCompare(range: DateRange | null) {
  const scope = useScope();
  const url = `/stats/compare${qs({ start_date: range?.start, end_date: range?.end })}`;
  return useQuery({
    queryKey: queryKeys.key('stats', scope, url),
    queryFn: ({ signal }) => unwrap<CompareResp>(url, signal),
  });
}

export function useStatsPeriods(year: number) {
  const scope = useScope();
  const url = `/stats/periods${qs({ year })}`;
  return useQuery({
    queryKey: queryKeys.key('stats', scope, url),
    queryFn: ({ signal }) => unwrap<PeriodSummaryResp>(url, signal),
  });
}

export function useStatsAssets(range: DateRange) {
  const scope = useScope();
  const url = `/stats/assets${qs({ start_date: range.start, end_date: range.end })}`;
  return useQuery({
    queryKey: queryKeys.key('stats', scope, url),
    queryFn: ({ signal }) => unwrap<AssetsResp>(url, signal),
    refetchInterval: 60_000,
    refetchOnWindowFocus: true,
  });
}
