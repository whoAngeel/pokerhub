# PROTOCOL.md — Contrato WebSocket backend ↔ frontend

> Fuente de verdad: `internal/protocol`. Si un evento cambia, este documento y
> `docs/schema/*.json` se actualizan en el mismo commit. Versión actual: **v1**.

Este documento describe el contrato en tiempo real entre el servidor Go
(`pokerd`) y cualquier cliente (el frontend React en `client/`, un script de
depuración, el propio `pokerd replay`). El servidor es la única fuente de
verdad: valida toda acción, decide qué ve cada rol y nunca confía en el
cliente para la lógica de juego.

## 1. Conexión

| Endpoint | Descripción |
|---|---|
| `GET /ws?role=spectator` | Conexión de espectador. Vista dios: todas las hole cards, `reasoning`, `memory_update`, `agent_error`. |
| `GET /ws?role=player&seat=N` | Conexión del asiento humano `N` (post-MVP). Ve sus propias hole cards + estado público; nunca ve `reasoning` ni las hole cards de otros antes del showdown. |
| `GET /healthz` | Liveness check HTTP simple, `200 OK`. No es WebSocket. |
| `GET /api/config` | HTTP GET, JSON con la configuración pública de la mesa: ciegas, buy-in, lista de asientos con `name`/`persona_id`/`kind` (sin claves ni prompts). Útil para que el frontend pinte la sala antes de que llegue el primer `snapshot`. |

### Reglas de visibilidad por rol

- **`spectator`**: ve todo. Es el rol pensado para la demo y para quien mira
  sin jugar.
- **`player:<seat>`**: ve su propio estado completo (`hole_cards` propias,
  `legal_actions` propias) y el estado público del resto (sin hole cards
  ajenas, sin `reasoning`, sin `memory_update`, sin `agent_error`).
- **Con un humano sentado**, el servidor **rechaza** nuevas conexiones
  `spectator` cerrando el WebSocket con el código de cierre **`4003`**
  (`policy violation`, "spectator deshabilitado: hay un humano jugando"),
  salvo que `server.debug: true` en `configs/config.yaml`. Esto evita que
  alguien con el enlace de espectador vea las cartas del humano.
- En el MVP no hay asientos humanos, así que `spectator` siempre está
  disponible salvo que se active deliberadamente para probar el rechazo.

### Reconexión

El cliente puede reconectar y recuperar el hilo enviando
`hello{"role":...,"seat":...,"last_seq":...}` (ver §5). El servidor reenvía
un `snapshot` fresco y continúa el stream desde ahí; no hay garantía de
reenviar eventos entre `last_seq` y el snapshot (el snapshot ya contiene el
estado resultante). `last_seq` es principalmente informativo/telemetría en
el MVP; el recorte documentado en `PLAN.md` §9 permite degradar esto a "solo
`snapshot` al conectar, sin replays de huecos".

## 2. Envoltorio de todos los mensajes

Todo mensaje — en cualquier dirección — es un objeto JSON con esta forma
fija:

```json
{
  "v": 1,
  "seq": 1234,
  "ts": "2026-10-31T18:04:12.500Z",
  "type": "action",
  "hand_id": 57,
  "payload": { }
}
```

- `v`: versión del protocolo (entero). Hoy siempre `1`.
- `seq`: contador monótono creciente por conexión, asignado por el servidor
  a cada mensaje que envía. El cliente no lo fija al enviar mensajes propios
  (`hello`, `control`, `action`); el servidor lo ignora si viene.
- `ts`: timestamp RFC 3339 (UTC), con milisegundos.
- `type`: nombre del evento o mensaje, en inglés, minúsculas (ver tablas
  abajo).
- `hand_id`: identificador entero de la mano en curso, o `null` para
  mensajes que no pertenecen a ninguna mano (p. ej. `snapshot` inicial antes
  de que empiece la primera mano, o `agent_error` de la cola de notas entre
  manos).
- `payload`: objeto específico del `type`. Nunca se envían eventos sin pasar
  antes por `server.Project(event, role)` — el proyector es el único punto
  que decide qué campos sobreviven para cada rol.

Al conectar, el servidor siempre envía primero un `snapshot` y después el
stream incremental de eventos. El cliente nunca debe asumir un orden
distinto.

## 3. Tipos compartidos

Estos tipos aparecen repetidos en varios payloads; se definen una sola vez
aquí.

