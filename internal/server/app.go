package server

import (
	"crypto/subtle"
	"net/http"
	"strconv"

	"github.com/coder/websocket"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// NewApp builds the Echo app: GET /healthz, GET /api/config, GET /ws
// (PROTOCOL.md §1). debug mirrors configs/config.yaml's server.debug
// (allows a spectator connection even with a human seated). token, when
// non-empty, is required as the ?token= query param on /ws and /api/config
// (constant-time compare); /healthz always stays open. An empty token
// preserves the previous (no-auth) behavior -- needed so a quick Cloudflare
// tunnel (docs/DEPLOY.md "Túnel de Cloudflare") isn't wide open to anyone
// with the URL.
func NewApp(hub *Hub, ctrl Controller, pub PublicConfig, debug bool, token string) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.Use(middleware.Recover())
	// Log the path only, never the raw URI: it carries ?token=<secret>.
	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: `{"time":"${time_rfc3339}","remote_ip":"${remote_ip}","method":"${method}","path":"${path}","status":${status},"latency":"${latency_human}","error":"${error}"}` + "\n",
	}))
	e.Use(middleware.CORS()) // any origin for GET -- frontend dev runs on :3000

	e.GET("/healthz", func(c echo.Context) error {
		return c.String(http.StatusOK, "OK")
	})
	e.GET("/api/config", func(c echo.Context) error {
		if !checkToken(c, token) {
			return c.String(http.StatusUnauthorized, "unauthorized")
		}
		return c.JSON(http.StatusOK, pub)
	})
	e.GET("/ws", func(c echo.Context) error {
		if !checkToken(c, token) {
			return c.String(http.StatusUnauthorized, "unauthorized")
		}
		return handleWS(c, hub, ctrl, pub, debug)
	})
	return e
}

// checkToken reports whether the request may proceed: always true when no
// token is configured (previous no-auth behavior), otherwise a
// constant-time comparison against ?token=. Never logs the token.
func checkToken(c echo.Context, token string) bool {
	if token == "" {
		return true
	}
	got := c.QueryParam("token")
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

// handleWS accepts the WebSocket upgrade, applies the spectator-with-a-
// human-seated refusal (PROTOCOL.md §1: close code 4003 unless debug), and
// otherwise hands the connection to serveConn for its whole lifetime.
func handleWS(c echo.Context, hub *Hub, ctrl Controller, pub PublicConfig, debug bool) error {
	role, ok := parseRole(c, pub)
	if !ok {
		return c.String(http.StatusBadRequest, "invalid role/seat")
	}

	conn, err := websocket.Accept(c.Response(), c.Request(), &websocket.AcceptOptions{
		// Origin is already governed by the CORS middleware above (any
		// origin, matching "frontend dev runs on :3000"); websocket's own
		// origin check would otherwise reject a cross-origin upgrade.
		InsecureSkipVerify: true,
	})
	if err != nil {
		return err
	}

	if role.IsSpectator() && pub.HasHumanSeat() && !debug {
		_ = conn.Close(websocket.StatusCode(4003), "spectator deshabilitado: hay un humano jugando")
		return nil
	}

	serveConn(c.Request().Context(), conn, hub, ctrl, role)
	return nil
}

// parseRole reads ?role=spectator|player&seat=N (PROTOCOL.md §1). An
// invalid/missing role, or a player seat outside [0,len(pub.Seats)), fails.
func parseRole(c echo.Context, pub PublicConfig) (Role, bool) {
	switch c.QueryParam("role") {
	case "spectator":
		return Spectate(), true
	case "player":
		seat, err := strconv.Atoi(c.QueryParam("seat"))
		if err != nil || seat < 0 || seat >= len(pub.Seats) {
			return Role{}, false
		}
		return PlayerSeat(seat), true
	default:
		return Role{}, false
	}
}
