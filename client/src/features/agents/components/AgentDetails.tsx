import { Trash2 } from 'lucide-react';
import AgentAvatar from './AgentAvatar';
import type { Agent } from '../../casino/model/casino';

interface AgentDetailsProps {
  agent: Agent;
  tableName: string;
  onRemove: () => void;
}

export default function AgentDetails({ agent, tableName, onRemove }: AgentDetailsProps) {
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
