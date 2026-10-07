package main

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
)

// house is a restaurant opened for administration: by a platform administrator on
// routes under /admin/, or by one of the restaurant's own managers elsewhere.
type house struct {
	Tenant  Tenant
	Pool    *pgxpool.Pool
	Release func()
	CSRF    string
	Manager string // staff id of the acting manager; empty for platform administrators
}

func (a *App) houseAccess(c echo.Context, mutation bool) (house, error) {
	if strings.HasPrefix(c.Path(), "/admin/") {
		s, err := a.session(c, "admin", "", mutation)
		if err != nil {
			return house{}, err
		}
		t, err := a.tenant(c.Request().Context(), c.Param("slug"))
		if err != nil {
			return house{}, echo.ErrNotFound
		}
		p, release, err := a.lease(c.Request().Context(), t)
		if err != nil {
			return house{}, err
		}
		return house{Tenant: t, Pool: p, Release: release, CSRF: s.CSRF}, nil
	}
	x, err := a.staffAccess(c, mutation, "manager")
	if err != nil {
		return house{}, err
	}
	return house{Tenant: x.Tenant, Pool: x.Pool, Release: x.Release, CSRF: x.Session.CSRF, Manager: x.Session.Subject}, nil
}

func queryMaps(ctx context.Context, p *pgxpool.Pool, sql string, args ...any) ([]map[string]any, error) {
	rows, err := p.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToMap)
}

// houseData loads the tables, team, menu and logo shown on the administration screens.
func houseData(ctx context.Context, p *pgxpool.Pool, slug string) (map[string]any, error) {
	tables, err := queryMaps(ctx, p, `SELECT number AS "Number",'/r/'||$1::text||'/m/'||qr_token AS "URL" FROM dining_tables ORDER BY number`, slug)
	if err != nil {
		return nil, err
	}
	staff, err := queryMaps(ctx, p, `SELECT id AS "ID",name AS "Name",email AS "Email",role AS "Role",active AS "Active" FROM staff ORDER BY name`)
	if err != nil {
		return nil, err
	}
	menu, err := queryMaps(ctx, p, `SELECT id AS "ID",name AS "Name",price,active AS "Active",image_url AS "ImageURL" FROM menu_items ORDER BY name`)
	if err != nil {
		return nil, err
	}
	for _, item := range menu {
		item["PriceBRL"] = priceBRL(int(item["price"].(int32)))
	}
	var logo string
	if err = p.QueryRow(ctx, "SELECT logo_url FROM restaurant_brand WHERE singleton").Scan(&logo); err != nil {
		return nil, err
	}
	return map[string]any{"Tables": tables, "Staff": staff, "Menu": menu, "LogoURL": logo}, nil
}

// loginRedirect sends visitors of the administration pages without a session to the team login.
func loginRedirect(c echo.Context, err error) error {
	var he *echo.HTTPError
	if errors.As(err, &he) && he.Code == 401 {
		return c.Redirect(303, "/r/"+c.Param("slug")+"/garcom")
	}
	return err
}

// housePage lets the restaurant's manager run the menu, the team and the table plates.
func (a *App) housePage(c echo.Context) error {
	h, err := a.houseAccess(c, false)
	if err != nil {
		return loginRedirect(c, err)
	}
	defer h.Release()
	data, err := houseData(c.Request().Context(), h.Pool, h.Tenant.Slug)
	if err != nil {
		return err
	}
	for k, v := range map[string]any{"Name": h.Tenant.Name, "Slug": h.Tenant.Slug, "Base": "/api/r/" + h.Tenant.Slug, "CSRF": h.CSRF, "ImagesEnabled": a.cfg.CloudName != "", "Expanded": true} {
		data[k] = v
	}
	return a.renderTemplate(c, "house.html", data)
}

// revokeStaff ends every open session of one person on the team.
func (a *App) revokeStaff(ctx context.Context, tenant, staff string) error {
	_, err := a.control.Exec(ctx, "DELETE FROM auth_sessions WHERE tenant_id=$1 AND kind='staff' AND subject=$2", tenant, staff)
	return err
}

// resetStaffPassword gives someone who forgot their password a new initial one.
func (a *App) resetStaffPassword(c echo.Context) error {
	h, err := a.houseAccess(c, true)
	if err != nil {
		return err
	}
	defer h.Release()
	password := c.FormValue("password")
	if len(password) < 12 || len(password) > 72 {
		return adminProblem(c, "Use uma senha de 12 a 72 caracteres")
	}
	if h.Manager == c.Param("id") {
		return adminProblem(c, "Para a sua própria senha, use “Trocar senha” no seu acesso")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	tag, err := h.Pool.Exec(ctx, "UPDATE staff SET password_hash=$2 WHERE id=$1", c.Param("id"), string(hash))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return echo.ErrNotFound
	}
	// Sessions created with the previous password end after the update.
	if err = a.revokeStaff(ctx, h.Tenant.ID, c.Param("id")); err != nil {
		return err
	}
	return adminOK(c, "Senha redefinida. Os acessos anteriores dessa pessoa foram encerrados.")
}
