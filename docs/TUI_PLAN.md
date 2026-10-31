# Plan — TUI con Charm (charm.land)

> Estado: **PLAN, sin implementar**. Fecha: 2026-10-31.
> Objetivo: ver la mesa en vivo en la terminal (también desde la Mac por Tailscale/SSH) y, más adelante, jugar en el asiento humano. Todo con Bubble Tea, sin navegador.

## Por qué encaja

- La TUI es **un cliente más del WebSocket** (`docs/PROTOCOL.md`), igual que el frontend web. No necesita lógica nueva de póker y valida el protocolo antes que la web.
- Puede reutilizar `internal/protocol`: los tipos de los eventos y el reductor `TableState.Apply(env)`, que ya reconstruye el estado de la mesa a partir del `snapshot` y los eventos. La TUI solo dibuja ese estado.
- La visibilidad ya la resuelve el servidor (`server.Project`). La TUI de espectador ve todo y la de jugador solo ve lo suyo, sin código extra.
- Funciona con `serve` (partida en vivo) y con `replay` (sin GPU).

## Stack

| Pieza | Módulo | Uso |
|---|---|---|
| Bubble Tea v2 | `charm.land/bubbletea/v2` | loop Model/Update/View |
| Lip Gloss v2 | `charm.land/lipgloss/v2` | estilos: cartas con color por palo, cajas de asientos, layout |
| Bubbles v2 | `charm.land/bubbles/v2` | `viewport` (log/chat con scroll), `spinner` (agente pensando), `help` + `key` (atajos) |
| WebSocket | `github.com/coder/websocket` (ya en go.mod) | cliente WS |

Antes de implementar, verificar en la documentación de v2 la firma de `View()` del modelo raíz (en v2 devuelve `tea.View`; los componentes de Bubbles devuelven `string`). Hay que añadir estas 3 dependencias a la lista aprobada de `CLAUDE.md`.

## Arquitectura

```
cmd/pokerd/tui.go            # subcomando: pokerd tui --url ws://host:8090/ws?role=spectator
internal/tui/
  client.go                  # conexión WS → tea.Msg (envMsg, connErrMsg); reconexión con hello{last_seq}
  model.go                   # Model: protocol.TableState + log + UI state; Update(msg)
  view_table.go              # mesa: 5 asientos alrededor, board, bote, botón, apuestas
  view_side.go               # panel derecho: chat / razonamiento / memoria (pestañas)
  styles.go                  # Lip Gloss: palos ♠♥♦♣ con color, asiento activo, all-in, folded
  keys.go                    # atajos (bubbles/key + help)
```

Flujo: una goroutine lee el WS y hace `program.Send(envMsg{env})`. `Update` aplica `state.Apply(env)`, añade una línea al log o al chat según el tipo, y marca quién está pensando (`thinking` → spinner hasta su `action`). `View` dibuja el estado. Las teclas de control envían `control{...}` por el mismo WS.

## Layout (≥ 110×32; modo compacto si es más pequeño)

```
┌─ Mano #12 · flop · Ciegas 5/10 ──────────────────────────────┬─ Chat ─ Razonamiento ─ Memoria ─┐
│              [El Toro 820]  K♠ Q♦         [La Abuela 990]    │ 💬 Toro: ¡Paga, mosca!           │
│                 bet 130                       folded         │ 🧠 Profesor (1.1s)               │
│  [Calculín 950]      ┌ 3♠ Q♦ T♣ ┐     [El Profesor 1000]     │   odds: equity 0.26 < 0.31       │
│     call 130         └ bote 550 ┘        ⠋ pensando…         │   plan: me retiro                │
│                  [El Payaso 820] ⓓ                            │                                  │
├─ Log ─────────────────────────────────────────────────────────┴──────────────────────────────────┤
│ Toro apuesta 130 · Payaso iguala 130 · Calculín iguala 130                                        │
└ espacio pausa · n paso · +/- velocidad · tab panel · q salir ────────────────────────────────────┘
```

- Las cartas propias o reveladas aparecen en color: ♥♦ en rojo, ♠♣ en blanco. Las ocultas se dibujan como `🂠 🂠`.
- El asiento que debe actuar se resalta; si está pensando, muestra un spinner. Los asientos que se retiraron se ven atenuados.
- Panel de razonamiento: solo con rol `spectator`. Se muestran los últimos N razonamientos (hand/odds/reads/plan), con su latencia y avisos.
- Panel de memoria: `memory_update`, con el formato "observador → rival: nota".

## Fases

| # | Qué | Esfuerzo | "Hecho cuando…" |
|---|---|---|---|
| T1 | Visor solo lectura: cliente WS, `TableState`, mesa + log, cartas con estilo | 2–3 h | `pokerd tui` muestra en vivo una partida de `serve --mock` y de `replay` |
| T2 | Paneles chat / razonamiento / memoria + controles (`control` pause/resume/step/speed) + help | 1–1.5 h | Se puede pausar y avanzar paso a paso desde la TUI |
| T3 | **Jugar como humano** (asiento 0): rol `player&seat=0`, botones de acciones legales con monto (textinput), timeout visible | 3 h (incluye backend) | Una partida completa jugando yo contra los 4 agentes |
| T4 | Pulido: animación al repartir, resaltar al ganador, modo compacto, tema claro/oscuro | opcional | — |

### T3 necesita trabajo en el backend (hoy no existe)
1. `player.Human`: un `Player` cuyo `Decide` espera un `action` del WS para su asiento, con timeout `decision_timeout_ms` y fallback a check/fold.
2. Que `config` acepte `type: human` en un asiento.
3. Que el servidor enrute `action{type,amount}` desde la conexión `player&seat=N` hacia ese `Human`. Ese mensaje ya está reservado en `PROTOCOL.md §5`; hoy el servidor responde `error`.
4. Con un humano sentado, el espectador queda bloqueado con código 4003, salvo `debug: true`. Eso ya está implementado; es la protección contra hacer trampas mirando el modo espectador.

## Cómo se usará desde la Mac

```bash
# Opción A: TUI en la Mac (requiere Go en la Mac), servidor en el PC
pokerd tui --url ws://100.98.127.108:8090/ws?role=spectator

# Opción B: todo en el PC por SSH (no necesita nada en la Mac)
ssh angel-pc -t 'cd ~/DEV/repositories/atzomx-hackathon && go run ./cmd/pokerd tui --url ws://127.0.0.1:8090/ws?role=spectator'
```

Sin `--url`, `pokerd tui` podría arrancar el servidor y la partida en el mismo proceso, con los mismos flags que `serve` (`--mock`, `--config`). Así un solo comando bastaría para jugar.

## Tests (solo esenciales)

Un único test: pasar `recordings/sample.jsonl` por `Model.Update` y comprobar que el `View()` final contiene el board y el ganador de la última mano. Si se hace T3, otro test para el `player.Human`: un `action` recibido se convierte en la `Decision` correcta, y si no llega, salta el timeout y hace fallback.

## Riesgos

| Riesgo | Mitigación |
|---|---|
| Terminal pequeña o sin Unicode | Modo compacto por debajo de 110×32; letras `s h d c` si `--ascii` |
| Emojis en el chat descuadran columnas | Medir el ancho con `lipgloss.Width` y recortar |
| La API v2 de Bubble Tea cambia | Verificar en la doc de v2 antes de empezar; versiones fijadas en go.mod |
| Cortes de WS | Reconectar con `hello{last_seq}` (el servidor ya lo soporta) |