### Card

String de 2 caracteres: `rango` + `palo`.

- Rangos: `2 3 4 5 6 7 8 9 T J Q K A`
- Palos: `s` (picas), `h` (corazones), `d` (diamantes), `c` (tréboles)

Ejemplos: `"Ah"` (as de corazones), `"Td"` (10 de diamantes).

### Seat

Describe a un jugador en la mesa, con o sin mano en curso.

| Campo | Tipo | Notas |
|---|---|---|
| `seat` | int | Índice de asiento, estable durante toda la sesión. |
| `name` | string | Nombre visible en mesa (persona o nombre de humano). |
| `persona_id` | string \| null | `id` de `configs/personas/*.yaml`; `null` para `rulebot`/`human`. |
| `kind` | `"llm"` \| `"rulebot"` \| `"human"` | Tipo de jugador. |
| `stack` | int | Fichas actuales. |
| `status` | `"active"` \| `"folded"` \| `"all_in"` \| `"sitting_out"` | Estado en la mano actual. |
| `bet_this_street` | int | Lo apostado en la calle en curso (se reinicia cada calle). |
| `model` | string \| null | Modelo LLM usado (p. ej. `"gemma4:e4b"`); `null` para `rulebot`/`human`. |

### LegalAction

Una acción que el motor permite en el turno actual. Solo la ve el dueño del
asiento y `spectator` (ver evento `turn`).

| Campo | Tipo | Notas |
|---|---|---|
| `type` | `"fold"` \| `"check"` \| `"call"` \| `"bet"` \| `"raise"` \| `"all_in"` | |
| `amount` | int? | Presente en `call`/`all_in`: monto fijo de esa acción. |
| `min` | int? | Presente en `bet`/`raise`: **total apostado tras la acción** ("raise to"), mínimo legal. |
| `max` | int? | Presente en `bet`/`raise`: total apostado tras la acción, máximo legal (normalmente el stack, i.e. all-in). |

`min`/`max` son siempre el total resultante de la apuesta en esa calle, no
el incremento — así el cliente puede usarlos directamente como extremos de
un slider sin hacer aritmética.

## 4. Eventos servidor → cliente

Para cada evento: visibilidad, forma del `payload` y un ejemplo (el
envoltorio se omite en los ejemplos por brevedad; solo se muestra
`payload`).

### `snapshot`

**Visibilidad:** por rol (contenido filtrado; todos los roles reciben uno al
conectar o reconectar). Es el único evento donde el `payload` depende
fuertemente del rol: `spectator` recibe `hole` de todos los asientos
ocupados; `player:<seat>` solo recibe el suyo (los demás llevan `hole: null`).

```json
{
  "hand_id": 57,
  "street": "turn",
  "button": 3,
  "small_blind": 5,
  "big_blind": 10,
  "board": ["Ks", "7h", "2c", "9d"],
  "pot": 310,
  "to_act": 2,
  "seats": [
    { "seat": 0, "name": "Calculín", "persona_id": null, "kind": "rulebot",
      "stack": 700, "status": "active", "bet_this_street": 0, "model": null,
      "hole": null }
  ],
  "legal_actions": [
    { "type": "fold" },
    { "type": "call", "amount": 60 },
    { "type": "raise", "min": 120, "max": 880 }
  ]
}
```

`legal_actions` solo aparece si `to_act` es un asiento que el rol conectado
puede ver (el propio, o cualquiera si es `spectator`).

### `hand_start`

**Visibilidad:** pública.

```json
{ "hand_id": 58, "button": 4, "small_blind": 5, "big_blind": 10,
  "seats": [ { "seat": 0, "name": "Calculín", "stack": 700 } ] }
```

### `blind`

**Visibilidad:** pública. Una por cada ciega posteada.

```json
{ "seat": 0, "type": "small", "amount": 5, "stack_after": 695, "pot_after": 5 }
```

### `hole_cards`

**Visibilidad:** dueño del asiento + `spectator`.

```json
{ "seat": 2, "cards": ["Ah", "Kd"] }
```

### `street`

**Visibilidad:** pública.

```json
{ "street": "flop", "board": ["Ks", "7h", "2c"] }
```

`street` ∈ `"preflop" | "flop" | "turn" | "river" | "showdown"`.

### `turn`

