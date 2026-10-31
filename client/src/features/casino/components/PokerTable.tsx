import type { CSSProperties } from 'react';
import { Pause, Play, Plus } from 'lucide-react';
import AgentAvatar from '../../agents/components/AgentAvatar';
import { TABLE_CAPACITY, type Table } from '../model/casino';
import { useTableSession } from '../hooks/useTableSession';

interface PokerTableProps {
  table: Table;
  index: number;
  onAddAgent: (tableId?: string, seat?: number) => void;
  onSelectAgent: (tableId: string, agentId: string) => void;
  onToggleSession: (tableId: string) => void;
}

export default function PokerTable({
  table,
  index,
  onAddAgent,
  onSelectAgent,
  onToggleSession,
}: PokerTableProps) {
  const { agentId: activeAgentId, phase } = useTableSession(table);
  return (
    <section className="table-zone" aria-label={table.name}>
      <div className="table-zone-header">
        <span className="table-number">0{index + 1}</span>
        <h2>{table.name}</h2>
        <span>
          {table.agents.length}/{TABLE_CAPACITY} agentes
        </span>
      </div>
      <div className="table-stage">
        <div className="carpet" />
        <div className="poker-table">
          <div className="felt">
            <div className="table-caption">
              AGENT ROYALE<span>{table.name.toUpperCase()}</span>
            </div>
            <div className="cards" aria-hidden="true">
              {['♠', '♥', '♣', '♦', '♠'].map((suit, card) => (
                <i key={card}>{suit}</i>
              ))}
            </div>
            <div className="chips" aria-hidden="true">
              ▰ ▰ ▰
            </div>
          </div>
        </div>
        {Array.from({ length: TABLE_CAPACITY }, (_, seat) => {
          const agent = table.agents.find((item) => item.seat === seat);
          const angle = ((-90 + seat * 40) * Math.PI) / 180;
          const isActive = agent?.id === activeAgentId;
          const thinking = isActive && phase === 'thinking';
          const speaking = isActive && phase === 'speaking';
          const stateLabel = thinking ? ' (pensando)' : speaking ? ' (hablando)' : '';
          return (
            <button
              key={seat}
              className={`seat ${agent ? 'occupied' : 'empty'} ${thinking ? 'thinking' : ''} ${speaking ? 'speaking' : ''}`}
              style={
                {
                  left: `${50 + 37 * Math.cos(angle)}%`,
                  top: `${50 + 35 * Math.sin(angle)}%`,
                  '--seat-i': seat,
                } as CSSProperties
              }
              onClick={() =>
                agent ? onSelectAgent(table.id, agent.id) : onAddAgent(table.id, seat)
              }
              aria-label={
                agent
                  ? `${agent.name}, ${agent.role}, ${table.name}${stateLabel}`
                  : `Añadir agente al asiento ${seat + 1} de ${table.name}`
              }
            >
              {agent ? (
                <>
                  <div className="chair">
                    <AgentAvatar agent={agent} size={48} />
                  </div>
                  <span className="seat-name">
                    <i />
                    {agent.name}
                  </span>
                  {thinking && (
                    <span className="thought" aria-hidden="true">
                      ⋯
                    </span>
                  )}
                  {speaking && (
                    <span className="bubble" aria-label="Colaborando">
                      •••
                    </span>
                  )}
                </>
              ) : (
                <>
                  <span className="empty-chair">+</span>
                  <span className="seat-name">Asiento libre</span>
                </>
              )}
            </button>
          );
        })}
      </div>
      <div className="table-controls">
        <span>
          <i className={table.running ? 'green-dot' : 'idle-dot'} />
          {table.running ? 'Colaborando · demo' : 'En espera'}
        </span>
        <button
          className="icon-button"
          disabled={table.agents.length >= TABLE_CAPACITY}
          onClick={() => onAddAgent(table.id)}
          aria-label={`Añadir agente a ${table.name}`}
        >
          <Plus size={16} />
        </button>
        <button
          className={table.running ? 'secondary' : 'primary'}
          disabled={!table.agents.length}
          onClick={() => onToggleSession(table.id)}
        >
          {table.running ? <Pause size={14} /> : <Play size={14} />}
          {table.running ? 'Pausar' : 'Iniciar sesión'}
        </button>
      </div>
    </section>
  );
}
