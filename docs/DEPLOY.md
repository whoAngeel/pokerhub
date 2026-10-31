# Despliegue: local (desktop con GPU) y nube (Droplet de DigitalOcean)

Pokerhub corre en dos modos con el mismo binario (`pokerd`). Solo cambia la configuración:

| | **Local** (desktop) | **Nube** (Droplet) |
|---|---|---|
| Config | `configs/config.yaml` | `configs/api.yaml` |
| Modelos | `gemma4:e4b` en Ollama, en la propia GPU | Por API: OpenRouter (Gemma 4 26B, Claude Haiku, DeepSeek, Llama 4 Scout) |
| Claves | ninguna | `OPENROUTER_API_KEY` en `.env` |
| Coste | 0 $ (electricidad) | ~0.001 $ por mano (ver [Costes](#costes)) |
| Latencia por decisión | ~1.2 s | ~2–3 s |
| Necesita | GPU con ~7 GB de VRAM, Ollama | Droplet básico, sin GPU (1 vCPU / 1 GB basta) |
| Acceso | por Tailscale (`100.98.127.108`) | IP pública del Droplet |

Ambos sirven el mismo WebSocket ([PROTOCOL.md](PROTOCOL.md)), así que el frontend es el mismo: solo cambia la URL.

---

## Modo local (desktop con gemma)

Requisitos: Go ≥ 1.24, Ollama con `ollama pull gemma4:e4b`; recomendado `OLLAMA_KEEP_ALIVE=-1`.

```bash
# en la terminal (vista bonita):
go run ./cmd/pokerd sim --hands 3

# servidor WebSocket accesible por Tailscale:
go run ./cmd/pokerd serve --addr 100.98.127.108:8090
#   ws://100.98.127.108:8090/ws?role=spectator
```

Notas:
- No uses el puerto 8080 en el desktop: lo ocupa el dev server del cliente.
- `--addr 0.0.0.0:8090` también funciona, pero entonces escucha en todas las interfaces de la red local, no solo en la tailnet.
- `configs/config.yaml` tiene `reasoning_effort: none`, imprescindible para apagar el thinking de gemma en Ollama.

---

## Túnel de Cloudflare (ver desde fuera de la tailnet, con `pokerd tui`)

Para que alguien sin Tailscale vea la mesa desde la terminal (`pokerd tui`,
`docs/TUI_PLAN.md`), un túnel rápido de Cloudflare expone `localhost:8090`
con una URL `https://…trycloudflare.com` pública, sin tocar firewall ni DNS.
**Pon siempre un token** (ver `docs/PROTOCOL.md` §1 "Autenticación opcional
por token"): sin él, cualquiera con la URL puede mirar las cartas de todos y
pausar la partida.

1. En la máquina que corre el servidor, un `.env` con el token (forma
   recomendada: variable de entorno, vía `.env`; `--token` en la línea de
   comandos es solo el *override* explícito):
   ```bash
   echo 'POKERHUB_TOKEN=<secreto>' >> .env
   go run ./cmd/pokerd serve
   # o, sin GPU: go run ./cmd/pokerd replay --loop recordings/sample.jsonl
   ```
2. Levanta el túnel (no necesita cuenta de Cloudflare):
   ```bash
   cloudflared tunnel --url http://localhost:8090
   ```
   Imprime algo como `https://random-words-1234.trycloudflare.com`.
3. En la máquina remota (fuera de la tailnet), el mismo `.env` con el mismo
   token junto al binario `pokerd`:
   ```bash
   echo 'POKERHUB_TOKEN=<secreto>' >> .env
   go run ./cmd/pokerd tui --url wss://random-words-1234.trycloudflare.com
   ```
   `pokerd tui` acepta la URL que imprime `cloudflared` tal cual (`https://`
   se interpreta como `wss://`); `--token` también funciona ahí como
   override si el `.env` no está a mano, y `--env-file <path>` apunta a un
   `.env` en otra ruta (flag global, antes del subcomando:
   `pokerd --env-file ~/pokerhub.env tui --url wss://...`).

El túnel rápido es efímero (la URL cambia cada vez que se reinicia
`cloudflared`) y no tiene SLA -- suficiente para una demo, no para dejarlo
encendido días.

---

## Modo nube (Droplet de DigitalOcean)

### 1. Crear el Droplet
- Imagen: **Ubuntu 24.04 LTS**, plan Basic: 1 vCPU / 1 GB (~6 $/mes) es suficiente, porque la inferencia ocurre en OpenRouter.
- Añade tu clave SSH al crearlo.
- **Firewall de DigitalOcean** (Networking → Firewalls): permite **22** solo desde tu IP y **80/8090** según quién deba verlo (ver [Seguridad](#seguridad)).

### 2. Preparar el servidor
```bash
ssh root@<ip-droplet>
curl -fsSL https://get.docker.com | sh                  # Docker + compose plugin
git clone https://github.com/whoAngeel/pokerhub.git && cd pokerhub
```

### 3. Claves en `.env` (en el Droplet, nunca en el repo)
```bash
cat > .env <<'EOF'
OPENROUTER_API_KEY=sk-or-v1-...
EOF
chmod 600 .env
```
`.env` está en `.gitignore` y en `.dockerignore`: no se commitea ni se copia dentro de la imagen. Llega al contenedor como variables de entorno (`env_file`).

### 4. Levantar backend + cliente
```bash
docker compose -f deploy/docker-compose.droplet.yml up -d --build
docker compose -f deploy/docker-compose.droplet.yml logs -f pokerd
```

| Servicio | URL |
|---|---|
| Cliente web | `http://<ip-droplet>/` |
| WebSocket | `ws://<ip-droplet>:8090/ws?role=spectator` |
| Salud | `http://<ip-droplet>:8090/healthz` → `OK` |
| Config pública | `http://<ip-droplet>:8090/api/config` |

La partida se graba en el volumen `recordings` (`recordings/live.jsonl`).

### 5. Operar
```bash
docker compose -f deploy/docker-compose.droplet.yml stop      # parar (deja de gastar API)
docker compose -f deploy/docker-compose.droplet.yml start     # reanudar
git pull && docker compose -f deploy/docker-compose.droplet.yml up -d --build   # actualizar
```

Sin Docker también funciona: instala Go, `go build -o pokerd ./cmd/pokerd` y `./pokerd serve --config configs/api.yaml --addr 0.0.0.0:8090`. `pokerd` lee `.env` del directorio actual.

---

## Costes

`serve` juega **manos sin parar** mientras está encendido, aunque nadie esté mirando.

- Una mano ≈ 12–15 decisiones ≈ **0.001 $** con los modelos de `configs/api.yaml` (medido el 2026-10-31).
- Una mano dura ~30–45 s, así que salen **~0.10–0.15 $/hora**, unos **3 $/día** si se deja 24 h encendido.
- La clave de OpenRouter actual tiene un **límite de 1 $**: alcanza para ~8 h de partida continua. Después, los agentes LLM caen a check/fold (la partida sigue, pero sin razonamiento).

Recomendaciones:
- Pon un límite de gasto en la clave (https://openrouter.ai/keys).
- Para una demo, **para el servicio** cuando no se use (`stop`).
- Para no gastar nada, el frontend puede desarrollarse con `pokerd replay --loop recordings/sample.jsonl`.

## Seguridad

El servidor **no tiene autenticación por defecto**. Sin un token
configurado (`POKERHUB_TOKEN`/`--token`, ver `docs/PROTOCOL.md` §1),
cualquiera que conozca la URL puede:
- Ver la partida completa como espectador, incluidas todas las cartas y los razonamientos. Con solo bots no importa; con un humano sentado el espectador se bloquea (código 4003).
- **Pausar o reanudar la partida** con `control` desde el rol espectador.

No puede leer las claves: `/api/config` nunca expone URLs ni claves.

Opciones de menor a mayor esfuerzo:
0. **Token** (`POKERHUB_TOKEN` en `.env`, ver "Túnel de Cloudflare" arriba): gratis, un cambio de una línea, y es lo mínimo imprescindible en cuanto la URL sale de una red de confianza (tailnet/firewall).
1. **Firewall de DigitalOcean** limitando 80/8090 a las IPs del equipo.
2. **Tailscale en el Droplet** (`curl -fsSL https://tailscale.com/install.sh | sh && tailscale up`) y cerrar 80/8090 al público. Así queda igual que el desktop: solo la tailnet entra.
3. Proxy con TLS y autenticación básica (Caddy) delante de 80 y 8090 → `wss://`. Necesario si el cliente se sirve por `https`, porque el navegador bloquea `ws://` desde una página `https`.

## Elegir modelos

Cada personaje usa el modelo que diga su asiento en el YAML; la personalidad está en `configs/personas/*.yaml`, así que el mismo personaje puede cambiar de modelo.

```yaml
seats:
  - { type: llm, persona: personas/toro.yaml, provider: openrouter, model: "google/gemma-4-26b-a4b-it" }
```

Probado el 2026-10-31:

| Modelo (OpenRouter) | Latencia | Notas |
|---|---|---|
| `google/gemma-4-26b-a4b-it` | 2–4 s | JSON válido, personalidad fuerte |
| `anthropic/claude-haiku-5.5` | 3–5 s | El mejor razonamiento (pot odds bien usadas, ignora provocaciones) |
| `deepseek/deepseek-v4.1-flash` | 1.5–2 s | Rápido y correcto |
| `meta-llama/llama-4-scout` | 3–7 s | Correcto, algo lento |
| `meta-llama/llama-3.3-70b-instruct` | 5–8 s | Lento; algún timeout con 15 s |

**Google AI Studio (`GOOGLE_API_KEY`) con Gemma no funciona bien**: Gemma 4 piensa siempre (mete `<thought>` en la respuesta, tarda ~10 s y agota los tokens sin dar JSON) y Google no deja apagarlo (`Thinking budget is not supported for this model`). Por eso `configs/api.yaml` usa Gemma a través de OpenRouter. El bloque `google:` está comentado en `api.yaml` para usarlo con modelos **Gemini**, que sí soportan `json_schema` y `reasoning_effort: none`.

## Comprobación rápida tras desplegar

```bash
curl http://<host>:8090/healthz                     # OK
curl http://<host>:8090/api/config                  # asientos y modelos
node -e 'const w=new WebSocket("ws://<host>:8090/ws?role=spectator");w.onmessage=m=>console.log(JSON.parse(m.data).type)'
# esperado: snapshot, turn, thinking, reasoning, chat, action, ...
```

Si los agentes solo se retiran o pasan, revisa los logs: `agent_error … fallback:provider_error:… 401` indica una clave inválida; `fallback:timeout` indica un modelo lento (sube `table.decision_timeout_ms`).
