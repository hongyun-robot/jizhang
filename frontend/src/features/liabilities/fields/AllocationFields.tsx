import type { FieldContext } from './context';
export default function AllocationFields({
  context,
}: {
  context: FieldContext;
}) {
  const { reason, allocationFields } = context;
  return (
    <>
      {allocationFields}
      {reason(true)}
    </>
  );
}
