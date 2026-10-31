# Pokerhub

**Orquestador de agentes LLM para evaluar su juicio bajo incertidumbre.** El primer escenario es una mesa de Texas Hold'em con fichas ficticias: varios agentes, cada uno con su personalidad y su modelo, **razonan** cada jugada en voz alta y hablan en la mesa.

El póker es solo el banco de pruebas. Lo que se mide es cómo decide cada modelo con información incompleta: si compara bien equity y pot odds, si lee a los rivales, si farolea con sentido y si mantiene un plan coherente. El razonamiento de cada decisión queda visible y grabado, así que se pueden comparar modelos y personalidades en las mismas condiciones (misma semilla, mismas cartas).

Cómo está montado:

- **Motor determinista en Go.** Las reglas no las decide ningún LLM. Los agentes solo *proponen* acciones y el motor las valida.
- **Cada agente ve solo lo suyo:** sus cartas y el estado público. Su `reasoning` es privado y solo lo ven los espectadores; `say` es lo único que oyen los demás.
- **Modelos intercambiables por asiento:** Ollama local, OpenRouter o cualquier endpoint compatible con OpenAI. Cada agente puede usar un modelo distinto.
- **Nunca se bloquea por un modelo:** salida JSON validada, un reintento y, si falla, fallback a check/fold.

Documentación:

- Despliegue local (GPU) y en la nube (DigitalOcean): [docs/DEPLOY.md](docs/DEPLOY.md)
- Contrato WebSocket para clientes: [docs/PROTOCOL.md](docs/PROTOCOL.md) (JSON Schema en `docs/schema/`)
- TUI: [docs/TUI_PLAN.md](docs/TUI_PLAN.md)
- Convenciones del backend: [CLAUDE.md](CLAUDE.md)

## Imágenes

![Captura de pantalla de Pokerhub](images/Captura%20de%20pantalla%202026-10-31%20163147.png)

![Imagen de Pokerhub](images/WhatsApp%20Image%202026-10-31%20at%204.42.52%20PM.jpeg)

## Agentes

Cuatro agentes LLM y un bot de reglas. Las personalidades viven en `configs/personas/*.yaml` y se pueden editar.

| Asiento | Agente | Estilo | Modelo local (`config.yaml`) | Modelo por API (`api.yaml`) |
|---|---|---|---|---|
| 0 | **Calculín** (RuleBot) | Reglas fijas, sin LLM. Sirve de referencia (y será el asiento humano) | — | — |
| 1 | **El Toro** | Fanfarrón y agresivo: juega muchas manos, sube y farolea | `gemma4:e4b` | `google/gemma-4-26b-a4b-it` |
| 2 | **La Abuela** | Muy conservadora: pocas manos, casi nunca farolea | `gemma4:e4b` | `anthropic/claude-haiku-5.5` |
| 3 | **El Profesor** | Analítico: decide con equity contra pot odds y cita porcentajes | `gemma4:e4b` | `deepseek/deepseek-v4.1-flash` |
| 4 | **El Payaso** | Impredecible: bromea, ve flops baratos y farolea en botes pequeños | `gemma4:e4b` | `meta-llama/llama-4-scout` |

En local, todos usan el mismo modelo (gemma en tu GPU) y solo cambia la personalidad. En `api.yaml`, cada personaje usa un modelo distinto para poder comparar el juicio de varios modelos en la misma mesa.

## Requisitos

