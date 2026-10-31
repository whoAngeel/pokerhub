import { useEffect, useState } from 'react';

// A future event transport can replace this local demo adapter.
export function useTableSession(table) {
  const [turn, setTurn] = useState(0);
  useEffect(() => {
    if (!table.running || !table.agents.length) return;
    const timer = setInterval(() => setTurn((value) => value + 1), 3200);
    return () => clearInterval(timer);
  }, [table.running, table.agents.length]);
  return table.running ? table.agents[turn % table.agents.length]?.id : null;
}
