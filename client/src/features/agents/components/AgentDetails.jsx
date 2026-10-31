import React from 'react';
import { Trash2 } from 'lucide-react';
import AgentAvatar from './AgentAvatar';

export default function AgentDetails({ agent, tableName, onRemove }) {
  return (
    <div className="agent-details">
      <AgentAvatar agent={agent} size={70} />
      <p>
        {agent.role} · {tableName}
      </p>
      <p>Agente de demostración. Su actividad se representa directamente en la mesa.</p>
      <button className="danger" onClick={onRemove}>
        <Trash2 size={16} /> Retirar de la mesa
      </button>
    </div>
  );
}
