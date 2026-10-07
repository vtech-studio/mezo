package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/joho/godotenv"
)

func TestManagerRunsTheHouse(t *testing.T) {
	if os.Getenv("MEZO_INTEGRATION") != "1" {
		t.Skip("PostgreSQL real: MEZO_INTEGRATION=1")
	}
	_ = godotenv.Load()
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	a, err := newApp(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.close)
	password, suffix := randomID(), randomID()[:10]
	tenant, err := a.provision(ctx, "test-house-"+suffix, "Casa da gestão", "manager@example.test", "Responsável", password, 2)
	if err != nil {
		t.Fatal(err)
	}
	other, err := a.provision(ctx, "test-house-other-"+suffix, "Outra casa", "manager@example.test", "Responsável", password, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		clean, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		for _, v := range []Tenant{tenant, other} {
			if !strings.HasPrefix(v.Slug, "test-house-") || v.DBName != "mezo_t_"+v.ID || v.DBRole != "mezo_u_"+v.ID {
				t.Error("limpeza recusada")
				continue
			}
			a.mu.Lock()
			if p := a.pools[v.ID]; p != nil && p.pool != nil {
				p.pool.Close()
				delete(a.pools, v.ID)
			}
			a.mu.Unlock()
			for _, query := range []string{"DELETE FROM auth_sessions WHERE tenant_id=$1", "DELETE FROM tenants WHERE id=$1"} {
				if _, e := a.control.Exec(clean, query, v.ID); e != nil {
					t.Error(e)
				}
			}
			if _, e := a.admin.Exec(clean, "DROP DATABASE "+pgx.Identifier{v.DBName}.Sanitize()+" WITH (FORCE)"); e != nil {
				t.Error(e)
			}
			if _, e := a.admin.Exec(clean, "DROP ROLE "+pgx.Identifier{v.DBRole}.Sanitize()); e != nil {
				t.Error(e)
			}
		}
	})
	router := a.router()
	base := "/api/r/" + tenant.Slug
	send := func(method, path string, body []byte, contentType string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		r.Header.Set("Origin", cfg.Origin)
		r.Header.Set("Content-Type", contentType)
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("HX-Request", "true")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	form := func(path string, values url.Values, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		return send("POST", path, []byte(values.Encode()), "application/x-www-form-urlencoded", cookie, csrf)
	}
	expect := func(w *httptest.ResponseRecorder, code int, text string) {
		t.Helper()
		if w.Code != code || !strings.Contains(w.Body.String(), text) {
			t.Fatalf("HTTP %d (esperado %d com %q): %s", w.Code, code, text, w.Body.String())
		}
	}
	type actor struct {
		cookie *http.Cookie
		csrf   string
	}
	login := func(email, secret string) actor {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"email": email, "password": secret})
		w := send("POST", base+"/login", body, "application/json", nil, "")
		expect(w, 200, "ok")
		cookie := w.Result().Cookies()[0]
		w = send("GET", base+"/operations?view=waiter", nil, "", cookie, "")
		if w.Code != 200 {
			w = send("GET", base+"/operations?view=kitchen", nil, "", cookie, "")
		}
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return actor{cookie, out["csrf"].(string)}
	}
	manager := login("manager@example.test", password)
	staffID := func(email string) string {
		t.Helper()
		p, release, err := a.lease(ctx, tenant)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		var id string
		if err = p.QueryRow(ctx, "SELECT id FROM staff WHERE email=$1", email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}

	// The manager's page offers the same administration as the platform, on the restaurant routes.
	if w := send("GET", "/r/"+tenant.Slug+"/gestao", nil, "", nil, ""); w.Code != 303 || w.Header().Get("Location") != "/r/"+tenant.Slug+"/garcom" {
		t.Fatal("a página de gestão sem sessão não levou ao login da equipe", w.Code)
	}
	page := send("GET", "/r/"+tenant.Slug+"/gestao", nil, "", manager.cookie, "")
	expect(page, 200, `hx-post="`+base+`/staff"`)
	for _, text := range []string{"Adicionar item", base + "/qr/1", "Redefinir senha de Responsável", " open>"} {
		if !strings.Contains(page.Body.String(), text) {
			t.Fatalf("página de gestão sem %q", text)
		}
	}
	waiterPassword := randomID()
	expect(form(base+"/staff", url.Values{"csrf": {manager.csrf}, "name": {"Garçom"}, "email": {"waiter@example.test"}, "role": {"waiter"}, "password": {waiterPassword}}, manager.cookie, ""), 200, "cadastrado")
	expect(form(base+"/staff", url.Values{"name": {"Sem CSRF"}, "email": {"x@example.test"}, "role": {"waiter"}, "password": {waiterPassword}}, manager.cookie, ""), 403, "")
	waiter := login("waiter@example.test", waiterPassword)
	expect(send("GET", "/r/"+tenant.Slug+"/gestao", nil, "", waiter.cookie, ""), 403, "")
	expect(form(base+"/menu", url.Values{"csrf": {waiter.csrf}, "name": {"Prato"}, "category": {"Principais"}, "priceBRL": {"R$ 10,00"}}, waiter.cookie, ""), 403, "")
	expect(send("GET", base+"/qr/1", nil, "", waiter.cookie, ""), 403, "")
	expect(form("/api/r/"+other.Slug+"/menu", url.Values{"csrf": {manager.csrf}, "name": {"Prato"}, "category": {"Principais"}, "priceBRL": {"R$ 10,00"}}, manager.cookie, ""), 401, "")

	// Menu: create, reprice and mark as sold out.
	expect(form(base+"/menu", url.Values{"csrf": {manager.csrf}, "name": {"Moqueca"}, "category": {"Principais"}, "priceBRL": {"R$ 89,90"}, "description": {"Para dois"}}, manager.cookie, ""), 200, "Item adicionado")
	p, release, err := a.lease(ctx, tenant)
	if err != nil {
		t.Fatal(err)
	}
	var itemID string
	var price int
	err = p.QueryRow(ctx, "SELECT id,price FROM menu_items WHERE name='Moqueca'").Scan(&itemID, &price)
	release()
	if err != nil || price != 8990 {
		t.Fatal("item não cadastrado com o preço em centavos", price, err)
	}
	expect(form(base+"/menu/"+itemID, url.Values{"csrf": {manager.csrf}, "name": {"Moqueca baiana"}, "priceBRL": {"R$ 92,50"}}, manager.cookie, ""), 200, "Cardápio atualizado")
	var menu map[string]any
	_ = json.Unmarshal(send("GET", base+"/operations?view=waiter", nil, "", waiter.cookie, "").Body.Bytes(), &menu)
	if strings.Contains(string(must(json.Marshal(menu["menu"]))), "Moqueca") {
		t.Fatal("item marcado como esgotado continuou no cardápio da equipe")
	}
	qr := send("GET", base+"/qr/1", nil, "", manager.cookie, "")
	if qr.Code != 200 || qr.Header().Get("Content-Type") != "image/png" {
		t.Fatal("o responsável não baixou o QR da mesa", qr.Code)
	}

	// Team: a new initial password ends the previous sessions; the house keeps a manager.
	newPassword := randomID()
	waiterID, managerID := staffID("waiter@example.test"), staffID("manager@example.test")
	expect(form(base+"/staff/"+waiterID+"/password", url.Values{"csrf": {manager.csrf}, "password": {"curta"}}, manager.cookie, ""), 200, "12 a 72")
	expect(form(base+"/staff/"+waiterID+"/password", url.Values{"csrf": {manager.csrf}, "password": {newPassword}}, manager.cookie, ""), 200, "Senha redefinida")
	expect(send("GET", base+"/operations?view=waiter", nil, "", waiter.cookie, ""), 401, "")
	body, _ := json.Marshal(map[string]string{"email": "waiter@example.test", "password": waiterPassword})
	expect(send("POST", base+"/login", body, "application/json", nil, ""), 401, "")
	login("waiter@example.test", newPassword)
	expect(form(base+"/staff/"+managerID+"/password", url.Values{"csrf": {manager.csrf}, "password": {newPassword}}, manager.cookie, ""), 200, "Trocar senha")
	expect(form(base+"/staff/"+managerID+"/active", url.Values{"csrf": {manager.csrf}, "active": {"false"}}, manager.cookie, ""), 200, "seu próprio acesso")
	adminLogin := httptest.NewRequest("POST", "/admin/login", strings.NewReader(url.Values{"email": {cfg.AdminEmail}, "password": {cfg.AdminPassword}}.Encode()))
	adminLogin.Header.Set("Origin", cfg.Origin)
	adminLogin.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	adminLogin.Header.Set("HX-Request", "true")
	aw := httptest.NewRecorder()
	router.ServeHTTP(aw, adminLogin)
	adminCookie := aw.Result().Cookies()[0]
	var adminCSRF string
	if err = a.control.QueryRow(ctx, "SELECT csrf FROM auth_sessions WHERE token_hash=$1", tokenHash(adminCookie.Value)).Scan(&adminCSRF); err != nil {
		t.Fatal(err)
	}
	expect(form("/admin/r/"+tenant.Slug+"/staff/"+managerID+"/active", url.Values{"csrf": {adminCSRF}, "active": {"false"}}, adminCookie, ""), 200, "pelo menos um responsável")
	expect(form(base+"/staff", url.Values{"csrf": {manager.csrf}, "name": {"Sócia"}, "email": {"partner@example.test"}, "role": {"manager"}, "password": {newPassword}}, manager.cookie, ""), 200, "cadastrado")
	expect(form(base+"/staff/"+staffID("partner@example.test")+"/active", url.Values{"csrf": {manager.csrf}, "active": {"false"}}, manager.cookie, ""), 200, "atualizado")
	// The admin panel renders the same sections on the platform routes.
	panel := send("GET", "/admin", nil, "", adminCookie, "")
	expect(panel, 200, `hx-post="/admin/r/`+tenant.Slug+`/staff/`+waiterID+`/password"`)
	if strings.Contains(panel.Body.String(), "/api/r/"+tenant.Slug+"/staff") {
		t.Fatal("o painel da plataforma usou as rotas do restaurante")
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
