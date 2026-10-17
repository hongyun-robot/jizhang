import type { ReactNode, Dispatch, SetStateAction } from 'react';
import type { FormInstance } from 'antd';
import type {
  LiabilityDetail,
  LiabilityBill,
  InstallmentPlan,
  SettlementPreview,
  OrdinaryRepaymentPreview,
} from '../../../api/liability';
import type {
  CategoryTreeNode,
  Transaction,
  TxnListResp,
} from '../../../api/types';
import type { FeeMode } from '../../../api/liability-inputs';
import type { LiabilityDialog } from '../dialog';
import type { LiabilityValues } from '../input';

export interface ScheduleRow {
  key: number;
  number: number;
  amount: string;
  fee: string;
  close: string;
  due: string;
  feeDate: string;
}
type Option = { value: number; label: string };
/** Private presentation context; callers use the four-input dialog interface. */
export interface FieldContext {
  ordinaryPreview: OrdinaryRepaymentPreview | null;
  ordinaryAvailable: string;
  ordinaryPreviewing: boolean;
  loadOrdinaryPreview: () => Promise<void>;
  saving: boolean;
  setCreatingAccount: (creating: boolean) => void;
  setCreatingPlatform: (creating: boolean) => void;
  d: LiabilityDetail;
  dialog: LiabilityDialog;
  plan?: InstallmentPlan;
  bill?: LiabilityBill;
  values?: LiabilityValues;
  form: FormInstance<LiabilityValues>;
  assets: Option[];
  feeAccounts: Option[];
  categories: CategoryTreeNode[];
  bills: Option[];
  availableExpenses: Transaction[];
  sourceExpense?: Transaction;
  originalRemaining: string;
  feeMode: FeeMode;
  feeParts: string[];
  schedule: ScheduleRow[];
  setFeeDates: Dispatch<SetStateAction<Record<number, string>>>;
  reason: (required?: boolean) => ReactNode;
  allocationFields: ReactNode;
  onAddExpense: () => void;
  editable: (owner: number) => boolean;
  feeCandidates: { data?: TxnListResp };
  preview: SettlementPreview | null;
  previewing: boolean;
  loadPreview: () => Promise<void>;
}
