# STATUS — estado del proyecto y cómo continuar

> Última actualización: 2026-10-31. Documento de traspaso: con esto cualquier persona (o una sesión nueva de Claude) puede retomar el trabajo sin el historial de la conversación.
> Diseño completo: [PLAN.md](PLAN.md) · Contrato con el frontend: [PROTOCOL.md](PROTOCOL.md) · Convenciones: [../CLAUDE.md](../CLAUDE.md)

## Resumen en una línea

El backend juega partidas completas con agentes LLM reales y las sirve **en tiempo real por WebSocket** (`pokerd serve`), con filtrado por rol, grabación y replay sin GPU. **Lo siguiente es integrar el frontend** (`client/`) y luego memoria LLM / docker.

## Hecho ✅

| Fase | Qué | Dónde | Verificación |
|---|---|---|---|
| F0 | Setup, módulo `github.com/whoAngeel/pokerhub`, licencia MIT, configs, 4 personalidades | `go.mod`, `configs/` | — |
| F0.5 | Contrato WebSocket (16 eventos, roles, envoltorio, diferencias con el `client/` actual) | `docs/PROTOCOL.md` | revisado |
| F1 | Motor determinista: reglas NLHE, side pots, all-in incompleto, odd chip, evaluación, equity Monte Carlo, `MadeHand` en español | `internal/engine/` | tests: invariante 10k manos, anti-fuga 1000 manos, determinismo |
| — | Proveedores LLM: `openai` (Ollama/vLLM/LM Studio/OpenRouter…) con modos `structured` json_schema/json_object/prompt, `mock`, registry, cola GPU con prioridad, suite de conformidad | `internal/llm/` | 56 tests + integración real con Ollama |
| — | Agente LLM: prompt, esquema JSON, normalización determinista, 1 reintento, fallback check/fold, detector de cartas alucinadas, calentamiento | `internal/agent/` | tests + integración real |
| — | RuleBot determinista (asiento 0, futuro humano) | `internal/player/rulebot.go` | tests |
| F2 | Mesa/sesión: turnos, rebuy, botón, pausa/step, ritmo, eventos tipados, `DecisionView` sin fuga | `internal/table/` | tests: 200 manos, anti-fuga, timeout, acción ilegal |
| F2 | Config tipada + construcción de jugadores | `internal/config/` | tests |
| F5 | Estadísticas deterministas de rivales (VPIP, PFR, AF, faroles en river) | `internal/memory/stats.go` | tests |
| F2 | CLI `pokerd sim` + impresora en terminal + Makefile | `cmd/pokerd/`, `Makefile` | mano real contra Ollama OK |
| F3 | Tipos de cable + conversor + reductor de estado (`snapshot`) + JSON Schema | `internal/protocol/`, `docs/schema/`, `tools/gen-schema` | build + e2e |
| F3 | Filtro único por rol `server.Project`, Hub (seq, ring buffer, cliente lento), Echo `/healthz` `/api/config` `/ws` | `internal/server/` | test de visibilidad + e2e WS; verificado a mano: player:2 no recibe `reasoning` y su `snapshot` solo trae sus cartas |
| F3 | Recorder JSONL + replay con speed/pause/loop; `pokerd serve`, `replay`, `sim --record` | `internal/replay/`, `cmd/pokerd/` | test grabar→reproducir + prueba manual |
| F5 | Notas LLM privadas por rival tras cada showdown (baja prioridad, ≤3 por rival), en el prompt del observador; evento `memory_update` con `observer`; `table.memory_notes` | `internal/memory/notes.go`, `internal/table/memory.go` | 1 test de privacidad + partida real |
| — | Config "nube" `configs/api.yaml` (OpenRouter + Google AI Studio, un modelo por personaje) junto a la local `configs/config.yaml`; `pokerd` lee `.env` | `configs/api.yaml`, `cmd/pokerd/dotenv.go` | probado con claves reales (OpenRouter OK; Gemma en AI Studio descartado por thinking forzado, ver DEPLOY.md) |
| F3 | Partida real grabada para el frontend (2 manos, gemma4:e4b, 147 eventos) | `recordings/sample.jsonl` | cada línea parsea, `seq` creciente |

Calidad (ya aplicado): español forzado en todos los campos, números redondeados a 2 decimales en el prompt, calentamiento del modelo antes de la 1.ª mano (5.3 s → 1.2 s), temperatura de El Payaso 0.8, campo `reasoning.hand` con basura se reemplaza por `made_hand`.

Medido con `gemma4:e4b` en una RTX 4070: ~1.0–1.4 s por decisión; una mano ≈ 20–30 s; JSON válido en todas las pruebas.

## En curso 🚧

Nada en curso. Siguiente paso recomendado: **integración del frontend** (ver sección siguiente) usando `make replay` o `make run-mock`.

Pruebas de aceptación de la fase 3 (todas pasaron el 2026-10-31):

```bash
go run ./cmd/pokerd serve --mock --addr 127.0.0.1:8080
# en otra terminal (Node ≥ 22 trae WebSocket nativo):
node -e 'const w=new WebSocket("ws://127.0.0.1:8080/ws?role=player&seat=2");w.onmessage=m=>console.log(JSON.parse(m.data).type)'
```

