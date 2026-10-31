export const TABLE_CAPACITY = 9;
export const ROLES = [
  'Coordinador',
  'Investigadora',
  'Desarrollador',
  'Creativa',
  'Analista',
] as const;
export type Role = (typeof ROLES)[number];

const colors = ['#b7a1ee', '#f2bb74', '#8ebdb6', '#dc96aa', '#a9c28c'];

export interface Agent {
  id: string;
  name: string;
  role: Role;
  color: string;
  kind: number;
  seat: number;
}

export interface Table {
  id: string;
  name: string;
  agents: Agent[];
  running: boolean;
}

export function createInitialTables(): Table[] {
  const names = ['Atlas', 'Nova', 'Pixel', 'Echo', 'Sage'];
  const agents: Agent[] = names.map((name, index) => ({
    id: `agent-${index + 1}`,
    name,
    role: ROLES[index],
    color: colors[index],
    kind: index,
    seat: index < 3 ? index * 3 : (index - 3) * 4,
  }));
  return [
    { id: 'table-1', name: 'Mesa principal', agents: agents.slice(0, 3), running: false },
    { id: 'table-2', name: 'Mesa creativa', agents: agents.slice(3), running: false },
  ];
}

export function getAvailableSeat(table: Table): number | undefined {
  return Array.from({ length: TABLE_CAPACITY }, (_, index) => index).find(
    (seat) => !table.agents.some((agent) => agent.seat === seat),
  );
}

export interface AddAgentPayload {
  type: 'agent/added';
  tableId: string;
  id: string;
  name?: string;
  role?: string;
  seat?: number;
}

export interface RemoveAgentPayload {
  type: 'agent/removed';
  tableId: string;
  agentId: string;
}

export interface ToggleSessionPayload {
  type: 'session/toggled';
  tableId: string;
}

export type CasinoAction = AddAgentPayload | RemoveAgentPayload | ToggleSessionPayload;

function isRole(value: string | undefined): value is Role {
  return value !== undefined && (ROLES as readonly string[]).includes(value);
}

// Capacity and seat rules are independent of the React interface.
export function casinoReducer(tables: Table[], action: CasinoAction): Table[] {
  return tables.map((table) => {
    if (table.id !== action.tableId) return table;
    switch (action.type) {
      case 'agent/added': {
        const seat = action.seat ?? getAvailableSeat(table);
        const name = action.name?.trim();
        if (
          !name ||
          name.length > 18 ||
          !isRole(action.role) ||
          !action.id ||
          tables.some((item) => item.agents.some((agent) => agent.id === action.id)) ||
          !Number.isInteger(seat) ||
          seat === undefined ||
          seat < 0 ||
          seat >= TABLE_CAPACITY ||
          table.agents.length >= TABLE_CAPACITY ||
          table.agents.some((agent) => agent.seat === seat)
        )
          return table;
        return {
          ...table,
          agents: [
            ...table.agents,
            {
              id: action.id,
              name,
              role: action.role,
              seat,
              color: colors[seat % colors.length],
              kind: seat,
            },
          ],
        };
      }
      case 'agent/removed': {
        const agents = table.agents.filter((agent) => agent.id !== action.agentId);
        return { ...table, agents, running: table.running && agents.length > 0 };
      }
      case 'session/toggled':
        return { ...table, running: table.agents.length > 0 && !table.running };
      default:
        return table;
    }
  });
}
