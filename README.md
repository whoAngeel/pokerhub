# Pokerhub

Mesa de Texas Hold'em open source (solo fichas ficticias) donde agentes LLM con personalidad **razonan** sus jugadas y hablan en la mesa. El motor de juego es código determinista en Go; los LLM solo proponen acciones que el motor valida. Funciona 100 % local con [Ollama](https://ollama.com), sin claves de API.

- Estado actual y qué falta: [docs/STATUS.md](docs/STATUS.md)
- Plan y arquitectura: [docs/PLAN.md](docs/PLAN.md)
- Contrato WebSocket para el frontend: [docs/PROTOCOL.md](docs/PROTOCOL.md)
- Convenciones del backend: [CLAUDE.md](CLAUDE.md)

## Estado

| Pieza | Estado |
|---|---|
| Motor (reglas, side pots, evaluación, equity) | ✅ listo, con tests |
| Proveedores LLM (Ollama / cualquier endpoint OpenAI-compatible, mock) | ✅ listo |
| Agente LLM (prompt, JSON validado, reintento, fallback) + RuleBot | ✅ listo |
| Mesa + simulación en terminal (`pokerd sim`) | ✅ listo |
| Servidor WebSocket (`pokerd serve`) + replay + grabación | ✅ listo |
| Memoria de rivales (stats + notas) | ✅ stats · ⏳ notas LLM |

## Estructura

```text
pokerhub/
  go.mod, go.sum          # Modulo y dependencias del codigo central en Go
  CLAUDE.md              # Convenciones del backend y su arquitectura
  cmd/pokerd/            # Binario: sim | serve | replay
  internal/
    engine/              # Motor determinista (sin I/O ni LLM)
    player/              # Contrato de turno (DecisionView/Decision) + RuleBot
    agent/               # Agente LLM: prompt, esquema, validacion, fallback
    llm/                 # Proveedores de modelos + cola de GPU
    table/               # Orquestador de la sesion (turnos, rebuy, eventos)
    memory/              # Estadisticas de rivales
    config/              # Carga de configs/config.yaml
  configs/
    config.yaml          # Servidor, proveedores, mesa y asientos
    personas/*.yaml      # Personalidades de los agentes (editables)
  docs/                  # PLAN.md y PROTOCOL.md
  client/                # Cliente web React + Vite
    src/app/             # Composicion y layout
    src/features/        # Casino, agentes y temas
    src/shared/          # Componentes reutilizables
    package.json         # Comandos y dependencias exclusivos del cliente
```

El codigo central en Go permanece en la raiz del repositorio y conserva su estructura prevista en CLAUDE.md. El cliente se mantiene aislado en `client/`, sin un repositorio Git anidado ni configuracion de despliegue de Sites.

## Backend en Go

### Requisitos

- Go ≥ 1.24 (`go version`)
- Ollama con un modelo local. Por defecto se usa `gemma4:e4b` (~7 GB de VRAM):
  ```bash
  ollama pull gemma4:e4b
  ```
  Recomendado: `OLLAMA_KEEP_ALIVE=-1` en el servicio de Ollama para no recargar el modelo entre manos.
- Sin GPU u Ollama puedes usar `--mock`, con agentes simulados y sin llamadas a modelos.

### Tests

```bash
go vet ./...
go test -race ./...
```

Los tests no tocan red ni GPU. Las pruebas contra un Ollama real son opcionales:

```bash
go test -tags integration ./internal/llm/ ./internal/agent/ -run Integration -v
```

### Ver jugar a los bots en la terminal

```bash
# Sin GPU: agentes mock, sin pausas, 20 manos
go run ./cmd/pokerd sim --mock --fast --hands 20

# Con Ollama: 4 agentes LLM + 1 RuleBot, razonamiento visible (vista espectador)
go run ./cmd/pokerd sim --hands 3

# Partida reproducible
go run ./cmd/pokerd sim --hands 3 --seed 7
```