- Go ≥ 1.24 (`go version`)
- Para modo local: [Ollama](https://ollama.com) y una GPU con ~7 GB de VRAM
- Para modo API: una clave de [OpenRouter](https://openrouter.ai/keys), sin GPU
- Sin ninguna de las dos: `--mock` (agentes simulados, sin llamadas a modelos)

## Modo local (Ollama, sin claves)

`configs/config.yaml` es la configuración por defecto y ya apunta a Ollama en `http://localhost:11434/v1`.

1. Descarga el modelo:
   ```bash
   ollama pull gemma4:e4b
   ```
   Recomendado: `OLLAMA_KEEP_ALIVE=-1` en el servicio de Ollama para no recargar el modelo entre manos.

2. Ver la partida en la terminal (vista espectador con razonamiento):
   ```bash
   go run ./cmd/pokerd sim --hands 3
   ```

3. O con la TUI. Son dos terminales: el servidor juega la partida y la TUI la muestra.
   ```bash
   # terminal 1: servidor con Ollama
   go run ./cmd/pokerd serve --addr 127.0.0.1:8090

   # terminal 2: TUI (por defecto se conecta a ws://127.0.0.1:8090)
   go run ./cmd/pokerd tui
   ```
   La TUI es de solo lectura y se cierra con `q`. Flags: `--url` (acepta `ws://`, `wss://`, `http(s)://`), `--token`, `--ascii` si tu terminal no muestra bien ♠♥♦♣. Se puede ver desde otra máquina por Tailscale: arranca `serve --addr <ip-tailnet>:8090` y en la otra máquina ejecuta `pokerd tui --url http://<ip-tailnet>:8090`.

`configs/config.yaml` lleva `reasoning_effort: none`. Es imprescindible: apaga el "thinking" nativo del modelo, porque el razonamiento va en el campo JSON `reasoning`.

## Modo API (OpenRouter, sin GPU)

`configs/api.yaml` pone cada agente en un modelo distinto de OpenRouter. Coste orientativo: ~0.001 $ por mano.

```bash
echo 'OPENROUTER_API_KEY=sk-or-...' >> .env      # pokerd lee .env al arrancar
go run ./cmd/pokerd sim   --config configs/api.yaml --hands 3
go run ./cmd/pokerd serve --config configs/api.yaml --addr 127.0.0.1:8090   # + pokerd tui
```

- Las claves van **solo** en variables de entorno o en `.env`, que está en `.gitignore`. El YAML solo guarda el nombre de la variable (`api_key_env: OPENROUTER_API_KEY`), nunca la clave.
- Otro fichero de entorno: `pokerd --env-file ~/pokerhub.env serve ...`.
- `POKERHUB_TOKEN` en `.env` (o `--token`) protege `/ws` y `/api/config` con `?token=`. La TUI lo lee del mismo `.env`.

## Cambiar modelos

Cada asiento indica su `provider` (definido en `providers:`) y su `model`:

```yaml
providers:
  local:
    type: openai
    base_url: http://localhost:11434/v1
    structured: json_schema
    extra: { reasoning_effort: none }
  openrouter:
    type: openai
    base_url: https://openrouter.ai/api/v1
    api_key_env: OPENROUTER_API_KEY
    extra: { reasoning: { enabled: false } }

seats:
  - { type: rulebot, name: "Calculín" }
  - { type: llm, persona: personas/toro.yaml,   provider: local,      model: "qwen3:8b" }
  - { type: llm, persona: personas/abuela.yaml, provider: openrouter, model: "anthropic/claude-haiku-5.5" }
```

- **Cambiar el modelo de un agente:** edita el `model` de su asiento. En local, antes descarga el modelo con `ollama pull <modelo>`. En OpenRouter, elige uno del [catálogo](https://openrouter.ai/models) que soporte *structured outputs*.
- **Mezclar local y API:** cada asiento puede usar un provider distinto en la misma mesa.
- **Qué modelos sirven:** cualquiera servido por un endpoint compatible con OpenAI (Ollama, vLLM, llama.cpp `server`, LM Studio, OpenRouter, Groq…). Hace falta que siga instrucciones y devuelva JSON. Si el servidor no soporta `json_schema`, usa `structured: json_object` o `structured: prompt`. Conviene poder apagar el thinking nativo del modelo (`reasoning_effort: none` en Ollama, `reasoning.enabled: false` en OpenRouter).
- **Personalidades:** edita o crea `configs/personas/*.yaml` (`name`, `voice`, `strategy`, `temperature`, `reasoning: rich|short`, `chattiness`) y apúntalas desde `seats`.
- **Provider nuevo en Go** (para APIs no compatibles con OpenAI): implementa `llm.Provider` y regístralo con `llm.Register`.
- **Partidas comparables:** `--seed N` (o `table.seed`) reparte las mismas cartas, y así se puede comparar el mismo escenario con modelos distintos.

## Comandos

```bash
go run ./cmd/pokerd sim --mock --fast --hands 20     # sin GPU: agentes mock, sin pausas
go run ./cmd/pokerd sim --hands 3 --seed 7           # partida reproducible
go run ./cmd/pokerd serve --record recordings/mi-partida.jsonl
go run ./cmd/pokerd replay --speed 2 --loop recordings/sample.jsonl   # partida grabada, sin GPU
go run ./cmd/pokerd tui --url http://127.0.0.1:8090
```

Flags comunes de `sim` y `serve`: `--config`, `--seed`, `--fast`, `--mock` y `--record`. `serve` y `replay` aceptan también `--addr` y `--token`.

Atajos: `make sim`, `make sim-mock`, `make run`, `make run-mock`, `make replay`, `make test`, `make schema`.

`recordings/sample.jsonl` es una partida real (2 manos, `gemma4:e4b`) para desarrollar clientes sin GPU.

### Endpoints

| Endpoint | Qué |
|---|---|
| `GET /healthz` | `OK` |
| `GET /api/config` | ciegas, buy-in y asientos (nombre, persona, tipo, modelo) |
| `GET /ws?role=spectator` | vista dios: todas las cartas + `reasoning` |
| `GET /ws?role=player&seat=N` | solo las cartas del asiento N; nunca `reasoning` |

Sin `POKERHUB_TOKEN` no hay autenticación: exponlo solo en una red de confianza (por ejemplo, Tailscale).

### Tests

```bash
go vet ./...
go test -race ./...
go test -tags integration ./internal/llm/ ./internal/agent/ -run Integration -v   # opcional, contra Ollama real
```

Los tests normales no tocan red ni GPU.

## Estructura

```text
cmd/pokerd/            # Binario: sim | serve | replay | tui
internal/
  engine/              # Motor determinista (sin I/O ni LLM)
  player/              # Contrato de turno (DecisionView/Decision) + RuleBot
  agent/               # Agente LLM: prompt, esquema, validación, fallback
  llm/                 # Providers de modelos + cola de GPU
  table/               # Orquestador de la sesión (turnos, rebuy, eventos)
  memory/              # Estadísticas y notas sobre los rivales
  protocol/            # Eventos WebSocket (fuente de verdad)
  server/              # WebSocket + proyección por rol
  replay/              # Reproducción de grabaciones .jsonl
  tui/                 # Visor en terminal (Bubble Tea)
  config/              # Carga de configs/*.yaml
configs/
  config.yaml          # Local: Ollama + gemma
  api.yaml             # Nube: OpenRouter, un modelo por agente
  personas/*.yaml      # Personalidades de los agentes
docs/                  # DEPLOY, PROTOCOL, TUI_PLAN, schema/
client/                # Cliente web React + Vite
```

## Cliente web

```bash
cd client
npm ci
npm run dev          # http://127.0.0.1:3000/
npm test && npm run build
```

Detalles en [client/README.md](client/README.md). El cliente sigue en modo demostración y todavía no está conectado al backend Go. En el desktop, el dev server del cliente puede ocupar el 8080; por eso los ejemplos usan el 8090 para `pokerd`.
