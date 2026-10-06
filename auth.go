package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
)

type Session struct{ Kind, TenantID, Subject, CSRF string }

func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
func (a *App) origin(c echo.Context) error {
	if c.Request().Header.Get("Origin") != a.cfg.Origin {
		return echo.NewHTTPError(403, "Origem não autorizada")
	}
	return nil
}
func decode(c echo.Context, v any) error {
	d := json.NewDecoder(c.Request().Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return echo.NewHTTPError(400, "Dados inválidos")
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		return echo.NewHTTPError(400, "Dados inválidos")
	}
	return nil
}
func (a *App) newSession(c echo.Context, kind, tenant, subject string) error {
	token := randomID() + randomID()
	csrf := randomID()
	var tenantID any = tenant
	if tenant == "" {
		tenantID = nil
	}
	_, err := a.control.Exec(c.Request().Context(), "INSERT INTO auth_sessions(token_hash,kind,tenant_id,subject,csrf,expires_at) VALUES($1,$2,$3,$4,$5,$6)", tokenHash(token), kind, tenantID, subject, csrf, time.Now().Add(8*time.Hour))
	if err != nil {
		return err
	}
	if old, e := c.Cookie("mezo_" + kind); e == nil {
		_, _ = a.control.Exec(c.Request().Context(), "DELETE FROM auth_sessions WHERE token_hash=$1", tokenHash(old.Value))
	}
	c.SetCookie(&http.Cookie{Name: "mezo_" + kind, Value: token, Path: "/", HttpOnly: true, Secure: a.cfg.Secure, SameSite: http.SameSiteLaxMode, MaxAge: 8 * 60 * 60})
	return nil
}
func (a *App) session(c echo.Context, kind, tenant string, mutation bool) (Session, error) {
	var s Session
	cookie, err := c.Cookie("mezo_" + kind)
	if err != nil {
		return s, echo.NewHTTPError(401, "Entre para continuar")
	}
	err = a.control.QueryRow(c.Request().Context(), "SELECT kind,COALESCE(tenant_id,''),subject,csrf FROM auth_sessions WHERE token_hash=$1 AND kind=$2 AND expires_at>now()", tokenHash(cookie.Value), kind).Scan(&s.Kind, &s.TenantID, &s.Subject, &s.CSRF)
	if errors.Is(err, pgx.ErrNoRows) || s.TenantID != tenant {
		return s, echo.NewHTTPError(401, "Acesso não autorizado para este restaurante")
	}
	if err != nil {
		return s, err
	}
	if mutation {
		if err = a.origin(c); err != nil {
			return s, err
		}
		csrf := c.Request().Header.Get("X-CSRF-Token")
		if csrf == "" {
			csrf = c.FormValue("csrf")
		}
		if subtle.ConstantTimeCompare([]byte(csrf), []byte(s.CSRF)) != 1 {
			return s, echo.NewHTTPError(403, "Sessão de segurança inválida. Atualize a página.")
		}
	}
	return s, nil
}

type Access struct {
	Tenant  Tenant
	Session Session
	Pool    *pgxpool.Pool
	Release func()
}

func (a *App) access(c echo.Context, kind string, mutation bool) (Access, error) {
	var out Access
	t, err := a.tenant(c.Request().Context(), c.Param("slug"))
	if errors.Is(err, pgx.ErrNoRows) {
		return out, echo.ErrNotFound
	}
	if err != nil {
		return out, err
	}
	s, err := a.session(c, kind, t.ID, mutation)
	if err != nil {
		return out, err
	}
	p, release, err := a.lease(c.Request().Context(), t)
	if err != nil {
		return out, err
	}
	if kind == "customer" {
		var active bool
		if err = p.QueryRow(c.Request().Context(), "SELECT EXISTS(SELECT 1 FROM table_sessions WHERE id=$1 AND closed_at IS NULL)", s.Subject).Scan(&active); err != nil {
			release()
			return out, err
		}
		if !active {
			release()
			return out, echo.NewHTTPError(410, "Esta conta foi encerrada. Escaneie a placa novamente quando a equipe abrir a mesa.")
		}
	}
	return Access{t, s, p, release}, nil
}
func (a *App) enterTable(c echo.Context) error {
	t, err := a.tenant(c.Request().Context(), c.Param("slug"))
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.ErrNotFound
	}
	if err != nil {
		return err
	}
	p, release, err := a.lease(c.Request().Context(), t)
	if err != nil {
		return err
	}
	defer release()
	var id string
	err = p.QueryRow(c.Request().Context(), "SELECT s.id FROM dining_tables d JOIN table_sessions s ON s.table_number=d.number AND s.closed_at IS NULL WHERE d.qr_token=$1", c.Param("token")).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return c.HTML(410, "<html lang='pt-BR'><meta name='viewport' content='width=device-width'><link rel='stylesheet' href='/admin.css'><main class='portal'><h1>Mesa indisponível</h1><p>Confira a placa ou peça à equipe para abrir a mesa.</p></main></html>")
	}
	if err != nil {
		return err
	}
	if cookie, e := c.Cookie("mezo_customer"); e == nil && cookie.Value != "" {
		s, e := a.session(c, "customer", t.ID, false)
		if e == nil && s.Subject == id {
			return a.appPage(c)
		}
	}
	if err = a.newSession(c, "customer", t.ID, id); err != nil {
		return err
	}
	return a.appPage(c)
}
func (a *App) staffPage(c echo.Context) error {
	if _, err := a.tenant(c.Request().Context(), c.Param("slug")); err != nil {
		return echo.ErrNotFound
	}
	return a.appPage(c)
}
func (a *App) appPage(c echo.Context) error {
	b, err := assets.ReadFile("dist/index.html")
	if err != nil {
		return err
	}
	html := strings.Replace(string(b), "<head>", "<head><meta name=\"mezo-runtime\" content=\"server\">", 1)
	t, err := a.tenant(c.Request().Context(), c.Param("slug"))
	if err != nil {
		return err
	}
	html = strings.ReplaceAll(html, "Casa do Sol", template.HTMLEscapeString(t.Name))
	return c.HTML(200, html)
}
func (a *App) staffLogin(c echo.Context) error {
	if err := a.origin(c); err != nil {
		return err
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decode(c, &in); err != nil {
		return err
	}
	t, err := a.tenant(c.Request().Context(), c.Param("slug"))
	if err != nil {
		return echo.NewHTTPError(401, "E-mail ou senha inválidos")
	}
	p, release, err := a.lease(c.Request().Context(), t)
	if err != nil {
		return err
	}
	defer release()
	var id, hash string
	err = p.QueryRow(c.Request().Context(), "SELECT id,password_hash FROM staff WHERE email=$1", strings.ToLower(strings.TrimSpace(in.Email))).Scan(&id, &hash)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Password)) != nil {
		return echo.NewHTTPError(401, "E-mail ou senha inválidos")
	}
	if err = a.newSession(c, "staff", t.ID, id); err != nil {
		return err
	}
	return c.JSON(200, map[string]bool{"ok": true})
}
func (a *App) logout(c echo.Context) error {
	x, err := a.access(c, "staff", true)
	if err != nil {
		return err
	}
	defer x.Release()
	cookie, _ := c.Cookie("mezo_staff")
	_, err = a.control.Exec(c.Request().Context(), "DELETE FROM auth_sessions WHERE token_hash=$1", tokenHash(cookie.Value))
	c.SetCookie(&http.Cookie{Name: "mezo_staff", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.cfg.Secure, SameSite: http.SameSiteLaxMode})
	if err != nil {
		return err
	}
	return c.JSON(200, map[string]bool{"ok": true})
}
