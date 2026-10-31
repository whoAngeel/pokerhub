# Agent Royale - Cliente de Pokerhub

Casino visual desarrollado con React y Vite. Incluye dos mesas separadas, nueve asientos por mesa, equipos y sesiones independientes, gestión de agentes y tres temas: Halloween (predeterminado), Casino clásico y Cyberpunk. La actividad se representa sobre los agentes; no hay panel de chat.

## Desarrollo

```sh
npm install
npm run dev
```

En PowerShell con scripts restringidos, utiliza `npm.cmd`. Abre `http://127.0.0.1:3000`. El puerto 5173 está reservado por Windows en este equipo. Para cambiarlo: `npm.cmd run dev -- --port 3001`.

## Comandos

```sh
npm run build
npm run preview
npm test
npm run format
npm run format:check
```

## Organización

```text
src/
  main.jsx                  # Entrada y StrictMode
  app/
    App.jsx                 # Composición de la aplicación y navegación
    styles.css              # Base visual, layout y formularios
  features/
    agents/components/      # Avatar, formulario y detalle
    casino/
      components/           # Sala y mesa reutilizable
      hooks/                # Estado React y actividad simulada por mesa
      model/                # Reglas puras de capacidad, asientos y sesiones
      styles/               # Arte pixelado y distribución de la sala
    themes/                 # Catálogo, selector y persistencia del tema
  shared/components/        # Diálogo nativo con foco y cierre mediante Escape
```

`casinoReducer` es la única entrada para modificar equipos y sesiones. Valida la capacidad, los asientos y los datos de los agentes sin depender de React. Cada agente mantiene un asiento estable; retirar otro agente no reorganiza la mesa. Las pruebas con `node:test` cubren aislamiento entre mesas, límites, asientos y detención al retirar al último agente.

`useCasino` conecta las reglas con React; `useTableSession` encapsula el temporizador de demostración y lo limpia al pausar o desmontar. Los componentes reciben datos y callbacks. `useTheme` valida el valor guardado y tolera la ausencia de localStorage. Prettier mantiene un formato consistente.

## Alcance

La actividad es simulada en este navegador. Los equipos y sesiones viven en memoria y se reinician al recargar; solo la temática se conserva. Para agentes reales se necesita un servidor con eventos WebSocket/SSE y adaptadores de IA. Las credenciales deben permanecer en el servidor. Las fuentes de Google Fonts tienen alternativas locales si la red no está disponible.

Este cliente vive en `pokerhub/client/`. El codigo central Go permanece en la raiz de `pokerhub/`. Ejecuta los comandos npm desde esta carpeta. Las reglas locales del casino pertenecen a la demostracion visual y no sustituyen al motor del servidor.