Deudas técnicas pequeñas de la fase 3:
- `table.Session` recibe su sink en el constructor y no tiene `AddSink`, así que `cmd/pokerd` crea el Hub y el Recorder antes de la Session (ver `runServe`). Un `Session.AddSink` lo simplificaría.
- Se añadió un mensaje `error` servidor→cliente (respuesta a `action` reservado o `control` desconocido), documentado en `PROTOCOL.md §8.1`.
- El rechazo del espectador con código 4003 cuando hay un humano sentado está implementado pero sin test, y no es alcanzable desde la config porque aún no existe `type: human`.

## Pendiente ⏳

| Fase | Qué | Notas |
|---|---|---|
| F4 | Revisar que las 4 personalidades se distinguen en juego real; ajustar `strategy`/`voice` en `configs/personas/*.yaml` | Ya funcionan con modelo/proveedor por asiento |
| — | **Examen de razonamiento**: escenarios con respuesta obvia (nuts → no retirarse; 7-2 contra all-in → retirarse), en espejo mano fuerte/débil, nota por agente y modelo | Responde a "¿razonan o solo actúan el personaje?". Datos actuales: plan y acción coinciden en ~88 %, pero El Toro sube el 96 % de las veces |
| — | Desplegar en un Droplet de DigitalOcean con `deploy/docker-compose.droplet.yml` (imagen probada en local) y decidir el acceso (firewall o Tailscale) | Guía en [DEPLOY.md](DEPLOY.md). Ojo: `serve` gasta API sin parar (~3 $/día) |
| F6 | `docker compose` (app en contenedor, Ollama en el host vía `host.docker.internal:11434`) + `Dockerfile` | PLAN §9. Recortable: `make run` basta |
| F6 | `pokerd probe` (verifica un modelo: % JSON válido, latencia, español) + `docs/MODELS.md` | PLAN §6.1. Candidato a "good first issue" |
| — | **TUI con Charm** (Bubble Tea v2): visor en vivo, paneles de chat/razonamiento/memoria y, más adelante, jugar como humano | Plan completo en [TUI_PLAN.md](TUI_PLAN.md), ~4 h para T1+T2 |
| — | Asiento humano: `action{type,amount}` por WS → un `player.Player` "human" que espera el mensaje con timeout | Post-MVP; el protocolo ya lo reserva |
| — | Provider Anthropic nativo | Fuera del alcance; la interfaz `llm.Provider` + `llm.Register` lo admite |

## Integración con el frontend (`client/`)

`client/` (React, de un compañero) todavía es una demo visual sin conceptos de póker. La lista concreta de cambios está en `docs/PROTOCOL.md §9 "Diferencias con el frontend actual"`. Resumen:

- Sustituir los turnos falsos (`useTableSession.js`) por los eventos `turn`/`thinking`/`action` del WS.
- Calcular el número de asientos a partir de `snapshot.seats`, no con `TABLE_CAPACITY=9`.
- Quitar "añadir agente" desde la UI: la mesa la define `configs/config.yaml`.
- Añadir cartas, bote, calles, burbujas de `chat` y un panel de `reasoning` (rol spectator).

Para desarrollar sin GPU: `make replay` (partida real grabada, en bucle) o `make run-mock`. El cliente se conecta a `ws://<host>:8080/ws?role=spectator`; CORS ya permite el dev server en `:3000`.

## Problemas conocidos / riesgos

- **Calidad del modelo pequeño** (`gemma4:e4b`): a veces lee mal su mano en el texto del razonamiento o se repite en el chat. La acción siempre la valida el motor, así que nunca rompe la partida. Mejora posible: probar `qwen3`/`llama3.x` de ~8B por asiento.
- **Sin autenticación en el WS**: por defecto escucha en `127.0.0.1`; para Tailscale, cambiar `server.addr`. Solo fichas ficticias.
- `madehand.go`: la heurística de proyectos de escalera puede exagerar un proyecto ya bloqueado (solo es una pista para el LLM).
- Las sesiones de Claude se cortaron dos veces y los agentes perdieron su trabajo en curso: **commitea a menudo**.

## Cómo retomar

```bash
go vet ./... && go test -race ./...            # todo debe pasar
go run ./cmd/pokerd sim --mock --fast --hands 5 # sin GPU
go run ./cmd/pokerd sim --hands 1 --seed 7      # con Ollama (gemma4:e4b)
```

Requisitos: Go ≥ 1.24, Ollama con `gemma4:e4b`. En Ollama `/v1` es imprescindible `extra.reasoning_effort: none` (ya está en `configs/config.yaml`); sin eso el modelo gasta todos los tokens en "thinking" y devuelve el contenido vacío.

```bash
go run ./cmd/pokerd serve --mock                 # servidor WS sin GPU
make replay                                     # partida real grabada en bucle
```

Para retomar con Claude Code: *"Lee docs/STATUS.md, CLAUDE.md y docs/PLAN.md y continúa con lo pendiente."*

Reglas que no se rompen (CLAUDE.md): el motor es puro y determinista; `agent` no importa `engine`; todo lo que sale por el WS pasa por `server.Project`; `reasoning` nunca llega a otros agentes ni a roles que no sean spectator; claves de API solo por variable de entorno.
