package main

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

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
