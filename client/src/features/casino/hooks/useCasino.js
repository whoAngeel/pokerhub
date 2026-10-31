import { useReducer } from 'react';
import { casinoReducer, createInitialTables } from '../model/casino';

export function useCasino() {
  const [tables, dispatch] = useReducer(casinoReducer, undefined, createInitialTables);
  return {
    tables,
    addAgent: (tableId, values) =>
      dispatch({ ...values, type: 'agent/added', tableId, id: crypto.randomUUID() }),
    removeAgent: (tableId, agentId) => dispatch({ type: 'agent/removed', tableId, agentId }),
    toggleSession: (tableId) => dispatch({ type: 'session/toggled', tableId }),
  };
}
