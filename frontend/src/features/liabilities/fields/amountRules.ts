import { amountError } from '../../../lib/money';

export const amountRules = [
  {
    validator: (_rule: unknown, value: unknown) => {
      const error = amountError(String(value ?? ''), { zero: true });
      return error ? Promise.reject(new Error(error)) : Promise.resolve();
    },
  },
];
