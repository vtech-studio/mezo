package main

import (
	"encoding/json"
	"errors"
	"strconv"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/labstack/echo/v4"
)

type Call struct {
	ID         string `json:"id"`
	SessionID  string `json:"sessionId"`
	Table      int    `json:"table"`
	Reason     string `json:"reason"`
	Note       string `json:"note"`
	Status     string `json:"status"`
	Created    int64  `json:"created"`
	Accepted   int64  `json:"accepted"`
	Completed  int64  `json:"completed"`
	Assigned   string `json:"assigned"`
	AssignedID string `json:"assignedId"`
}

func (a *App) bootstrap(c echo.Context) error {
	kind := "customer"
	if c.QueryParam("role") == "staff" {
		kind = "staff"
	}
	x, err := a.access(c, kind, false)
	if err != nil {
		return err
	}
	defer x.Release()
	ctx := c.Request().Context()
	out := map[string]any{"restaurant": map[string]string{"name": x.Tenant.Name, "slug": x.Tenant.Slug}, "csrf": x.Session.CSRF, "role": kind, "paymentsEnabled": false}
	out["realtime"] = "events"
	if a.cfg.Serverless {
		out["realtime"] = "poll"
	}
	if kind == "staff" {
		var name, role string
		if err = x.Pool.QueryRow(ctx, "SELECT name,role FROM staff WHERE id=$1", x.Session.Subject).Scan(&name, &role); err != nil {
			return err
		}
		out["staff"] = map[string]string{"id": x.Session.Subject, "name": name, "role": role}
	}
	where := ""
	args := []any{}
	if kind == "customer" {
		where = " WHERE s.id=$1"
		args = append(args, x.Session.Subject)
	}
	// Keep every active call visible even after the restaurant accumulates a long history.
	rows, err := x.Pool.Query(ctx, "WITH visible_calls AS (SELECT * FROM calls WHERE status IN ('pending','progress') UNION ALL (SELECT * FROM calls WHERE status NOT IN ('pending','progress') ORDER BY created_at DESC LIMIT 100)) SELECT c.id,c.session_id,s.table_number,c.reason,c.note,c.status,(extract(epoch FROM c.created_at)*1000)::bigint,COALESCE((extract(epoch FROM c.accepted_at)*1000)::bigint,0),COALESCE((extract(epoch FROM c.completed_at)*1000)::bigint,0),COALESCE(u.name,''),COALESCE(c.assigned_to,'') FROM visible_calls c JOIN table_sessions s ON s.id=c.session_id LEFT JOIN staff u ON u.id=c.assigned_to"+where+" ORDER BY c.created_at DESC", args...)
	if err != nil {
		return err
	}
	calls := []Call{}
	for rows.Next() {
		var v Call
		if err = rows.Scan(&v.ID, &v.SessionID, &v.Table, &v.Reason, &v.Note, &v.Status, &v.Created, &v.Accepted, &v.Completed, &v.Assigned, &v.AssignedID); err != nil {
			rows.Close()
			return err
		}
		calls = append(calls, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	out["calls"] = calls
	rows, err = x.Pool.Query(ctx, "SELECT id,name,description,price,category,art,tags FROM menu_items WHERE active ORDER BY category,name")
	if err != nil {
		return err
	}
	menu := []map[string]any{}
	for rows.Next() {
		var id, name, desc, cat, art string
		var price int
		var tags json.RawMessage
		if err = rows.Scan(&id, &name, &desc, &price, &cat, &art, &tags); err != nil {
			rows.Close()
			return err
		}
		menu = append(menu, map[string]any{"id": id, "name": name, "description": desc, "price": price, "category": cat, "art": art, "tags": tags})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	out["menu"] = menu
	if kind == "customer" {
		var n, seats int
		var service bool
		if err = x.Pool.QueryRow(ctx, "SELECT d.number,d.seats,s.service FROM table_sessions s JOIN dining_tables d ON d.number=s.table_number WHERE s.id=$1", x.Session.Subject).Scan(&n, &seats, &service); err != nil {
			return err
		}
		out["table"] = map[string]any{"number": n, "seats": seats}
		out["service"] = service
		rows, err = x.Pool.Query(ctx, "SELECT id,menu_id,name,price,qty FROM consumption WHERE session_id=$1 ORDER BY created_at", x.Session.Subject)
		if err != nil {
			return err
		}
		items := []map[string]any{}
		subtotal := 0
		for rows.Next() {
			var id, menuID, name string
			var price, qty int
			if err = rows.Scan(&id, &menuID, &name, &price, &qty); err != nil {
				rows.Close()
				return err
			}
			subtotal += price * qty
			items = append(items, map[string]any{"lineId": id, "id": menuID, "name": name, "price": price, "qty": qty})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		fee := 0
		if service {
			fee = (subtotal + 5) / 10
		}
		out["items"] = items
		out["totals"] = map[string]int{"subtotal": subtotal, "service": fee, "total": subtotal + fee}
	} else {
		rows, err = x.Pool.Query(ctx, "SELECT d.number,d.seats,COALESCE(s.id,''),COALESCE(sum(v.price::bigint*v.qty),0) FROM dining_tables d LEFT JOIN table_sessions s ON s.table_number=d.number AND s.closed_at IS NULL LEFT JOIN consumption v ON v.session_id=s.id GROUP BY d.number,d.seats,s.id ORDER BY d.number")
		if err != nil {
			return err
		}
		tables := []map[string]any{}
		for rows.Next() {
			var n, seats int
			var session string
			var subtotal int64
			if err = rows.Scan(&n, &seats, &session, &subtotal); err != nil {
				rows.Close()
				return err
			}
			tables = append(tables, map[string]any{"number": n, "seats": seats, "sessionId": session, "subtotal": subtotal})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		out["tables"] = tables
	}
	return c.JSON(200, out)
}
func (a *App) createCall(c echo.Context) error {
	x, err := a.access(c, "customer", true)
	if err != nil {
		return err
	}
	defer x.Release()
	var in struct {
		Reason string `json:"reason"`
		Note   string `json:"note"`
	}
	if err = decode(c, &in); err != nil {
		return err
	}
	if !(in.Reason == "help" || in.Reason == "water" || in.Reason == "order" || in.Reason == "bill") || utf8.RuneCountInString(in.Note) > 140 {
		return echo.NewHTTPError(400, "Motivo ou observação inválidos")
	}
	tx, err := x.Pool.Begin(c.Request().Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(c.Request().Context())
	var open bool
	if err = tx.QueryRow(c.Request().Context(), "SELECT closed_at IS NULL FROM table_sessions WHERE id=$1 FOR UPDATE", x.Session.Subject).Scan(&open); err != nil {
		return err
	}
	if !open {
		return echo.NewHTTPError(410, "Conta encerrada")
	}
	id := randomID()
	var created string
	err = tx.QueryRow(c.Request().Context(), "INSERT INTO calls(id,session_id,reason,note) VALUES($1,$2,$3,$4) ON CONFLICT(session_id,reason) WHERE status IN ('pending','progress') DO NOTHING RETURNING id", id, x.Session.Subject, in.Reason, in.Note).Scan(&created)
	duplicate := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !duplicate {
		return err
	}
	if duplicate {
		if err = tx.QueryRow(c.Request().Context(), "SELECT id FROM calls WHERE session_id=$1 AND reason=$2 AND status IN ('pending','progress')", x.Session.Subject, in.Reason).Scan(&id); err != nil {
			return err
		}
	} else {
		if _, err = tx.Exec(c.Request().Context(), "INSERT INTO call_events(call_id,action) VALUES($1,'created')", id); err != nil {
			return err
		}
	}
	if err = tx.Commit(c.Request().Context()); err != nil {
		return err
	}
	a.hub.publish(x.Tenant.ID, x.Session.Subject)
	return c.JSON(200, map[string]any{"id": id, "duplicate": duplicate})
}
func (a *App) updateCall(c echo.Context) error {
	x, err := a.access(c, "staff", true)
	if err != nil {
		return err
	}
	defer x.Release()
	action := c.Param("action")
	tx, err := x.Pool.Begin(c.Request().Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(c.Request().Context())
	var sessionID string
	if action == "accept" {
		err = tx.QueryRow(c.Request().Context(), "UPDATE calls SET status='progress',assigned_to=$2,accepted_at=now() WHERE id=$1 AND status='pending' RETURNING session_id", c.Param("id"), x.Session.Subject).Scan(&sessionID)
	} else if action == "complete" {
		err = tx.QueryRow(c.Request().Context(), "UPDATE calls SET status='done',completed_at=now() WHERE id=$1 AND status='progress' AND assigned_to=$2 RETURNING session_id", c.Param("id"), x.Session.Subject).Scan(&sessionID)
	} else {
		return echo.ErrNotFound
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(409, "Este chamado já foi atualizado ou pertence a outro atendente")
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(c.Request().Context(), "INSERT INTO call_events(call_id,action,staff_id) VALUES($1,$2,$3)", c.Param("id"), action, x.Session.Subject); err != nil {
		return err
	}
	if err = tx.Commit(c.Request().Context()); err != nil {
		return err
	}
	a.hub.publish(x.Tenant.ID, sessionID)
	return c.JSON(200, map[string]bool{"ok": true})
}
func (a *App) service(c echo.Context) error {
	x, err := a.access(c, "customer", true)
	if err != nil {
		return err
	}
	defer x.Release()
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if err = decode(c, &in); err != nil {
		return err
	}
	tag, err := x.Pool.Exec(c.Request().Context(), "UPDATE table_sessions SET service=$2 WHERE id=$1 AND closed_at IS NULL", x.Session.Subject, in.Enabled)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return echo.NewHTTPError(410, "Conta encerrada")
	}
	a.hub.publish(x.Tenant.ID, x.Session.Subject)
	return c.JSON(200, map[string]bool{"ok": true})
}
func (a *App) addConsumption(c echo.Context) error {
	x, err := a.access(c, "staff", true)
	if err != nil {
		return err
	}
	defer x.Release()
	n, err := strconv.Atoi(c.Param("number"))
	if err != nil {
		return echo.ErrNotFound
	}
	var in struct {
		MenuID string `json:"menuId"`
		Qty    int    `json:"qty"`
	}
	if err = decode(c, &in); err != nil {
		return err
	}
	if in.Qty < 1 || in.Qty > 100 {
		return echo.NewHTTPError(400, "Quantidade inválida")
	}
	tx, err := x.Pool.Begin(c.Request().Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(c.Request().Context())
	var session string
	if err = tx.QueryRow(c.Request().Context(), "SELECT id FROM table_sessions WHERE table_number=$1 AND closed_at IS NULL FOR UPDATE", n).Scan(&session); errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(409, "Abra a mesa antes de lançar o consumo")
	}
	if err != nil {
		return err
	}
	tag, err := tx.Exec(c.Request().Context(), "INSERT INTO consumption(id,session_id,menu_id,name,price,qty,created_by) SELECT $1,$2,id,name,price,$4,$5 FROM menu_items WHERE id=$3 AND active", randomID(), session, in.MenuID, in.Qty, x.Session.Subject)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return echo.NewHTTPError(400, "Item indisponível")
	}
	if err = tx.Commit(c.Request().Context()); err != nil {
		return err
	}
	a.hub.publish(x.Tenant.ID, session)
	return c.JSON(201, map[string]bool{"ok": true})
}
func (a *App) closeTable(c echo.Context) error {
	x, err := a.access(c, "staff", true)
	if err != nil {
		return err
	}
	defer x.Release()
	n, err := strconv.Atoi(c.Param("number"))
	if err != nil {
		return echo.ErrNotFound
	}
	var in struct {
		Confirmed bool `json:"confirmed"`
	}
	if err = decode(c, &in); err != nil {
		return err
	}
	if !in.Confirmed {
		return echo.NewHTTPError(400, "Confirme o encerramento presencial")
	}
	tx, err := x.Pool.Begin(c.Request().Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(c.Request().Context())
	var session string
	err = tx.QueryRow(c.Request().Context(), "UPDATE table_sessions SET closed_at=now() WHERE table_number=$1 AND closed_at IS NULL RETURNING id", n).Scan(&session)
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(409, "A mesa já está fechada")
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(c.Request().Context(), "UPDATE calls SET status='cancelled' WHERE session_id=$1 AND status IN ('pending','progress')", session); err != nil {
		return err
	}
	if err = tx.Commit(c.Request().Context()); err != nil {
		return err
	}
	a.hub.publish(x.Tenant.ID, session)
	return c.JSON(200, map[string]bool{"ok": true})
}
func (a *App) openTable(c echo.Context) error {
	x, err := a.access(c, "staff", true)
	if err != nil {
		return err
	}
	defer x.Release()
	n, err := strconv.Atoi(c.Param("number"))
	if err != nil {
		return echo.ErrNotFound
	}
	_, err = x.Pool.Exec(c.Request().Context(), "INSERT INTO table_sessions(id,table_number) VALUES($1,$2)", randomID(), n)
	if err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && (pe.Code == "23505" || pe.Code == "23503") {
			return echo.NewHTTPError(409, "Mesa inexistente ou já aberta")
		}
		return err
	}
	a.hub.publish(x.Tenant.ID, "")
	return c.JSON(201, map[string]bool{"ok": true})
}