**Visibilidad:** pública, pero `legal_actions` solo va al dueño del asiento
y a `spectator`.

```json
{ "seat": 2, "to_call": 60, "deadline_ms": 10000,
  "legal_actions": [ { "type": "fold" }, { "type": "call", "amount": 60 },
                      { "type": "raise", "min": 120, "max": 880 } ] }
```

### `thinking`

**Visibilidad:** pública. Señal para que la UI muestre el spinner/avatar
pensando mientras el LLM responde. No lleva más datos.

```json
{ "seat": 2 }
```

### `action`

**Visibilidad:** pública.

```json
{ "seat": 2, "type": "raise", "amount": 120, "stack_after": 760, "pot_after": 430 }
```

`amount` es **lo que esa acción añade al bote** (no el total de la calle).
`stack_after` y `pot_after` son los valores resultantes tras aplicar la
acción, para que el cliente no tenga que acumular estado por su cuenta.

### `chat`

**Visibilidad:** pública. Es el único contenido "libre" de un agente — puede
ser un farol. Máx. 120 caracteres (recortado en el servidor).

```json
{ "seat": 4, "say": "¡Esa carta me encanta!" }
```

### `reasoning`

**Visibilidad:** **solo `spectator`**. Nunca llega a un `player:<seat>`,
ni siquiera el del propio asiento razonado — es deliberadamente privado
incluso para su dueño, para no romper la separación con el prompt de otros
agentes.

```json
{ "seat": 2,
  "hand": "pareja de reyes, kicker as",
  "odds": "equity 0.74, pot odds 0.16",
  "reads": "Abuela solo sube con manos hechas",
  "plan": "subir para sacar manos marginales",
  "meta": { "provider": "local", "model": "gemma4:e4b", "latency_ms": 1120,
            "tokens_out": 151, "retries": 0, "fallback": false, "warnings": [] } }
```

### `showdown`

**Visibilidad:** pública (al llegar a showdown las cartas reveladas son
públicas por reglas del juego).

```json
{ "reveals": [ { "seat": 2, "cards": ["Ah", "Kd"], "rank": "par de reyes" } ] }
```

### `pot_awarded`

**Visibilidad:** pública. Puede incluir varios botes (bote principal + side
pots) y empates (varios `winners`).

```json
{ "pots": [ { "amount": 430, "winners": [2] } ] }
```

### `hand_end`

**Visibilidad:** pública. Cierre de la mano: stacks finales de todos los
asientos que seguían en la mesa.

```json
{ "hand_id": 57, "seats": [ { "seat": 0, "stack": 695 }, { "seat": 2, "stack": 1190 } ] }
```

### `rebuy`

**Visibilidad:** pública. Se emite cuando un asiento queda en 0 (o por
debajo del mínimo configurado) y el servidor le repone fichas
automáticamente (`table.rebuy: auto`).

```json
{ "seat": 1, "amount": 1000, "stack_after": 1000 }
```

### `memory_update`

**Visibilidad:** **solo `spectator`**. Notas nuevas (privadas) que un
agente escribió sobre sus rivales tras un showdown.

```json
{ "updates": [ { "seat": 2, "notes": ["Payaso farolea seguido en el river"] } ] }
```

### `agent_error`

**Visibilidad:** **solo `spectator`**. Diagnóstico de cuándo el motor tuvo
que corregir, reintentar o caer a fallback una decisión de un LLM.

```json
{ "seat": 3, "error": "invalid_json", "fallback": true, "warnings": ["hallucinated_cards"] }
```

## 5. Mensajes cliente → servidor

| Mensaje | Quién puede enviarlo | Payload |
|---|---|---|
| `hello` | cualquiera, al conectar/reconectar | `{"role":"spectator"\|"player","seat":int?,"last_seq":int?}` |
| `control` | solo `spectator` | `{"cmd":"pause"\|"resume"\|"step"\|"speed","value":number?}` — controla el ritmo de la mesa (útil en demo/replay); `value` solo aplica a `speed` (multiplicador, p. ej. `2`). |
| `action` | solo el asiento `human` al que le toca jugar (**reservado / post-MVP**: en el MVP no hay asientos humanos) | `{"type":"fold"\|"check"\|"call"\|"bet"\|"raise"\|"all_in","amount":int?}` |

