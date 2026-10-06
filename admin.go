package main

import (
	"bytes"
	"errors"
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
	t, err := template.ParseFS(assets, "templates/"+name)
	if err != nil {
		return err
	}
	var b bytes.Buffer
	if err = t.Execute(&b, data); err != nil {
		return err
	}
	return c.HTML(200, b.String())
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
		card := map[string]any{"Name": t.Name, "Slug": t.Slug, "Status": t.Status, "TeamURL": "/r/" + t.Slug + "/equipe"}
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
			release()
			if e != nil {
				return e
			}
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
	var hash string
	err := a.control.QueryRow(c.Request().Context(), "SELECT password_hash FROM administrators WHERE email=$1", email).Scan(&hash)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(c.FormValue("password"))) != nil {
		return adminProblem(c, "E-mail ou senha inválidos")
	}
	if err = a.newSession(c, "admin", "", email); err != nil {
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
	return adminOK(c, "Restaurante cadastrado com banco próprio. O cardápio começa vazio; cadastre os itens abaixo.")
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
	if name == "" || len(name) > 80 || !strings.Contains(email, "@") || len(email) > 200 || len(password) < 12 || len(password) > 72 {
		return adminProblem(c, "Informe nome, e-mail e senha de 12–72 caracteres")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = p.Exec(c.Request().Context(), "INSERT INTO staff(id,name,email,password_hash,role) VALUES($1,$2,$3,$4,'waiter')", randomID(), name, email, string(hash))
	if err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" {
			return adminProblem(c, "Este e-mail já está cadastrado no restaurante")
		}
		return err
	}
	return adminOK(c, "Atendente cadastrado")
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
	price, err := strconv.Atoi(c.FormValue("price"))
	if err != nil || price < 0 || price > 10000000 || name == "" || len(name) > 100 || len(desc) > 500 || !(category == "Entradas" || category == "Principais" || category == "Bebidas" || category == "Sobremesas") {
		return adminProblem(c, "Confira o item. Preço deve ser um inteiro em centavos, por exemplo 3800 para R$ 38,00.")
	}
	_, err = p.Exec(c.Request().Context(), "INSERT INTO menu_items(id,name,description,price,category) VALUES($1,$2,$3,$4,$5)", randomID(), name, desc, price, category)
	if err != nil {
		return err
	}
	a.hub.publish(t.ID, "")
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
