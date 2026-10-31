import test from 'node:test';
import assert from 'node:assert/strict';
import { casinoReducer, createInitialTables, TABLE_CAPACITY } from './casino.js';

function add(tables, overrides = {}) {
  return casinoReducer(tables, {
    type: 'agent/added',
    tableId: 'table-1',
    id: 'new-agent',
    name: 'Merlin',
    role: 'Analista',
    ...overrides,
  });
}

test('each table has an independent session and team', () => {
  const initial = createInitialTables();
  const result = casinoReducer(initial, { type: 'session/toggled', tableId: 'table-1' });
  assert.equal(result[0].running, true);
  assert.equal(result[1], initial[1]);
  assert.equal(initial[0].running, false);
  const next = add(result, { tableId: 'table-2' });
  assert.equal(next[0], result[0]);
  assert.equal(next[1].agents.length, 3);
});

test('nine seats per table, without affecting availability at the other table', () => {
  let tables = createInitialTables();
  for (let i = 0; i < 12; i++) tables = add(tables, { id: `extra-${i}` });
  assert.equal(tables[0].agents.length, TABLE_CAPACITY);
  assert.equal(new Set(tables[0].agents.map((agent) => agent.seat)).size, TABLE_CAPACITY);
  assert.equal(add(tables, { tableId: 'table-2' })[1].agents.length, 3);
});

test('honors requested seats and rejects occupied or invalid seats', () => {
  const initial = createInitialTables();
  assert.equal(add(initial, { seat: 2 })[0].agents.at(-1).seat, 2);
  for (const seat of [0, -1, 9, 1.5]) assert.equal(add(initial, { seat })[0], initial[0]);
  for (const values of [{ name: '  ' }, { role: 'invalid' }, { id: 'agent-1' }])
    assert.equal(add(initial, values)[0], initial[0]);
});

test('removing an agent preserves other seats and makes that seat available again', () => {
  const initial = createInitialTables();
  const removed = casinoReducer(initial, {
    type: 'agent/removed',
    tableId: 'table-1',
    agentId: 'agent-1',
  });
  assert.deepEqual(
    removed[0].agents.map((agent) => agent.seat),
    [3, 6],
  );
  assert.equal(add(removed, { seat: 0 })[0].agents.at(-1).seat, 0);
});

test('removing the final agent stops the table and an empty table cannot start', () => {
  let tables = casinoReducer(createInitialTables(), {
    type: 'session/toggled',
    tableId: 'table-1',
  });
  for (const agent of tables[0].agents)
    tables = casinoReducer(tables, {
      type: 'agent/removed',
      tableId: 'table-1',
      agentId: agent.id,
    });
  assert.equal(tables[0].running, false);
  assert.equal(
    casinoReducer(tables, { type: 'session/toggled', tableId: 'table-1' })[0].running,
    false,
  );
});