El servidor valida siempre contra `LegalActions` del motor antes de aplicar
cualquier `action` entrante; un `action` ilegal se descarta con un mensaje
de error (fuera del alcance de este documento en el MVP, ya que no hay
asiento humano activo).

## 6. Secuencia de una mano típica

1. `hand_start` (botón, ciegas, stacks)
2. `blind` × 2 (ciega pequeña, ciega grande)
3. `hole_cards` × N (una por asiento, filtrada por rol)
4. `street{preflop}` → por cada turno: `turn` → `thinking` → (`chat`?) → `action`
5. `street{flop}` → turnos como arriba
6. `street{turn}` → turnos como arriba
7. `street{river}` → turnos como arriba
8. si llega a showdown: `street{showdown}` → `showdown`
9. `pot_awarded`
10. `hand_end`
11. `rebuy` para quien haya quedado en 0 (si aplica)
12. entre manos, de forma asíncrona y de baja prioridad: `memory_update` por
    agente que jugó un showdown
13. vuelve a 1 con `hand_id` incrementado

`reasoning` y `agent_error` (solo `spectator`) pueden intercalarse en
cualquier punto cercano a un `turn`/`action` del asiento correspondiente.

## 7. Cómo desarrollar el frontend sin GPU

No se necesita Ollama ni GPU para trabajar en la interfaz:

- `pokerd replay recordings/<archivo>.jsonl --speed 2` sirve exactamente el
  mismo WebSocket (`/ws?role=...`) que el servidor en vivo. El recorder
  grabó el stream completo en **vista dios**; al reproducirlo, cada
  conexión pasa por el **mismo projector** que en vivo, así que un
  `player:<seat>` conectado a un replay ve exactamente lo que vería en una
  partida real — las hole cards ajenas, `reasoning`, `memory_update` y
  `agent_error` se siguen filtrando igual.
- Mientras el recorder no exista todavía (primeras fases), el equivalente
  provisional es `pokerd sim --mock --fast`: corre la simulación con el
  provider `mock` (determinista, sin red) a máxima velocidad y expone el
  mismo WebSocket en vivo. Es el *fallback* hasta que haya una grabación
  real.
- **Importante:** este documento no incluye un archivo de ejemplo en
  `recordings/`. La primera grabación real la genera el recorder del
  backend; hasta entonces, usar `pokerd sim --mock --fast` para probar el
  frontend contra el contrato aquí descrito.
- `docs/schema/*.json` (generado con `make schema` desde `internal/protocol`
  vía `invopop/jsonschema`) permite validar automáticamente cada `payload`
  contra su esquema, útil para tests de contrato en el cliente.

## 8. Versionado

- `v` en el envoltorio es el número de **protocolo**, no el de la app.
- Un cambio **aditivo** (nuevo campo opcional, nuevo `type` de evento, nuevo
  valor de enum que el cliente puede ignorar con seguridad) **no** sube `v`:
  sigue siendo `v1`.
- Un cambio **incompatible** (renombrar o quitar un campo, cambiar su tipo,
  cambiar el significado de uno existente) sube `v` a `2`, documentado aquí
  y en `docs/schema/`. Un cliente puede negociar o detectar la versión
  leyendo `v` en el primer mensaje recibido.

## 9. Diferencias con el frontend actual

