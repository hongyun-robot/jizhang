import { useInvalidation } from './cache';
import { writeEmptyApi } from './request';
import { useMutation, useQuery } from '@tanstack/react-query';
import type { Category, CategoryForest } from './types';
import { readApi as unwrap, writeApi } from './request';
import { queryKeys, useScope } from './keys';

export function useCategoryForest() {
  const scope = useScope();
  return useQuery({
    queryKey: queryKeys.key('categories', scope),
    queryFn: ({ signal }) => unwrap<CategoryForest>('/categories', signal),
  });
}

export interface CategoryInput {
  kind: 'expense' | 'income';
  name: string;
  parent_id?: number;
  sort?: number;
}

export function useCategoryMutations() {
  const done = useInvalidation('category');
  const create = useMutation({
    mutationFn: async (input: CategoryInput) => {
      const data = await writeApi<Category>('post', '/categories', input);
      return data;
    },
    onSuccess: done,
  });
  const update = useMutation({
    mutationFn: async ({
      id,
      name,
      sort,
    }: {
      id: number;
      name: string;
      sort?: number;
    }) => {
      const data = await writeApi<Category>('put', `/categories/${id}`, {
        name,
        sort,
      });
      return data;
    },
    onSuccess: done,
  });
  const remove = useMutation({
    mutationFn: async (id: number) => {
      await writeEmptyApi('delete', `/categories/${id}`);
    },
    onSuccess: done,
  });
  return { create, update, remove };
}
