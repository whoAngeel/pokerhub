# CLAUDE.md — convenciones del proyecto

Mesa de Texas Hold'em (fichas ficticias) con agentes LLM que razonan. Backend en Go. Plan completo: `docs/PLAN.md`. Contrato con el frontend: `docs/PROTOCOL.md`.

## Arquitectura (reglas duras)
- `internal/engine` es **puro y determinista**: sin I/O, sin red, sin LLM, sin goroutines, sin `time.Now()`. La aleatoriedad entra solo como `*rand.Rand` sembrado.
- Ninguna decisión de reglas la toma un LLM. El motor valida **toda** acción; los agentes solo proponen.
- `internal/agent` **no importa** `internal/engine`. Recibe `player.DecisionView` (DTO) y devuelve `player.Decision`.
- Un agente solo ve sus hole cards + estado público. La única fuente de vistas es `engine.(*Hand).ViewFor(seat)`.
- Todo lo que sale por WebSocket pasa por `server.Project(event, role)`. No se envían eventos sin proyectar.
- `reasoning` es privado: nunca entra en el prompt de otro agente ni sale a roles distintos de `spectator`. `say` es lo único público de un agente.
- Los providers (`internal/llm`) no saben nada de póker: reciben `System`, `User` y `Schema`, y devuelven texto.
- Los eventos se definen en `internal/protocol`, que es la fuente de verdad. Si cambia un evento, se actualizan `docs/PROTOCOL.md` y el JSON Schema en el mismo cambio.

## Código
- Go ≥ 1.24. `gofmt`, `go vet ./...` y `go test ./...` deben pasar antes de dar algo por terminado.
- **Tests: solo los esenciales** (hackathon). Uno por funcionalidad nueva: el camino feliz o el invariante crítico. Nada de suites exhaustivas. Los tests de seguridad que ya existen se mantienen: invariante de fichas del motor, anti-fuga de información y visibilidad del projector.
- Errores con `fmt.Errorf("...: %w", err)`; errores centinela tipados donde el llamador decida (`llm.ErrRateLimit`, etc.).
- Sin dependencias nuevas sin justificarlas. Aprobadas: echo v4, coder/websocket, openai-go v3, yaml.v3, chehsunliu/poker, invopop/jsonschema, charm.land/bubbletea/v2, charm.land/lipgloss/v2, charm.land/bubbles/v2 (TUI, `internal/tui`/`pokerd tui`, docs/TUI_PLAN.md).
- Identificadores y comentarios de código en inglés; textos de la mesa, prompts y docs en español.
- Fichas como `int` (unidades enteras), nunca `float`.

## LLM
- Thinking nativo del modelo **siempre apagado** (`reasoning_effort: none` en Ollama `/v1`). El razonamiento va en el campo JSON `reasoning`.
- Salida estructurada → validación → normalización determinista → 1 reintento → fallback check/fold. Nunca se bloquea la partida por un modelo.
- Los tests no tocan red ni GPU: usan `llm/mock` o `httptest`.
- Las claves de API solo por variable de entorno (`api_key_env`), nunca en YAML ni en el repo.

## Comandos
- `make test` · `make run` (server) · `make sim` (bots en terminal) · `make replay` · `make schema` (genera `docs/schema/`)

## Git
- Conventional Commits (`feat(engine): ...`, `fix(agent): ...`, `docs: ...`).