El frontend ya empujado en `client/` (`Agent Royale`) es, según su propio
`README.md`, una **demo visual sin backend real** ("la actividad es
simulada... no hay conexión con modelos de IA ni con otros usuarios"). Su
modelo de dominio (`client/src/features/casino/model/casino.js`) resuelve un
problema distinto al de esta mesa de póker. Lista concreta de choques con
este contrato:

| Supuesto actual del cliente | Este contrato | Adaptación sugerida |
|---|---|---|
| Dos mesas (`table-1`, `table-2`) de **9 asientos** cada una, capacidad `TABLE_CAPACITY = 9`. | Una sola mesa de **5 asientos** fijos por sesión de servidor (config `seats:` en `config.yaml`). | Frontend: una vista de mesa por conexión WS, con el número de asientos que diga `snapshot.seats.length` (o `/api/config`), no una constante `9`. No hay "mesa creativa" ni múltiples mesas — cada mesa es un proceso/puerto `pokerd` distinto si se quieren varias. |
| Agentes con `role` de colaboración (`Coordinador`, `Investigadora`, `Desarrollador`, `Creativa`, `Analista`) y `color`/`kind` puramente decorativos elegidos por índice de asiento. | `Seat.kind` es `llm \| rulebot \| human` (tipo de jugador, no especialidad) y `persona_id`/`name`/`model` vienen de `configs/personas/*.yaml` vía `/api/config` y `snapshot`. | Frontend: sustituir `ROLES` por la lista de personas reales del backend (`/api/config`); usar `kind` para decidir el ícono (robot/humano) y `persona_id` para elegir avatar/color, no un índice arbitrario. |
| El usuario **añade/retira agentes desde la UI** (`AddAgentForm`, `agent/added`, `agent/removed`) y arranca/pausa la sesión con un botón (`session/toggled`). | La composición de la mesa (quién juega, con qué persona y qué modelo) se fija en `configs/config.yaml` **antes** de arrancar `pokerd`; no hay endpoint para añadir/quitar agentes en caliente en el MVP. `control{pause|resume|step|speed}` solo regula el *ritmo* de una partida ya en marcha, no quién se sienta. | Frontend: quitar (o marcar como "próximamente") los flujos de añadir/retirar agente y el botón de iniciar/pausar sesión pasa a mapear a `control{"cmd":"pause"\|"resume"}`. Si se quiere "añadir agente" desde UI, es trabajo de backend fuera de alcance de este contrato (anotado como mejora futura). |
| **No hay panel de chat** ("La actividad se representa sobre los agentes; no hay panel de chat", según el README). El único indicador es una burbuja `•••` cuando el `useTableSession` simulado marca turno. | El protocolo tiene un evento `chat` público explícito (`say`, máx. 120 caracteres) y es contenido real del juego (puede ser un farol), no cosmético. | Frontend: añadir una superficie para `chat` (aunque sea una burbuja sobre el asiento, ya hay precedente visual con `speaking`/`bubble`) en vez de descartar el texto. También conviene una superficie — aunque sea solo para `spectator` — para mostrar `reasoning` y `agent_error`, que hoy no existen en la UI. |
| El "turno" lo decide un `setInterval` cliente-side cada 3200 ms, en round-robin ciego sobre `table.agents` (`useTableSession.js`). | El turno lo decide el motor del servidor; el cliente solo refleja `turn`/`thinking`/`action` tal como llegan por WS. No hay temporización local. | Frontend: eliminar `useTableSession` (o reescribirlo como adaptador que consuma el WS) y pintar `speaking`/`thinking` a partir del evento `thinking` del seat indicado, no de un timer propio. |
| No hay concepto de cartas, bote, ciegas, calles ni stacks — es un dominio abstracto de "colaboración". | El dominio completo de Texas Hold'em: `board`, `pot`, `street`, hole cards, `legal_actions`, stacks, side pots, showdown. | Frontend: esta es la brecha más grande — hay que añadir render de cartas (formato `"Ah"` de dos caracteres, fácil de mapear a sprites), bote, fichas por asiento (`stack`/`bet_this_street`) y las calles. Nada de esto existe hoy en `PokerTable.jsx`, que solo pinta un tapete decorativo con palos estáticos (`['♠','♥','♣','♦','♠']`) sin relación con la mano real. |
| `agent.seat` es un índice elegido libremente por la UI al añadir el agente (`getAvailableSeat`), y es estable mientras no se borre el agente. | `seat` lo asigna la configuración del servidor (`configs/config.yaml`) y es estable durante toda la sesión del proceso `pokerd`; el cliente nunca elige asientos. | Frontend: el campo `seat` (nombre ya coincide, no hace falta renombrar) pasa de ser estado local mutable a un dato de solo lectura que llega en `snapshot`/`hand_start`. Buena noticia: el nombre `seat` ya es el mismo en ambos lados, así que el tipo `Seat` de este contrato puede sustituir directamente al `agent` local sin renombrar esa clave. |

**Nombres de campo reutilizados del cliente sin coste:** `seat` (índice de
asiento) y `name` ya se llaman igual en `casino.js` y en este contrato, así
que se mantuvieron tal cual en vez de inventar sinónimos. El resto de
campos del cliente (`role`, `color`, `kind` numérico decorativo, `running`)
no tienen equivalente limpio en el dominio de póker y no se reutilizaron:
forzarlos habría mezclado "especialidad de colaboración" con "tipo de
jugador de póker", que son conceptos distintos.
