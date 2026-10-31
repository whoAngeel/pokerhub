import React, { useState } from 'react';
import { Plus } from 'lucide-react';
import { ROLES, TABLE_CAPACITY } from '../../casino/model/casino';

export default function AddAgentForm({ tables, initialTableId, seat, onSubmit }) {
  const [tableId, setTableId] = useState(initialTableId);
  const [name, setName] = useState('');
  const [role, setRole] = useState('Desarrollador');
  const table = tables.find((item) => item.id === tableId);
  const full = table.agents.length >= TABLE_CAPACITY;
  function handleSubmit(event) {
    event.preventDefault();
    if (!name.trim() || full) return;
    onSubmit(tableId, {
      name: name.trim(),
      role,
      seat: tableId === initialTableId ? seat : undefined,
    });
  }
  return (
    <form className="agent-form" onSubmit={handleSubmit}>
      <p>Cada mesa tiene su propio equipo y espacio para nueve agentes.</p>
      <label>
        Mesa
        <select value={tableId} onChange={(event) => setTableId(event.target.value)}>
          {tables.map((item) => (
            <option key={item.id} value={item.id} disabled={item.agents.length >= TABLE_CAPACITY}>
              {item.name} · {item.agents.length}/{TABLE_CAPACITY}
            </option>
          ))}
        </select>
      </label>
      <label>
        Nombre
        <input
          autoFocus
          required
          maxLength={18}
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder="Ej. Merlin"
        />
      </label>
      <label>
        Especialidad
        <select value={role} onChange={(event) => setRole(event.target.value)}>
          {ROLES.map((item) => (
            <option key={item}>{item}</option>
          ))}
        </select>
      </label>
      <button className="primary" disabled={!name.trim() || full}>
        <Plus size={16} /> Añadir a la mesa
      </button>
    </form>
  );
}
