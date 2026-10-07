package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
)

// notify wakes this instance's event streams and advances the restaurant
// revision, which polling screens on any instance compare before reloading.
func (a *App) notify(ctx context.Context, tenant, session string) {
	a.hub.publish(tenant, session)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if _, err := a.control.Exec(ctx, "UPDATE tenants SET revision=revision+1 WHERE id=$1", tenant); err != nil {
		slog.Warn("revisão do restaurante não registrada", "tenant", tenant, "error", err)
	}
}

// revision answers polling screens with one query to the central database and
// no restaurant connection; they fetch the full state only when it changes.
func (a *App) revision(c echo.Context) error {
	kind := "customer"
	if c.QueryParam("role") == "staff" {
		kind = "staff"
	}
	cookie, err := c.Cookie("mezo_" + kind)
	if err != nil {
		return echo.NewHTTPError(401, "Entre para continuar")
	}
	var revision int64
	err = a.control.QueryRow(c.Request().Context(), "SELECT t.revision FROM auth_sessions s JOIN tenants t ON t.id=s.tenant_id WHERE s.token_hash=$1 AND s.kind=$2 AND s.expires_at>now() AND t.slug=$3 AND t.status='active'", tokenHash(cookie.Value), kind, c.Param("slug")).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(401, "Acesso não autorizado para este restaurante")
	}
	if err != nil {
		return err
	}
	return c.JSON(200, map[string]int64{"revision": revision})
}

type subscription struct {
	tenant, session string
	ch              chan struct{}
}
type Hub struct {
	mu      sync.Mutex
	clients map[*subscription]bool
}

func newHub() *Hub { return &Hub{clients: make(map[*subscription]bool)} }
func (h *Hub) subscribe(tenant, session string) (*subscription, func()) {
	s := &subscription{tenant: tenant, session: session, ch: make(chan struct{}, 1)}
	h.mu.Lock()
	h.clients[s] = true
	h.mu.Unlock()
	return s, func() { h.mu.Lock(); delete(h.clients, s); h.mu.Unlock() }
}
func (h *Hub) publish(tenant, session string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.clients {
		if s.tenant == tenant && (session == "" || s.session == "" || s.session == session) {
			select {
			case s.ch <- struct{}{}:
			default:
			}
		}
	}
}
func (a *App) events(c echo.Context) error {
	kind := "customer"
	if c.QueryParam("role") == "staff" {
		kind = "staff"
	}
	x, err := a.access(c, kind, false)
	if err != nil {
		return err
	}
	x.Release()
	session := ""
	if kind == "customer" {
		session = x.Session.Subject
	}
	sub, unsubscribe := a.hub.subscribe(x.Tenant.ID, session)
	defer unsubscribe()
	f, ok := c.Response().Writer.(http.Flusher)
	if !ok {
		return echo.NewHTTPError(500, "Streaming indisponível")
	}
	c.Response().Header().Set("Content-Type", "text/event-stream")
	c.Response().Header().Set("X-Accel-Buffering", "no")
	c.Response().WriteHeader(200)
	if _, err = fmt.Fprint(c.Response(), "event: update\ndata: {}\n\n"); err != nil {
		return nil
	}
	f.Flush()
	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-c.Request().Context().Done():
			return nil
		case <-sub.ch:
			if _, err = fmt.Fprint(c.Response(), "event: update\ndata: {}\n\n"); err != nil {
				return nil
			}
			f.Flush()
		case <-tick.C:
			// Recheck authorization without retaining a tenant connection for each stream.
			check, e := a.access(c, kind, false)
			if e != nil {
				_, _ = fmt.Fprint(c.Response(), "event: expired\ndata: {}\n\n")
				f.Flush()
				return nil
			}
			check.Release()
			if _, err = fmt.Fprint(c.Response(), ": keepalive\n\n"); err != nil {
				return nil
			}
			f.Flush()
		}
	}
}
