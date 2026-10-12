import { createContext, useContext, type ReactNode } from 'react';

const ActiveSelection = createContext(true);

// Overlay content may be memoized while its parent closes. Context updates still
// reach portaled children so a kept-mounted form cannot leave a sheet behind.
export function useSelectionActive(active = true) {
  return useContext(ActiveSelection) && active;
}

export default function SelectionScope({
  active,
  children,
}: {
  active: boolean;
  children: ReactNode;
}) {
  return (
    <ActiveSelection.Provider value={active}>
      {children}
    </ActiveSelection.Provider>
  );
}
