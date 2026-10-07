package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
)

// The QR printed on a table never changes. A short code, renewed every time the
// table opens, proves the guest is at the restaurant now; whoever saved the link
// earlier cannot order onto someone else's bill.
const tableCodeFailures = 10 // wrong codes per table session and hour

func newTableCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%04d", n.Int64())
}

func (a *App) tableCodeKey(tenant, session string) string {
	return tokenHash(a.cfg.Secret + "table-code:" + tenant + ":" + session)
}

// tableCodeWait returns how many seconds a table refuses codes after repeated failures.
func (a *App) tableCodeWait(ctx context.Context, tenant, session string) (int, error) {
	var wait int
	err := a.control.QueryRow(ctx, "SELECT GREATEST(1,ceil(extract(epoch FROM (reset_at-now()))))::int FROM login_attempts WHERE key=$1 AND attempts>=$2 AND reset_at>now()", a.tableCodeKey(tenant, session), tableCodeFailures).Scan(&wait)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return wait, err
}

// Only wrong codes count, so a large group entering the right one is never blocked.
func (a *App) tableCodeFailed(ctx context.Context, tenant, session string) error {
	_, err := a.control.Exec(ctx, `INSERT INTO login_attempts(key,attempts,reset_at) VALUES($1,1,now()+interval '1 hour') ON CONFLICT(key) DO UPDATE SET attempts=CASE WHEN login_attempts.reset_at<=now() THEN 1 ELSE login_attempts.attempts+1 END,reset_at=CASE WHEN login_attempts.reset_at<=now() THEN now()+interval '1 hour' ELSE login_attempts.reset_at END`, a.tableCodeKey(tenant, session))
	return err
}

func (a *App) tableCodePage(c echo.Context, status int, restaurant, message string) error {
	return a.renderTemplateStatus(c, status, "table-code.html", map[string]string{"Name": restaurant, "Action": c.Request().URL.Path, "Error": message})
}

// submitTableCode admits a guest to the open table once the code matches.
func (a *App) submitTableCode(c echo.Context) error {
	if err := a.origin(c); err != nil {
		return err
	}
	ctx := c.Request().Context()
	t, err := a.tenant(ctx, c.Param("slug"))
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.ErrNotFound
	}
	if err != nil {
		return err
	}
	p, release, err := a.lease(ctx, t)
	if err != nil {
		return err
	}
	defer release()
	var id string
	var code *string
	err = p.QueryRow(ctx, "SELECT s.id,s.code FROM dining_tables d JOIN table_sessions s ON s.table_number=d.number AND s.closed_at IS NULL WHERE d.qr_token=$1", c.Param("token")).Scan(&id, &code)
	if errors.Is(err, pgx.ErrNoRows) {
		return tableUnavailable(c)
	}
	if err != nil {
		return err
	}
	if code != nil {
		wait, err := a.tableCodeWait(ctx, t.ID, id)
		if err != nil {
			return err
		}
		if wait > 0 {
			c.Response().Header().Set("Retry-After", strconv.Itoa(wait))
			return a.tableCodePage(c, 429, t.Name, fmt.Sprintf("Muitas tentativas nesta mesa. Tente novamente em %d min ou peça ajuda à equipe.", (wait+59)/60))
		}
		if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(c.FormValue("code"))), []byte(*code)) != 1 {
			if err = a.tableCodeFailed(ctx, t.ID, id); err != nil {
				return err
			}
			return a.tableCodePage(c, 401, t.Name, "Código incorreto. Confira com a equipe.")
		}
	}
	if err = a.newSession(c, "customer", t.ID, id); err != nil {
		return err
	}
	return c.Redirect(303, c.Request().URL.Path)
}

// renewTableCode replaces the code of an open table, for example after it was shared
// beyond the table, and clears the failures recorded against the previous one.
func (a *App) renewTableCode(c echo.Context) error {
	x, err := a.staffAccess(c, true, "manager", "waiter")
	if err != nil {
		return err
	}
	defer x.Release()
	n, err := strconv.Atoi(c.Param("number"))
	if err != nil {
		return echo.ErrNotFound
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
	code := newTableCode()
	var session string
	err = tx.QueryRow(ctx, "UPDATE table_sessions SET code=$2 WHERE table_number=$1 AND closed_at IS NULL RETURNING id", n, code).Scan(&session)
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(409, "Abra a mesa antes de gerar o código")
	}
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if _, err = a.control.Exec(ctx, "DELETE FROM login_attempts WHERE key=$1", a.tableCodeKey(x.Tenant.ID, session)); err != nil {
		return err
	}
	a.notify(ctx, x.Tenant.ID, "")
	return c.JSON(200, map[string]string{"code": code})
}

func tableUnavailable(c echo.Context) error {
	return c.HTML(410, "<html lang='pt-BR'><meta name='viewport' content='width=device-width'><link rel='stylesheet' href='"+assetURL("admin.css")+"'><main class='portal'><h1>Mesa indisponível</h1><p>Confira a placa ou peça à equipe para abrir a mesa.</p></main></html>")
}
