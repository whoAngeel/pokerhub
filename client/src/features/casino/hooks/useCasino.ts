import { useReducer } from 'react';
import { casinoReducer, createInitialTables } from '../model/casino';

export interface AddAgentValues {
  name: string;
  role: string;
  seat?: number;
}

export function useCasino() {
  const [tables, dispatch] = useReducer(casinoReducer, undefined, createInitialTables);
  return {
    tables,
    addAgent: (tableId: string, values: AddAgentValues) =>
      dispatch({ ...values, type: 'agent/added', tableId, id: crypto.randomUUID() }),
    removeAgent: (tableId: string, agentId: string) =>
      dispatch({ type: 'agent/removed', tableId, agentId }),
    toggleSession: (tableId: string) => dispatch({ type: 'session/toggled', tableId }),
  };
}
