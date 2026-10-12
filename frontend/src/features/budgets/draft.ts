import type { BudgetView } from '../../api/types';

export interface BudgetDraft {
  key: string;
  fields: Record<string, string>;
  dirty: boolean;
}
export function budgetFields(view: BudgetView): Record<string, string> {
  const fields: Record<string, string> = {};
  if (view.total.budget) fields.total = view.total.budget;
  for (const category of view.categories)
    if (category.budget) fields[String(category.category_id)] = category.budget;
  return fields;
}
export function receiveBudget(
  draft: BudgetDraft,
  key: string,
  fields: Record<string, string>
): BudgetDraft {
  return draft.key === key && draft.dirty
    ? draft
    : { key, fields, dirty: false };
}
export function savedBudget(
  draft: BudgetDraft,
  key: string,
  fields: Record<string, string>
): BudgetDraft {
  return draft.key === key ? { key, fields, dirty: false } : draft;
}
