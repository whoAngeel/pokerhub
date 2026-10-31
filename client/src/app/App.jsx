import React, { useState } from 'react';
import { LayoutGrid, Plus, Settings, Spade, Users } from 'lucide-react';
import CasinoRoom from '../features/casino/components/CasinoRoom';
import { useCasino } from '../features/casino/hooks/useCasino';
import { TABLE_CAPACITY } from '../features/casino/model/casino';
import AddAgentForm from '../features/agents/components/AddAgentForm';
import AgentDetails from '../features/agents/components/AgentDetails';
import ThemePicker from '../features/themes/ThemePicker';
import { useTheme } from '../features/themes/useTheme';
import Dialog from '../shared/components/Dialog';
import './styles.css';

export default function App() {
  const { tables, addAgent, removeAgent, toggleSession } = useCasino();
  const { themeId, theme, setThemeId } = useTheme();
  const [dialog, setDialog] = useState(null);
  const totalAgents = tables.reduce((total, table) => total + table.agents.length, 0);
  const availableTable = tables.find((table) => table.agents.length < TABLE_CAPACITY);
  const selectedTable = tables.find((table) => table.id === dialog?.tableId);
  const selectedAgent = selectedTable?.agents.find((agent) => agent.id === dialog?.agentId);
  function openAddAgent(tableId = availableTable?.id, seat) {
    if (tableId) setDialog({ type: 'add', tableId, seat });
  }
  return (
    <div
      className="app"
      style={{ '--accent': theme.accent, '--felt': theme.felt, '--floor': theme.floor }}
    >
      <aside className="rail" aria-label="Navegación principal">
        <a className="brand" href="#casino" aria-label="Agent Royale">
          <Spade fill="currentColor" size={26} />
        </a>
        <a className="rail-link active" href="#casino" aria-label="Casino">
          <LayoutGrid size={21} />
        </a>
        <button
          className="rail-link"
          onClick={() => setDialog({ type: 'agents' })}
          aria-label="Ver agentes"
        >
          <Users size={21} />
        </button>
        <button
          className="rail-link rail-bottom"
          onClick={() => setDialog({ type: 'settings' })}
          aria-label="Configuración"
        >
          <Settings size={21} />
        </button>
      </aside>
      <main id="casino">
        <header className="app-header">
          <div className="wordmark">
            agent<span>royale</span>
            <small>WORKSPACE</small>
          </div>
          <span className="demo-badge">Modo demo</span>
        </header>
        <div className="workspace">
          <div className="breadcrumb">
            Workspace <span>/</span> Casino <span>/</span> <b>Sala principal</b>
          </div>
          <div className="heading">
            <div>
              <div className="eyebrow">ESPACIO PARA GRANDES IDEAS</div>
              <h1>
                Dos mesas. Infinitas posibilidades<span>.</span>
              </h1>
              <p>Dos equipos, su propio ritmo y más espacio para colaborar.</p>
            </div>
            <button className="primary" disabled={!availableTable} onClick={() => openAddAgent()}>
              <Plus size={17} /> Añadir agente
            </button>
          </div>
          <section className="room-panel">
            <div className="panel-toolbar">
              <div>
                <Spade size={18} />
                <strong>Sala principal</strong>
                <span className="room-count">2 mesas · {totalAgents}/18 agentes</span>
              </div>
              <ThemePicker value={themeId} onChange={setThemeId} />
            </div>
            <CasinoRoom
              tables={tables}
              theme={theme}
              onAddAgent={openAddAgent}
              onSelectAgent={(tableId, agentId) => setDialog({ type: 'agent', tableId, agentId })}
              onToggleSession={toggleSession}
            />
          </section>
          <footer>
            <span>Hecho para colaborar. Diseñado para disfrutar.</span>
            <span>
              <i className="green-dot" />
              Actividad simulada · sesiones independientes
            </span>
          </footer>
        </div>
      </main>
      {dialog && (
        <Dialog
          title={
            {
              add: 'Un nuevo agente en la mesa.',
              agent: selectedAgent?.name,
              agents: 'Los equipos del casino',
              settings: 'Tu casino, tu ambiente.',
            }[dialog.type]
          }
          onClose={() => setDialog(null)}
        >
          {dialog.type === 'add' && (
            <AddAgentForm
              tables={tables}
              initialTableId={dialog.tableId}
              seat={dialog.seat}
              onSubmit={(tableId, values) => {
                addAgent(tableId, values);
                setDialog(null);
              }}
            />
          )}
          {dialog.type === 'agent' && selectedAgent && (
            <AgentDetails
              agent={selectedAgent}
              tableName={selectedTable.name}
              onRemove={() => {
                removeAgent(selectedTable.id, selectedAgent.id);
                setDialog(null);
              }}
            />
          )}
          {dialog.type === 'agents' && (
            <div className="team-list">
              {tables.map((table) => (
                <section key={table.id}>
                  <h3>
                    {table.name}{' '}
                    <small>
                      {table.agents.length}/{TABLE_CAPACITY}
                    </small>
                  </h3>
                  {table.agents.map((agent) => (
                    <button
                      key={agent.id}
                      onClick={() =>
                        setDialog({ type: 'agent', tableId: table.id, agentId: agent.id })
                      }
                    >
                      <span>{agent.name}</span>
                      <small>{agent.role}</small>
                    </button>
                  ))}
                  {!table.agents.length && <p>No hay agentes en esta mesa.</p>}
                  <button
                    className="secondary"
                    disabled={table.agents.length >= TABLE_CAPACITY}
                    onClick={() => openAddAgent(table.id)}
                  >
                    <Plus size={14} /> Añadir agente
                  </button>
                </section>
              ))}
            </div>
          )}
          {dialog.type === 'settings' && (
            <>
              <p>La temática se aplica a toda la sala y se guarda en este navegador.</p>
              <ThemePicker value={themeId} onChange={setThemeId} />
              <p className="demo-note">
                La actividad es simulada. Los agentes y las sesiones se reinician al recargar. No
                hay conexión con modelos de IA ni con otros usuarios.
              </p>
            </>
          )}
        </Dialog>
      )}
    </div>
  );
}
