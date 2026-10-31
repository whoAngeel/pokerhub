import { useEffect, useState } from 'react';
import type { Table } from '../model/casino';

const THINKING_MS = 1100;
const SPEAKING_MS = 2100;

export type SessionPhase = 'idle' | 'thinking' | 'speaking';

export interface SessionState {
  agentId: string | null;
  phase: SessionPhase;
}

const IDLE: SessionState = { agentId: null, phase: 'idle' };

// Demo adapter: cycles seated agents through thinking -> speaking, turn by
// turn. A future WS transport can drive the same { agentId, phase } shape
// straight off the real `turn`/`thinking`/`chat` protocol events, so
// consumers (PokerTable) don't need to change when that lands.
export function useTableSession(table: Table): SessionState {
  const [state, setState] = useState<SessionState>(IDLE);

  useEffect(() => {
    if (!table.running || !table.agents.length) {
      setState(IDLE);
      return;
    }
    const agents = table.agents;
    let turn = 0;
    let timer: ReturnType<typeof setTimeout>;
    const enter = (phase: SessionPhase, delay: number) => {
      setState({ agentId: agents[turn % agents.length].id, phase });
      timer = setTimeout(() => {
        if (phase === 'thinking') {
          enter('speaking', SPEAKING_MS);
        } else {
          turn += 1;
          enter('thinking', THINKING_MS);
        }
      }, delay);
    };
    enter('thinking', THINKING_MS);
    return () => clearTimeout(timer);
  }, [table.running, table.agents]);

  return table.running ? state : IDLE;
}
