import { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigationType, useSearchParams } from 'react-router-dom';
import { useTxnListInfinite } from '../../api/transactions';
import { presetRange } from '../../lib/dates';
import { useDebouncedValue } from '../../hooks/useDebouncedValue';
import {
  EMPTY_FILTERS,
  listParams,
  parseFilters,
  parsePage,
  serializeFilters,
  txnQueryParams,
  type FilterState,
} from './filters';

export function useRecordsState() {
  const [search, setSearch] = useSearchParams();
  const defaultRange = useMemo(() => presetRange('thisMonth'), []);
  const filters = useMemo(
    () => parseFilters(search, defaultRange),
    [search, defaultRange]
  );
  const page = parsePage(search);
  const [startPage, setStartPage] = useState(page);
  const navigation = useNavigationType();
  const keyword = useDebouncedValue(filters.keyword, navigation === 'POP');
  const params = useMemo(
    () => listParams({ ...filters, keyword }, page),
    [filters, keyword, page]
  );
  const list = useTxnListInfinite(params, startPage);
  const items = useMemo(
    () => list.data?.pages.flatMap((result) => result.items) ?? [],
    [list.data]
  );
  const total = list.data?.pages.at(-1)?.total ?? 0;
  const signature = serializeFilters(filters, 1, defaultRange).toString();
  const latest = useRef(signature);
  latest.current = signature;
  const appending = useRef(false);
  const generation = useRef(0);
  useEffect(() => {
    if (navigation === 'POP') {
      generation.current++;
      setStartPage(page);
    }
  }, [navigation, page]);
  const patch = (value: Partial<FilterState>) => {
    generation.current++;
    setStartPage(1);
    setSearch(serializeFilters({ ...filters, ...value }, 1, defaultRange));
  };
  const gotoPage = async (next: number) => {
    const current = ++generation.current;
    await list.resetPages(next);
    if (current !== generation.current) return;
    setStartPage(next);
    setSearch(serializeFilters(filters, next, defaultRange));
    window.scrollTo({ top: 0, behavior: 'instant' });
  };
  const loadNext = async () => {
    if (appending.current || list.isFetchingNextPage || !list.hasNextPage)
      return;
    appending.current = true;
    const current = generation.current;
    try {
      const result = await list.fetchNextPage();
      if (
        !result.isSuccess ||
        !result.data?.pages.length ||
        latest.current !== signature ||
        current !== generation.current
      )
        return;
      let index = result.data.pages.length - 1;
      while (index > 0 && !result.data.pages[index].items.length) index--;
      const loaded = startPage + index;
      if (loaded !== page)
        setSearch(serializeFilters(filters, loaded, defaultRange), {
          replace: true,
        });
    } finally {
      appending.current = false;
    }
  };
  return {
    filters,
    defaultRange,
    page,
    list,
    items,
    total,
    exportParams: txnQueryParams(listParams(filters, page), true),
    patch,
    gotoPage,
    loadNext,
    clearFilters: () => patch({ ...EMPTY_FILTERS, range: defaultRange }),
  };
}
