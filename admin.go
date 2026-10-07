package main

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/labstack/echo/v4"
	"github.com/skip2/go-qrcode"
	"golang.org/x/crypto/bcrypt"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func (a *App) home(c echo.Context) error { return a.renderTemplate(c, "home.html", nil) }
func (a *App) renderTemplate(c echo.Context, name string, data any) error {
	return a.renderTemplateStatus(c, 200, name, data)
}
func (a *App) renderTemplateStatus(c echo.Context, status int, name string, data any) error {
	var b bytes.Buffer
	if err := pages.ExecuteTemplate(&b, name, data); err != nil {
		return err
	}
	return c.HTML(status, b.String())
}
func (a *App) adminLoginProblem(c echo.Context, status int, message string) error {
	retry, _ := strconv.Atoi(c.Response().Header().Get("Retry-After"))
	if c.Request().Header.Get("HX-Request") == "true" {
		return c.HTML(status, fmt.Sprintf("<p class='notice error' role='alert' data-retry-seconds='%d'><span data-login-message>%s</span></p>", retry, template.HTMLEscapeString(message)))
	}
	return a.renderTemplateStatus(c, status, "admin.html", map[string]any{"LoggedIn": false, "LoginError": message, "RetrySeconds": retry, "LoginEmail": strings.ToLower(strings.TrimSpace(c.FormValue("email")))})
}
func (a *App) teamEntry(c echo.Context) error {
	slug := strings.ToLower(strings.TrimSpace(c.QueryParam("slug")))
	if len(slug) > 60 || !slugPattern.MatchString(slug) {
		return a.renderTemplateStatus(c, 400, "home.html", map[string]string{"TeamError": "Confira o identificador do restaurante fornecido pelo Mezô."})
	}
	return c.Redirect(http.StatusSeeOther, "/r/"+slug+"/garcom")
}
func adminProblem(c echo.Context, text string) error {
	return c.HTML(200, "<p class='notice error' role='alert'>"+template.HTMLEscapeString(text)+"</p>")
}
func adminOK(c echo.Context, text string) error {
	return c.HTML(200, "<p class='notice' role='status'>"+template.HTMLEscapeString(text)+" <a href='/admin'>Atualizar painel</a></p>")
}
func (a *App) adminPage(c echo.Context) error {
	s, err := a.session(c, "admin", "", false)
	if err != nil {
		return a.renderTemplate(c, "admin.html", map[string]any{"LoggedIn": false})
	}
	tenants, err := a.listTenants(c.Request().Context())
	if err != nil {
		return err
	}
	cards := []map[string]any{}
	for _, t := range tenants {
		card := map[string]any{"Name": t.Name, "Slug": t.Slug, "Status": t.Status, "TeamURL": "/r/" + t.Slug + "/garcom", "KitchenURL": "/r/" + t.Slug + "/cozinha"}
		tables := []map[string]any{}
		if t.Status == "active" {
			p, release, e := a.lease(c.Request().Context(), t)
			if e != nil {
				return e
			}
			rows, e := p.Query(c.Request().Context(), "SELECT number,qr_token FROM dining_tables ORDER BY number")
			if e != nil {
				release()
				return e
			}
			for rows.Next() {
				var n int
				var token string
				if e = rows.Scan(&n, &token); e != nil {
					rows.Close()
					release()
					return e
				}
				tables = append(tables, map[string]any{"Number": n, "URL": "/r/" + t.Slug + "/m/" + token, "QR": "/admin/r/" + t.Slug + "/qr/" + strconv.Itoa(n)})
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				release()
				return e
			}
			staff, menu := []map[string]any{}, []map[string]any{}
			rows, e = p.Query(c.Request().Context(), "SELECT id,name,email,role,active FROM staff ORDER BY name")
			if e != nil {
				release()
				return e
			}
			for rows.Next() {
				var id, name, email, role string
				var active bool
				if e = rows.Scan(&id, &name, &email, &role, &active); e != nil {
					rows.Close()
					release()
					return e
				}
				staff = append(staff, map[string]any{"ID": id, "Name": name, "Email": email, "Role": role, "Active": active})
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				release()
				return e
			}
			rows, e = p.Query(c.Request().Context(), "SELECT id,name,price,active FROM menu_items ORDER BY name")
			if e != nil {
				release()
				return e
			}
			for rows.Next() {
				var id, name string
				var price int
				var active bool
				if e = rows.Scan(&id, &name, &price, &active); e != nil {
					rows.Close()
					release()
					return e
				}
				menu = append(menu, map[string]any{"ID": id, "Name": name, "PriceBRL": priceBRL(price), "Active": active})
			}
			e = rows.Err()
			rows.Close()
			release()
			if e != nil {
				return e
			}
			card["Staff"] = staff
			card["Menu"] = menu
		}
		card["Tables"] = tables
		cards = append(cards, card)
	}
	return a.renderTemplate(c, "admin.html", map[string]any{"LoggedIn": true, "CSRF": s.CSRF, "Restaurants": cards})
}
func (a *App) adminLogin(c echo.Context) error {
	if err := a.origin(c); err != nil {
		return err
	}
	email := strings.ToLower(strings.TrimSpace(c.FormValue("email")))
	password := c.FormValue("password")
	if len(email) == 0 || len(email) > 200 || len(password) == 0 || len(password) > 72 {
		return a.adminLoginProblem(c, 401, "E-mail ou senha inválidos")
	}
	if err := a.limitLogin(c, "admin:", email); err != nil {
		return err
	}
	var hash string
	err := a.control.QueryRow(c.Request().Context(), "SELECT password_hash FROM administrators WHERE email=$1", email).Scan(&hash)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return a.adminLoginProblem(c, 401, "E-mail ou senha inválidos. Use o acesso da plataforma; a equipe entra pelo link do restaurante.")
	}
	if err = a.newSession(c, "admin", "", email); err != nil {
		return err
	}
	if err = a.resetLoginLimit(c, "admin:", email); err != nil {
		return err
	}
	c.Response().Header().Set("HX-Redirect", "/admin")
	if c.Request().Header.Get("HX-Request") == "true" {
		return c.NoContent(200)
	}
	return c.Redirect(http.StatusSeeOther, "/admin")
}
func (a *App) adminLogout(c echo.Context) error {
	if _, err := a.session(c, "admin", "", true); err != nil {
		return err
	}
	cookie, _ := c.Cookie("mezo_admin")
	_, err := a.control.Exec(c.Request().Context(), "DELETE FROM auth_sessions WHERE token_hash=$1", tokenHash(cookie.Value))
	if err != nil {
		return err
	}
	c.SetCookie(&http.Cookie{Name: "mezo_admin", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.cfg.Secure, SameSite: http.SameSiteLaxMode})
	c.Response().Header().Set("HX-Redirect", "/admin")
	if c.Request().Header.Get("HX-Request") == "true" {
		return c.NoContent(200)
	}
	return c.Redirect(303, "/admin")
}
func (a *App) createRestaurant(c echo.Context) error {
	if _, err := a.session(c, "admin", "", true); err != nil {
		return err
	}
	name, slug := strings.TrimSpace(c.FormValue("name")), c.FormValue("slug")
	email, staffName, password := c.FormValue("email"), c.FormValue("staffName"), c.FormValue("password")
	tables, err := strconv.Atoi(c.FormValue("tables"))
	if err != nil || tables < 1 || tables > 100 || name == "" || len(name) > 100 || len(slug) > 60 || !slugPattern.MatchString(slug) || !strings.Contains(email, "@") || len(email) > 200 || staffName == "" || len(staffName) > 80 || len(password) < 12 || len(password) > 72 {
		return adminProblem(c, "Confira os dados. Use 1–100 mesas e senha de 12–72 caracteres.")
	}
	_, err = a.provision(c.Request().Context(), slug, name, email, staffName, password, tables)
	if err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" {
			return adminProblem(c, "Este identificador já está cadastrado")
		}
		return err
	}
	return adminOK(c, "Restaurante cadastrado com banco próprio. O cardápio começa vazio; cadastre os itens abaixo. As mesas começam fechadas: a equipe abre cada uma ao receber os clientes.")
}
func (a *App) createStaff(c echo.Context) error {
	if _, err := a.session(c, "admin", "", true); err != nil {
		return err
	}
	t, err := a.tenant(c.Request().Context(), c.Param("slug"))
	if err != nil {
		return echo.ErrNotFound
	}
	p, release, err := a.lease(c.Request().Context(), t)
	if err != nil {
		return err
	}
	defer release()
	name, email, password := strings.TrimSpace(c.FormValue("name")), strings.ToLower(strings.TrimSpace(c.FormValue("email"))), c.FormValue("password")
	role := c.FormValue("role")
	if role == "" {
		role = "waiter"
	}
	if role != "waiter" && role != "kitchen" && role != "manager" {
		return adminProblem(c, "Escolha uma função válida")
	}
	if name == "" || len(name) > 80 || !strings.Contains(email, "@") || len(email) > 200 || len(password) < 12 || len(password) > 72 {
		return adminProblem(c, "Informe nome, e-mail e senha de 12–72 caracteres")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = p.Exec(c.Request().Context(), "INSERT INTO staff(id,name,email,password_hash,role) VALUES($1,$2,$3,$4,$5)", randomID(), name, email, string(hash), role)
	if err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" {
			return adminProblem(c, "Este e-mail já está cadastrado no restaurante")
		}
		return err
	}
	return adminOK(c, "Atendente cadastrado")
}
func (a *App) setStaffActive(c echo.Context) error {
	if _, err := a.session(c, "admin", "", true); err != nil {
		return err
	}
	t, err := a.tenant(c.Request().Context(), c.Param("slug"))
	if err != nil {
		return echo.ErrNotFound
	}
	p, release, err := a.lease(c.Request().Context(), t)
	if err != nil {
		return err
	}
	defer release()
	active := c.FormValue("active")
	if active != "true" && active != "false" {
		return adminProblem(c, "Estado inválido")
	}
	ctx := c.Request().Context()
	tx, err := p.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id string
	if err = tx.QueryRow(ctx, "SELECT id FROM staff WHERE id=$1 FOR UPDATE", c.Param("id")).Scan(&id); err != nil {
		return echo.ErrNotFound
	}
	if active == "false" {
		var busy bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM calls WHERE assigned_to=$1 AND status='progress') OR EXISTS(SELECT 1 FROM orders WHERE assigned_to=$1 AND status='preparing')", id).Scan(&busy); err != nil {
			return err
		}
		if busy {
			return adminProblem(c, "Conclua ou devolva os atendimentos e pedidos desta pessoa antes de desativar o acesso")
		}
	}
	if _, err = tx.Exec(ctx, "UPDATE staff SET active=$2 WHERE id=$1", id, active == "true"); err != nil {
		return err
	}
	if active == "false" {
		if _, err = a.control.Exec(ctx, "DELETE FROM auth_sessions WHERE tenant_id=$1 AND kind='staff' AND subject=$2", t.ID, id); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return adminOK(c, "Acesso da equipe atualizado")
}
func (a *App) editMenuItem(c echo.Context) error {
	if _, err := a.session(c, "admin", "", true); err != nil {
		return err
	}
	t, err := a.tenant(c.Request().Context(), c.Param("slug"))
	if err != nil {
		return echo.ErrNotFound
	}
	p, release, err := a.lease(c.Request().Context(), t)
	if err != nil {
		return err
	}
	defer release()
	name := strings.TrimSpace(c.FormValue("name"))
	price, err := parsePriceBRL(c.FormValue("priceBRL"))
	active := c.FormValue("active") == "true"
	if err != nil || name == "" || len(name) > 100 || price < 0 || price > 10000000 {
		return adminProblem(c, "Confira o nome e o preço em reais, por exemplo R$ 38,00")
	}
	tag, err := p.Exec(c.Request().Context(), "UPDATE menu_items SET name=$2,price=$3,active=$4 WHERE id=$1", c.Param("id"), name, price, active)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return echo.ErrNotFound
	}
	a.notify(c.Request().Context(), t.ID, "")
	return adminOK(c, "Cardápio atualizado. Pedidos anteriores preservam seus preços.")
}
func (a *App) createMenuItem(c echo.Context) error {
	if _, err := a.session(c, "admin", "", true); err != nil {
		return err
	}
	t, err := a.tenant(c.Request().Context(), c.Param("slug"))
	if err != nil {
		return echo.ErrNotFound
	}
	p, release, err := a.lease(c.Request().Context(), t)
	if err != nil {
		return err
	}
	defer release()
	name, category, desc := strings.TrimSpace(c.FormValue("name")), c.FormValue("category"), c.FormValue("description")
	price, err := parsePriceBRL(c.FormValue("priceBRL"))
	if err != nil || price < 0 || price > 10000000 || name == "" || len(name) > 100 || len(desc) > 500 || !(category == "Entradas" || category == "Principais" || category == "Bebidas" || category == "Sobremesas") {
		return adminProblem(c, "Confira o item e o preço em reais, por exemplo R$ 38,00.")
	}
	_, err = p.Exec(c.Request().Context(), "INSERT INTO menu_items(id,name,description,price,category) VALUES($1,$2,$3,$4,$5)", randomID(), name, desc, price, category)
	if err != nil {
		return err
	}
	a.notify(c.Request().Context(), t.ID, "")
	return adminOK(c, "Item adicionado ao cardápio")
}
func (a *App) tableQR(c echo.Context) error {
	if _, err := a.session(c, "admin", "", false); err != nil {
		return err
	}
	t, err := a.tenant(c.Request().Context(), c.Param("slug"))
	if err != nil {
		return echo.ErrNotFound
	}
	p, release, err := a.lease(c.Request().Context(), t)
	if err != nil {
		return err
	}
	defer release()
	n, err := strconv.Atoi(c.Param("number"))
	if err != nil {
		return echo.ErrNotFound
	}
	var token string
	if err = p.QueryRow(c.Request().Context(), "SELECT qr_token FROM dining_tables WHERE number=$1", n).Scan(&token); err != nil {
		return echo.ErrNotFound
	}
	b, err := qrcode.Encode(a.cfg.Origin+"/r/"+t.Slug+"/m/"+token, qrcode.Medium, 384)
	if err != nil {
		return err
	}
	return c.Blob(200, "image/png", b)
}