Flags: `--config` (por defecto `configs/config.yaml`), `--hands N`, `--seed N`, `--fast` (sin pausas entre acciones), `--mock` (todos los proveedores pasan a mock).

### Servidor para el frontend

```bash
go run ./cmd/pokerd serve                    # partida real con Ollama, WebSocket en server.addr
go run ./cmd/pokerd serve --mock             # sin GPU (agentes mock)
go run ./cmd/pokerd serve --record recordings/mi-partida.jsonl
go run ./cmd/pokerd replay --speed 2 --loop recordings/sample.jsonl   # sin GPU, partida real grabada
```

Atajos: `make run`, `make run-mock`, `make replay`. Flags de `serve`: `--addr host:port`, `--seed`, `--fast`, `--mock`, `--record`. `sim` también acepta `--record`.

Endpoints (contrato completo en [docs/PROTOCOL.md](docs/PROTOCOL.md); JSON Schema en `docs/schema/`):

| Endpoint | Qué |
|---|---|
| `GET /healthz` | `OK` |
| `GET /api/config` | ciegas, buy-in y asientos (nombre, persona, tipo, modelo) |
| `GET /ws?role=spectator` | vista dios: todas las cartas + `reasoning` |
| `GET /ws?role=player&seat=N` | solo las cartas del asiento N; nunca `reasoning` |

Prueba rápida desde el navegador o con Node:

```js
const ws = new WebSocket("ws://127.0.0.1:8080/ws?role=spectator");
ws.onmessage = (m) => console.log(JSON.parse(m.data));
```

`recordings/sample.jsonl` es una partida real (2 manos, `gemma4:e4b`) para desarrollar el frontend sin GPU. Por defecto se escucha en `127.0.0.1:8080`. Para abrirlo desde otra máquina por Tailscale, usa `--addr 0.0.0.0:8080` o cambia `server.addr`. No tiene autenticación: exponlo solo en una red de confianza.

### Configuración

Todo vive en `configs/config.yaml`:

```yaml
providers:
  local:                       # Ollama
    type: openai
    base_url: http://localhost:11434/v1
    extra: { reasoning_effort: none }   # apaga el "thinking" nativo (imprescindible)
seats:
  - { type: rulebot, name: "Calculín" }
  - { type: llm, persona: personas/toro.yaml, provider: local, model: "gemma4:e4b" }
```

- **Otro modelo u otro servidor:** cualquier endpoint compatible con OpenAI (vLLM, llama.cpp `server`, LM Studio, OpenRouter, Groq…) se añade como provider con `type: openai` y su `base_url`. Cada asiento puede usar un provider y un modelo distintos. Si el servidor no soporta `json_schema`, usa `structured: json_object` o `structured: prompt`.
- **Claves de API:** solo por variable de entorno, con `api_key_env: OPENROUTER_API_KEY`. Nunca en el YAML.
- **Personalidades:** edita o crea `configs/personas/*.yaml` (`name`, `voice`, `strategy`, `temperature`, `reasoning: rich|short`, `chattiness`).
- **Provider nuevo en Go** (APIs no compatibles con OpenAI): implementa `llm.Provider` y regístralo con `llm.Register`. Ver [docs/PLAN.md §6.1](docs/PLAN.md).

## Ejecutar el cliente

Desde PowerShell:

```powershell
cd client
npm.cmd ci
npm.cmd run dev
```

Abrir http://127.0.0.1:3000/.

## Verificar el cliente

```powershell
cd client
npm.cmd test
npm.cmd run build
npm.cmd run format:check
```

La documentacion detallada de la interfaz esta en [client/README.md](client/README.md).

## Integracion

El cliente conserva su modo de demostracion: dos mesas, nueve asientos por mesa, sesiones independientes y temas visuales. No se ha conectado al backend Go. Las reglas en `client/src/features/casino/model/` administran la demostracion visual; no sustituyen al motor de poker del servidor. La integracion futura debe respetar los contratos y las reglas de proyeccion documentados por el backend.