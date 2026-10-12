import { writeApi } from './request';
import type { SettlementPreview } from './liability';
import type {
  LiabilityCommand,
  LiabilityResult,
  SettlementInput,
} from './liability-inputs';

export function createLiabilityClient(send = writeApi) {
  return {
    execute(command: LiabilityCommand): Promise<LiabilityResult> {
      switch (command.mode) {
        case 'repay':
          return send('post', '/liabilities/repayments', command.input);
        case 'allocation':
          return send(
            'put',
            `/liabilities/repayments/${command.txnId}/allocations`,
            command.input
          );
        case 'reconcile':
          return send(
            'post',
            `/statements/${command.billId}/reconcile`,
            command.input
          );
        case 'create_plan':
          return send('post', '/liabilities/installments', command.input);
        case 'edit_plan':
          return send(
            'patch',
            `/liabilities/installments/${command.planId}`,
            command.input
          );
        case 'refund_adjustment':
          return send(
            'post',
            `/liabilities/installments/${command.planId}/refund-adjustments`,
            command.input
          );
        case 'settle':
          return send(
            'post',
            `/liabilities/installments/${command.planId}/settle`,
            command.input
          );
        case 'reverse':
          return send(
            'post',
            `/liabilities/operations/${command.operationId}/reverse`,
            command.input
          );
      }
    },
    preview(
      planId: number,
      input: SettlementInput
    ): Promise<SettlementPreview> {
      return send(
        'post',
        `/liabilities/installments/${planId}/settlement-preview`,
        input
      );
    },
  };
}
export const liabilityClient = createLiabilityClient();
