# Pokerhub

## Estructura

```text
pokerhub/
  go.mod, go.sum          # Modulo y dependencias del codigo central en Go
  CLAUDE.md              # Convenciones del backend y su arquitectura
  client/                # Cliente web React + Vite
    src/app/             # Composicion y layout
    src/features/        # Casino, agentes y temas
    src/shared/          # Componentes reutilizables
    package.json         # Comandos y dependencias exclusivos del cliente
```

El codigo central en Go permanece en la raiz del repositorio y conserva su estructura prevista en CLAUDE.md. El cliente se mantiene aislado en `client/`, sin un repositorio Git anidado ni configuracion de despliegue de Sites.

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