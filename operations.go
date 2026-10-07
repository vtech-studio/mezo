package main

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
)

type OrderItem struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Qty   int    `json:"qty"`
	Price int    `json:"price,omitempty"`
	Note  string `json:"note"`
}
type Order struct {
	ID         string      `json:"id"`
	SessionID  string      `json:"sessionId"`
	Table      int         `json:"table"`
	Status     string      `json:"status"`
	Source     string      `json:"source"`
	Note       string      `json:"note"`
	Assigned   string      `json:"assigned"`
	AssignedID string      `json:"assignedId"`
	Created    int64       `json:"created"`
	Updated    int64       `json:"updated"`
	Items      []OrderItem `json:"items"`
}

func lockOperator(ctx context.Context, tx pgx.Tx, id, expectedRole string) error {
	var role string
	err := tx.QueryRow(ctx, "SELECT role FROM staff WHERE id=$1 AND active FOR SHARE", id).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(401, "Acesso da equipe desativado")
	}
	if err != nil {
		return err
	}
	if role != expectedRole {
		return echo.NewHTTPError(403, "Sua função foi alterada. Entre novamente.")
	}
	return nil
}

func readOrders(ctx context.Context, p *pgxpool.Pool, session string, kitchen bool) ([]Order, error) {
	// Every active ticket remains visible, even with a long order history.
	visible := `SELECT * FROM orders WHERE status IN ('queued','preparing','ready') UNION ALL (SELECT * FROM orders WHERE status IN ('delivered','cancelled') ORDER BY updated_at DESC LIMIT 100)`
	args := []any{}
	if session != "" {
		// A table keeps its whole history, however busy the rest of the restaurant is.
		visible = `SELECT * FROM orders WHERE session_id=$1`
		args = append(args, session)
	}
	rows, err := p.Query(ctx, `WITH visible AS (`+visible+`)
 SELECT o.id,o.session_id,s.table_number,o.status,CASE WHEN o.customer_session_id IS NULL THEN 'waiter' ELSE 'customer' END,o.note,COALESCE(u.name,''),COALESCE(o.assigned_to,''),
 (extract(epoch FROM o.created_at)*1000)::bigint,(extract(epoch FROM o.updated_at)*1000)::bigint,
 COALESCE((SELECT jsonb_agg(jsonb_build_object('id',i.id,'name',i.name,'qty',i.qty,'price',i.price,'note',i.note) ORDER BY i.id) FROM order_items i WHERE i.order_id=o.id),'[]')
 FROM visible o JOIN table_sessions s ON s.id=o.session_id LEFT JOIN staff u ON u.id=o.assigned_to
 ORDER BY o.created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Order{}
	for rows.Next() {
		var v Order
		var raw []byte
		if err = rows.Scan(&v.ID, &v.SessionID, &v.Table, &v.Status, &v.Source, &v.Note, &v.Assigned, &v.AssignedID, &v.Created, &v.Updated, &raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &v.Items); err != nil {
			return nil, err
		}
		if kitchen {
			for i := range v.Items {
				v.Items[i].Price = 0
			}
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (a *App) workstationPage(c echo.Context) error {
	t, err := a.tenant(c.Request().Context(), c.Param("slug"))
	if err != nil {
		return echo.ErrNotFound
	}
	view := "waiter"
	title := "Garçom"
	if strings.HasSuffix(c.Path(), "/cozinha") {
		view = "kitchen"
		title = "Cozinha"
	}
	return a.renderTemplate(c, "workstation.html", map[string]string{"Name": t.Name, "Slug": t.Slug, "View": view, "Title": title})
}
func (a *App) operations(c echo.Context) error {
	view := c.QueryParam("view")
	if view == "waiter" {
		return a.bootstrap(c)
	}
	if view != "kitchen" {
		return echo.NewHTTPError(400, "Área inválida")
	}
	x, err := a.staffAccess(c, false, "manager", "kitchen")
	if err != nil {
		return err
	}
	defer x.Release()
	var name string
	if err = x.Pool.QueryRow(c.Request().Context(), "SELECT name FROM staff WHERE id=$1", x.Session.Subject).Scan(&name); err != nil {
		return err
	}
	orders, err := readOrders(c.Request().Context(), x.Pool, "", true)
	if err != nil {
		return err
	}
	return c.JSON(200, map[string]any{"restaurant": map[string]string{"name": x.Tenant.Name}, "csrf": x.Session.CSRF, "staff": map[string]string{"id": x.Session.Subject, "name": name, "role": x.Role}, "orders": orders, "revision": x.Tenant.Revision})
}

func (a *App) createOrder(c echo.Context) error {
	return a.submitOrder(c, false)
}
func (a *App) createCustomerOrder(c echo.Context) error {
	return a.submitOrder(c, true)
}
func (a *App) submitOrder(c echo.Context, customer bool) error {
	var x Access
	var err error
	if customer {
		x, err = a.access(c, "customer", true)
	} else {
		x, err = a.staffAccess(c, true, "manager", "waiter")
	}
	if err != nil {
		return err
	}
	defer x.Release()
	var in struct {
		Table int    `json:"table"`
		Key   string `json:"key"`
		Note  string `json:"note"`
		Items []struct {
			MenuID string `json:"menuId"`
			Qty    int    `json:"qty"`
			Note   string `json:"note"`
		} `json:"items"`
		ExpectedSubtotal *int64 `json:"expectedSubtotal,omitempty"`
	}
	if err = decode(c, &in); err != nil {
		return err
	}
	if (!customer && (in.Table < 1 || in.Table > 500)) || (customer && (in.Table != 0 || in.ExpectedSubtotal == nil || *in.ExpectedSubtotal < 0)) || len(in.Key) < 16 || len(in.Key) > 80 || utf8.RuneCountInString(in.Note) > 300 || len(in.Items) < 1 || len(in.Items) > 40 {
		return echo.NewHTTPError(400, "Confira a mesa, os itens e a observação do pedido")
	}
	for _, v := range in.Items {
		if v.MenuID == "" || v.Qty < 1 || v.Qty > 100 || utf8.RuneCountInString(v.Note) > 200 {
			return echo.NewHTTPError(400, "Quantidade ou observação inválida")
		}
	}
	raw, _ := json.Marshal(in)
	digest := tokenHash(string(raw))
	ctx := c.Request().Context()
	tx, err := x.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if !customer {
		if err = lockOperator(ctx, tx, x.Session.Subject, x.Role); err != nil {
			return err
		}
	}
	// Serializes retries of the same basket, including concurrent requests for different tables.
	actorKey := x.Session.Subject + ":" + in.Key
	actorColumn := "created_by"
	if customer {
		actorKey = "customer:" + actorKey
		actorColumn = "customer_session_id"
	}
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", actorKey); err != nil {
		return err
	}
	var id, previous string
	err = tx.QueryRow(ctx, "SELECT id,request_hash FROM orders WHERE "+actorColumn+"=$1 AND request_key=$2", x.Session.Subject, in.Key).Scan(&id, &previous)
	if err == nil {
		if previous != digest {
			return echo.NewHTTPError(409, "Este envio já foi usado para outro pedido")
		}
		return c.JSON(200, map[string]any{"id": id, "duplicate": true})
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var session string
	if customer {
		err = tx.QueryRow(ctx, "SELECT id FROM table_sessions WHERE id=$1 AND closed_at IS NULL FOR UPDATE", x.Session.Subject).Scan(&session)
	} else {
		err = tx.QueryRow(ctx, "SELECT id FROM table_sessions WHERE table_number=$1 AND closed_at IS NULL FOR UPDATE", in.Table).Scan(&session)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(409, "Abra a mesa antes de enviar o pedido")
	}
	if err != nil {
		return err
	}
	var staffID, customerSession any
	if customer {
		customerSession = session
		var recent int
		if err = tx.QueryRow(ctx, "SELECT count(*) FROM orders WHERE customer_session_id=$1 AND created_at>now()-interval '1 minute'", session).Scan(&recent); err != nil {
			return err
		}
		if recent >= 12 {
			return echo.NewHTTPError(429, "Muitos pedidos nesta mesa. Aguarde um minuto antes de confirmar outro.")
		}
	} else {
		staffID = x.Session.Subject
	}
	id = randomID()
	if _, err = tx.Exec(ctx, "INSERT INTO orders(id,session_id,created_by,request_key,request_hash,note,customer_session_id) VALUES($1,$2,$3,$4,$5,$6,$7)", id, session, staffID, in.Key, digest, in.Note, customerSession); err != nil {
		return err
	}
	var subtotal int64
	for _, v := range in.Items {
		var name string
		var price int
		err = tx.QueryRow(ctx, "SELECT name,price FROM menu_items WHERE id=$1 AND active FOR SHARE", v.MenuID).Scan(&name, &price)
		if errors.Is(err, pgx.ErrNoRows) {
			return echo.NewHTTPError(409, "Um item ficou indisponível. Revise o pedido antes de enviar")
		}
		if err != nil {
			return err
		}
		subtotal += int64(price) * int64(v.Qty)
		if _, err = tx.Exec(ctx, "INSERT INTO order_items(id,order_id,menu_id,name,price,qty,note) VALUES($1,$2,$3,$4,$5,$6,$7)", randomID(), id, v.MenuID, name, price, v.Qty, v.Note); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO consumption(id,session_id,menu_id,name,price,qty,created_by,order_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", randomID(), session, v.MenuID, name, price, v.Qty, staffID, id); err != nil {
			return err
		}
	}
	if customer && subtotal != *in.ExpectedSubtotal {
		return echo.NewHTTPError(409, "O preço mudou. Confira o pedido atualizado antes de confirmar.")
	}
	if _, err = tx.Exec(ctx, "INSERT INTO order_events(order_id,action,staff_id,customer_session_id) VALUES($1,'created',$2,$3)", id, staffID, customerSession); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	a.notify(c.Request().Context(), x.Tenant.ID, session)
	return c.JSON(201, map[string]any{"id": id, "duplicate": false})
}

func (a *App) updateOrder(c echo.Context) error {
	action := c.Param("action")
	roles := []string{"manager", "kitchen"}
	if action == "deliver" {
		roles = []string{"manager", "waiter"}
	} else if action == "cancel" {
		roles = []string{"manager"}
	} else if action != "start" && action != "ready" {
		return echo.ErrNotFound
	}
	x, err := a.staffAccess(c, true, roles...)
	if err != nil {
		return err
	}
	defer x.Release()
	var in struct {
		Note string `json:"note"`
	}
	if err = decode(c, &in); err != nil {
		return err
	}
	if utf8.RuneCountInString(in.Note) > 300 || (action == "cancel" && strings.TrimSpace(in.Note) == "") {
		return echo.NewHTTPError(400, "Informe o motivo do cancelamento (até 300 caracteres)")
	}
	ctx := c.Request().Context()
	tx, err := x.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockOperator(ctx, tx, x.Session.Subject, x.Role); err != nil {
		return err
	}
	var session string
	err = tx.QueryRow(ctx, "SELECT session_id FROM orders WHERE id=$1", c.Param("id")).Scan(&session)
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.ErrNotFound
	}
	if err != nil {
		return err
	}
	// All mutations lock the table session first, so closing/cancelling/ordering cannot race.
	var open bool
	if err = tx.QueryRow(ctx, "SELECT closed_at IS NULL FROM table_sessions WHERE id=$1 FOR UPDATE", session).Scan(&open); err != nil {
		return err
	}
	if !open {
		return echo.NewHTTPError(409, "A conta desta mesa já foi encerrada")
	}
	var status, assigned string
	if err = tx.QueryRow(ctx, "SELECT status,COALESCE(assigned_to,'') FROM orders WHERE id=$1 FOR UPDATE", c.Param("id")).Scan(&status, &assigned); err != nil {
		return err
	}
	next := ""
	switch action {
	case "start":
		if status == "queued" {
			next = "preparing"
		}
	case "ready":
		if status == "preparing" && (assigned == x.Session.Subject || x.Role == "manager") {
			next = "ready"
		}
	case "deliver":
		if status == "ready" {
			next = "delivered"
		}
	case "cancel":
		if status == "queued" || status == "preparing" || status == "ready" {
			next = "cancelled"
		}
	}
	if next == "" {
		return echo.NewHTTPError(409, "Este pedido já foi atualizado ou está com outra pessoa")
	}
	if action == "start" {
		_, err = tx.Exec(ctx, "UPDATE orders SET status=$2,assigned_to=$3,updated_at=now() WHERE id=$1", c.Param("id"), next, x.Session.Subject)
	} else {
		_, err = tx.Exec(ctx, "UPDATE orders SET status=$2,updated_at=now() WHERE id=$1", c.Param("id"), next)
	}
	if err != nil {
		return err
	}
	if action == "cancel" {
		if _, err = tx.Exec(ctx, "UPDATE consumption SET cancelled=true WHERE order_id=$1", c.Param("id")); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, "INSERT INTO order_events(order_id,action,staff_id,note) VALUES($1,$2,$3,$4)", c.Param("id"), action, x.Session.Subject, in.Note); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	a.notify(c.Request().Context(), x.Tenant.ID, session)
	return c.JSON(200, map[string]bool{"ok": true})
}

func (a *App) tableBill(c echo.Context) error {
	x, err := a.staffAccess(c, false, "manager", "waiter")
	if err != nil {
		return err
	}
	defer x.Release()
	n, err := strconv.Atoi(c.Param("number"))
	if err != nil {
		return echo.ErrNotFound
	}
	var session string
	var service bool
	err = x.Pool.QueryRow(c.Request().Context(), "SELECT id,service FROM table_sessions WHERE table_number=$1 AND closed_at IS NULL", n).Scan(&session, &service)
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(409, "Mesa fechada")
	}
	if err != nil {
		return err
	}
	rows, err := x.Pool.Query(c.Request().Context(), "SELECT name,price,qty FROM consumption WHERE session_id=$1 AND NOT cancelled ORDER BY created_at", session)
	if err != nil {
		return err
	}
	items := []OrderItem{}
	var subtotal int64
	for rows.Next() {
		var v OrderItem
		if err = rows.Scan(&v.Name, &v.Price, &v.Qty); err != nil {
			rows.Close()
			return err
		}
		subtotal += int64(v.Price) * int64(v.Qty)
		items = append(items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var fee int64
	if service {
		fee = (subtotal + 5) / 10
	}
	return c.JSON(200, map[string]any{"sessionId": session, "items": items, "subtotal": subtotal, "service": fee, "total": subtotal + fee})
}

func (a *App) changeStaffPassword(c echo.Context) error {
	x, err := a.access(c, "staff", true)
	if err != nil {
		return err
	}
	defer x.Release()
	var in struct {
		Current  string `json:"current"`
		Password string `json:"password"`
	}
	if err = decode(c, &in); err != nil {
		return err
	}
	if len(in.Password) < 12 || len(in.Password) > 72 {
		return echo.NewHTTPError(400, "Use uma senha de 12 a 72 caracteres")
	}
	var hash string
	ctx := c.Request().Context()
	tx, err := x.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = tx.QueryRow(ctx, "SELECT password_hash FROM staff WHERE id=$1 AND active FOR UPDATE", x.Session.Subject).Scan(&hash); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return echo.NewHTTPError(401, "Acesso da equipe desativado")
		}
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Current)) != nil {
		return echo.NewHTTPError(403, "Senha atual inválida")
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE staff SET password_hash=$2 WHERE id=$1", x.Session.Subject, string(hashed)); err != nil {
		return err
	}
	// Revoke while the staff row remains locked, so a simultaneous login
	// cannot establish a session using the previous password.
	if _, err = a.control.Exec(ctx, "DELETE FROM auth_sessions WHERE tenant_id=$1 AND kind='staff' AND subject=$2", x.Tenant.ID, x.Session.Subject); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return c.JSON(200, map[string]bool{"ok": true})
}
