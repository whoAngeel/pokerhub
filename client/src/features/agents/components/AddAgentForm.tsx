import { useState, type FormEvent } from 'react';
import { Plus } from 'lucide-react';
import { ROLES, TABLE_CAPACITY, type Table } from '../../casino/model/casino';
import type { AddAgentValues } from '../../casino/hooks/useCasino';

interface AddAgentFormProps {
  tables: Table[];
  initialTableId?: string;
  seat?: number;
  onSubmit: (tableId: string, values: AddAgentValues) => void;
}

export default function AddAgentForm({
  tables,
  initialTableId,
  seat,
  onSubmit,
}: AddAgentFormProps) {
  const [tableId, setTableId] = useState(initialTableId ?? tables[0]?.id);
  const [name, setName] = useState('');
  const [role, setRole] = useState<string>('Desarrollador');
  const table = tables.find((item) => item.id === tableId);
  const full = (table?.agents.length ?? 0) >= TABLE_CAPACITY;
  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!name.trim() || full || !tableId) return;
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
