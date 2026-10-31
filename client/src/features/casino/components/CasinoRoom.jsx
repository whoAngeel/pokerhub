import React from 'react';
import PokerTable from './PokerTable';
import '../styles/scene.css';
import '../styles/casino.css';

export default function CasinoRoom({ tables, theme, onAddAgent, onSelectAgent, onToggleSession }) {
  return (
    <section className="casino" aria-label="Sala de casino con dos mesas">
      <div className="room-wall">
        <div className="shelf" aria-hidden="true">
          <i />
          <i />
          <i />
          <i />
          <i />
        </div>
        <div className="neon">
          {theme.sign}
          <span>♠ &nbsp; AGENTS ONLY &nbsp; ♠</span>
        </div>
        <div className="shelf right" aria-hidden="true">
          <i />
          <i />
          <i />
          <i />
          <i />
        </div>
      </div>
      <div className="casino-floor">
        <div className="room-decor" aria-hidden="true">
          <span>{theme.icon}</span>
          <span>♣</span>
        </div>
        <div className="tables-layout">
          {tables.map((table, index) => (
            <PokerTable
              key={table.id}
              table={table}
              index={index}
              onAddAgent={onAddAgent}
              onSelectAgent={onSelectAgent}
              onToggleSession={onToggleSession}
            />
          ))}
        </div>
        <div className="room-decor bottom-decor" aria-hidden="true">
          <span>♣</span>
          <span>♠ GOOD IDEAS. GREAT COMPANY. ♠</span>
          <span>{theme.icon}</span>
        </div>
      </div>
      <div className="room-footer">
        <span>
          <i className="green-dot" />
          Sala abierta · 2 mesas independientes
        </span>
        <span>18 asientos en total</span>
      </div>
    </section>
  );
}
