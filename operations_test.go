package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
	"github.com/pressly/goose/v3"
)

func TestKitchenWaiterOrdersAndSettlement(t *testing.T) {
	if os.Getenv("MEZO_INTEGRATION") != "1" {
		t.Skip("PostgreSQL real: MEZO_INTEGRATION=1")
	}
	_ = godotenv.Load()
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	a, err := newApp(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.close)
	tenants := []Tenant{}
	t.Cleanup(func() {
		cleanCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		for _, v := range tenants {
			if !strings.HasPrefix(v.Slug, "test-ops-") || v.DBName != "mezo_t_"+v.ID || v.DBRole != "mezo_u_"+v.ID {
				t.Error("limpeza recusada")
				continue
			}
			a.mu.Lock()
			if p := a.pools[v.ID]; p != nil {
				p.pool.Close()
				delete(a.pools, v.ID)
			}
			a.mu.Unlock()
			if _, err = a.control.Exec(cleanCtx, "DELETE FROM auth_sessions WHERE tenant_id=$1", v.ID); err != nil {
				t.Error(err)
			}
			if _, err = a.control.Exec(cleanCtx, "DELETE FROM tenants WHERE id=$1", v.ID); err != nil {
				t.Error(err)
			}
			if _, err = a.admin.Exec(cleanCtx, "DROP DATABASE "+pgx.Identifier{v.DBName}.Sanitize()+" WITH (FORCE)"); err != nil {
				t.Error(err)
			}
			if _, err = a.admin.Exec(cleanCtx, "DROP ROLE "+pgx.Identifier{v.DBRole}.Sanitize()); err != nil {
				t.Error(err)
			}
		}
	})
	password := randomID()
	suffix := randomID()[:10]
	tenant, err := a.provision(ctx, "test-ops-"+suffix, "Operações <teste>", "manager@example.test", "Responsável", password, 2)
	if err != nil {
		t.Fatal(err)
	}
	tenants = append(tenants, tenant)
	other, err := a.provision(ctx, "test-ops-other-"+suffix, "Outro", "other@example.test", "Outra pessoa", password, 1)
	if err != nil {
		t.Fatal(err)
	}
	tenants = append(tenants, other)
	// Exercise an existing restaurant on migration 1, before kitchen support.
	pw, e := a.crypt(tenant.DBSecret, false)
	if e != nil {
		t.Fatal(e)
	}
	legacyCfg, e := a.connection(tenant.DBName, tenant.DBRole, pw)
	if e != nil {
		t.Fatal(e)
	}
	db := stdlib.OpenDB(*legacyCfg.ConnConfig)
	sub, e := fs.Sub(assets, "migrations/tenant")
	if e != nil {
		t.Fatal(e)
	}
	provider, e := goose.NewProvider(goose.DialectPostgres, db, sub, goose.WithDisableGlobalRegistry(true))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = provider.DownTo(ctx, 1); e != nil {
		t.Fatal(e)
	}
	_ = db.Close()
	a.mu.Lock()
	if cached := a.pools[tenant.ID]; cached != nil {
		cached.pool.Close()
		delete(a.pools, tenant.ID)
	}
	a.mu.Unlock()
	instances := make([]*App, 2)
	for i := range instances {
		instances[i], e = newApp(ctx, cfg)
		if e != nil {
			t.Fatal(e)
		}
	}
	var migrationWG sync.WaitGroup
	upgradeErrors := make([]error, 2)
	for i := range instances {
		migrationWG.Add(1)
		go func(i int) {
			defer migrationWG.Done()
			pool, release, e := instances[i].lease(ctx, tenant)
			if e != nil {
				upgradeErrors[i] = e
				return
			}
			defer release()
			var active bool
			upgradeErrors[i] = pool.QueryRow(ctx, "SELECT active FROM staff LIMIT 1").Scan(&active)
		}(i)
	}
	migrationWG.Wait()
	for i := range instances {
		instances[i].close()
		if upgradeErrors[i] != nil {
			t.Fatal("migração concorrente do restaurante existente", upgradeErrors[i])
		}
	}
	p, release, err := a.lease(ctx, tenant)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	var hash, token, waiterID string
	if err = p.QueryRow(ctx, "SELECT password_hash FROM staff LIMIT 1").Scan(&hash); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"waiter", "kitchen1", "kitchen2"} {
		actual := role
		if strings.HasPrefix(role, "kitchen") {
			actual = "kitchen"
		}
		id := randomID()
		if role == "waiter" {
			waiterID = id
		}
		if _, err = p.Exec(ctx, "INSERT INTO staff(id,name,email,password_hash,role) VALUES($1,$2,$3,$4,$5)", id, role, role+"@example.test", hash, actual); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = p.Exec(ctx, "INSERT INTO menu_items(id,name,price,category) VALUES('dish','Prato teste',1200,'Principais')"); err != nil {
		t.Fatal(err)
	}
	if err = p.QueryRow(ctx, "SELECT qr_token FROM dining_tables WHERE number=1").Scan(&token); err != nil {
		t.Fatal(err)
	}
	router := a.router()
	published := os.Getenv("MEZO_PUBLISHED_TEST") == "1"
	if published && (!cfg.Serverless || !cfg.Secure) {
		t.Fatal("teste publicado exige configuração HTTPS da Vercel")
	}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	serve := func(w *httptest.ResponseRecorder, r *http.Request) {
		if !published {
			router.ServeHTTP(w, r)
			return
		}
		out := r.Clone(ctx)
		out.URL, _ = url.Parse(cfg.Origin + r.URL.String())
		out.Host = out.URL.Host
		out.RequestURI = ""
		response, err := client.Do(out)
		if err != nil {
			w.WriteHeader(599)
			_, _ = w.Write([]byte("Falha na requisição ao deployment"))
			return
		}
		defer response.Body.Close()
		for name, values := range response.Header {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	}
	base := "/api/r/" + tenant.Slug
	request := func(method, path string, payload any, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(payload)
		r := httptest.NewRequest(method, path, bytes.NewReader(b))
		r.Header.Set("Origin", cfg.Origin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		serve(w, r)
		return w
	}
	expect := func(w *httptest.ResponseRecorder, code int) {
		t.Helper()
		if w.Code != code {
			t.Fatalf("HTTP %d esperado %d: %s", w.Code, code, w.Body.String())
		}
	}
	read := func(w *httptest.ResponseRecorder) map[string]any {
		t.Helper()
		out := map[string]any{}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	type actor struct {
		cookie *http.Cookie
		csrf   string
	}
	login := func(email, view string) actor {
		t.Helper()
		w := request("POST", base+"/login", map[string]string{"email": email, "password": password}, nil, "")
		expect(w, 200)
		cookie := w.Result().Cookies()[0]
		w = request("GET", base+"/operations?view="+view, nil, cookie, "")
		expect(w, 200)
		return actor{cookie, read(w)["csrf"].(string)}
	}
	manager := login("manager@example.test", "waiter")
	waiter := login("waiter@example.test", "waiter")
	chefs := []actor{login("kitchen1@example.test", "kitchen"), login("kitchen2@example.test", "kitchen")}
	expect(request("GET", base+"/operations?view=waiter", nil, nil, ""), 401)
	expect(request("GET", base+"/operations?view=kitchen", nil, waiter.cookie, ""), 403)
	expect(request("GET", base+"/bootstrap?role=staff", nil, chefs[0].cookie, ""), 403)
	for _, path := range []string{"/tables/1/open", "/tables/1/close", "/tables/1/consumption", "/calls/fake/accept", "/orders"} {
		expect(request("POST", base+path, map[string]any{}, chefs[0].cookie, chefs[0].csrf), 403)
	}
	expect(request("GET", base+"/tables/1/bill", nil, chefs[0].cookie, ""), 403)
	expect(request("GET", "/api/r/"+other.Slug+"/operations?view=kitchen", nil, chefs[0].cookie, ""), 401)
	w := request("GET", "/r/"+tenant.Slug+"/m/"+token, nil, nil, "")
	expect(w, 200)
	customer := w.Result().Cookies()[0]
	w = request("GET", base+"/bootstrap", nil, customer, "")
	expect(w, 200)
	customerCSRF := read(w)["csrf"].(string)
	expect(request("GET", base+"/operations?view=waiter", nil, customer, ""), 401)
	payload := map[string]any{"table": 1, "key": randomID(), "note": "Entradas primeiro <script>", "items": []map[string]any{{"menuId": "dish", "qty": 2, "note": "Sem cebola"}}}
	expect(request("POST", base+"/orders", payload, waiter.cookie, ""), 403)
	bad := map[string]any{"table": 1, "key": randomID(), "items": []map[string]any{{"menuId": "dish", "qty": 1, "price": 1}}}
	expect(request("POST", base+"/orders", bad, waiter.cookie, waiter.csrf), 400)
	results := make([]*httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = request("POST", base+"/orders", payload, waiter.cookie, waiter.csrf)
		}(i)
	}
	wg.Wait()
	if !((results[0].Code == 201 && results[1].Code == 200) || (results[0].Code == 200 && results[1].Code == 201)) {
		t.Fatalf("envio concorrente: %d/%d", results[0].Code, results[1].Code)
	}
	id := read(results[0])["id"].(string)
	if read(results[1])["id"] != id {
		t.Fatal("envio repetido duplicou o pedido")
	}
	w = request("GET", base+"/tables/1/bill", nil, waiter.cookie, "")
	expect(w, 200)
	if read(w)["total"] != float64(2640) {
		t.Fatal("conta incorreta")
	}
	var count int
	if err = p.QueryRow(ctx, "SELECT count(*) FROM consumption WHERE order_id=$1", id).Scan(&count); err != nil || count != 1 {
		t.Fatal("consumo duplicado", err, count)
	}
	payload["note"] = "Outro envio"
	expect(request("POST", base+"/orders", payload, waiter.cookie, waiter.csrf), 409)
	payload["note"] = "Entradas primeiro <script>"
	if _, err = p.Exec(ctx, "UPDATE menu_items SET price=2000,active=false WHERE id='dish'"); err != nil {
		t.Fatal(err)
	}
	expect(request("POST", base+"/orders", payload, waiter.cookie, waiter.csrf), 200)
	payload["key"] = randomID()
	expect(request("POST", base+"/orders", payload, waiter.cookie, waiter.csrf), 409)
	if err = p.QueryRow(ctx, "SELECT count(*) FROM orders").Scan(&count); err != nil || count != 1 {
		t.Fatal("pedido indisponível não sofreu rollback", err, count)
	}
	if _, err = p.Exec(ctx, "UPDATE menu_items SET active=true WHERE id='dish'"); err != nil {
		t.Fatal(err)
	}
	expect(request("POST", base+"/orders", payload, waiter.cookie, waiter.csrf), 201)
	second := request("GET", base+"/operations?view=waiter", nil, manager.cookie, "")
	expect(second, 200)
	orders := read(second)["orders"].([]any)
	var cancelID string
	for _, v := range orders {
		if v.(map[string]any)["id"] != id {
			cancelID = v.(map[string]any)["id"].(string)
		}
	}
	expect(request("POST", base+"/orders/"+cancelID+"/cancel", map[string]any{"note": "Cancelamento"}, waiter.cookie, waiter.csrf), 403)
	expect(request("POST", base+"/orders/"+cancelID+"/cancel", map[string]any{}, manager.cookie, manager.csrf), 400)
	expect(request("POST", base+"/orders/"+cancelID+"/cancel", map[string]any{"note": "Cliente desistiu"}, manager.cookie, manager.csrf), 200)
	if read(request("GET", base+"/tables/1/bill", nil, waiter.cookie, ""))["total"] != float64(2640) {
		t.Fatal("cancelamento não retirou consumo ou mudou preço anterior")
	}
	w = request("GET", base+"/operations?view=kitchen", nil, chefs[0].cookie, "")
	expect(w, 200)
	if strings.Contains(w.Body.String(), `"price"`) || strings.Contains(w.Body.String(), `"tables"`) || strings.Contains(w.Body.String(), `"calls"`) {
		t.Fatal("cozinha recebeu dados financeiros ou do salão")
	}
	expect(request("POST", base+"/tables/1/close", map[string]any{"confirmed": true, "expectedTotal": 2640, "method": "pix"}, manager.cookie, manager.csrf), 409)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = request("POST", base+"/orders/"+id+"/start", map[string]any{}, chefs[i].cookie, chefs[i].csrf)
		}(i)
	}
	wg.Wait()
	winner := 0
	if results[1].Code == 200 {
		winner = 1
	}
	expect(results[winner], 200)
	expect(results[1-winner], 409)
	expect(request("POST", base+"/orders/"+id+"/ready", map[string]any{}, waiter.cookie, waiter.csrf), 403)
	expect(request("POST", base+"/orders/"+id+"/ready", map[string]any{}, chefs[1-winner].cookie, chefs[1-winner].csrf), 409)
	expect(request("POST", base+"/orders/"+id+"/ready", map[string]any{}, chefs[winner].cookie, chefs[winner].csrf), 200)
	expect(request("POST", base+"/orders/"+id+"/ready", map[string]any{}, chefs[winner].cookie, chefs[winner].csrf), 409)
	expect(request("POST", base+"/orders/"+id+"/deliver", map[string]any{}, chefs[winner].cookie, chefs[winner].csrf), 403)
	expect(request("POST", base+"/orders/"+id+"/deliver", map[string]any{}, waiter.cookie, waiter.csrf), 200)
	expect(request("POST", base+"/orders/"+id+"/deliver", map[string]any{}, waiter.cookie, waiter.csrf), 409)
	expect(request("POST", base+"/orders/"+id+"/cancel", map[string]any{"note": "Depois da entrega"}, manager.cookie, manager.csrf), 409)
	recovered, err := newApp(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", base+"/operations?view=kitchen", nil)
	r.AddCookie(chefs[0].cookie)
	rw := httptest.NewRecorder()
	recovered.router().ServeHTTP(rw, r)
	expect(rw, 200)
	if !strings.Contains(rw.Body.String(), `"delivered"`) {
		t.Fatal("pedido não persistiu em outra instância")
	}
	recovered.close()
	expect(request("POST", base+"/service", map[string]any{"enabled": false}, customer, customerCSRF), 200)
	expect(request("POST", base+"/tables/1/close", map[string]any{"confirmed": true, "expectedTotal": 2640, "method": "pix"}, manager.cookie, manager.csrf), 409)
	expect(request("POST", base+"/tables/1/close", map[string]any{"confirmed": true, "expectedTotal": 2400}, manager.cookie, manager.csrf), 400)
	expect(request("POST", base+"/tables/1/close", map[string]any{"confirmed": true, "expectedTotal": 2400, "method": "pix"}, manager.cookie, manager.csrf), 200)
	var method string
	var paid int64
	if err = p.QueryRow(ctx, "SELECT method,total FROM settlements").Scan(&method, &paid); err != nil || method != "pix" || paid != 2400 {
		t.Fatal("recebimento não persistiu", err, method, paid)
	}
	expect(request("GET", base+"/receipts", nil, waiter.cookie, ""), 403)
	expect(request("GET", base+"/receipts", nil, chefs[0].cookie, ""), 403)
	w = request("GET", base+"/receipts", nil, manager.cookie, "")
	expect(w, 200)
	if read(w)["total"] != float64(2400) {
		t.Fatal("relatório divergiu do recebimento")
	}
	w = request("GET", base+"/receipts?format=csv", nil, manager.cookie, "")
	expect(w, 200)
	if !strings.Contains(w.Body.String(), "24,00") {
		t.Fatal("CSV incorreto")
	}
	expect(request("GET", base+"/receipts?date=2026-99-99", nil, manager.cookie, ""), 400)
	expect(request("GET", base+"/bootstrap", nil, customer, ""), 410)
	expect(request("POST", base+"/tables/1/open", map[string]any{}, waiter.cookie, waiter.csrf), 201)
	w = request("GET", base+"/tables/1/bill", nil, waiter.cookie, "")
	expect(w, 200)
	if read(w)["total"] != float64(0) {
		t.Fatal("conta anterior vazou para nova mesa")
	}
	// Platform administration verifies the same role checks and templates used in onboarding.
	adminLogin := httptest.NewRequest("POST", "/admin/login", strings.NewReader(url.Values{"email": {cfg.AdminEmail}, "password": {cfg.AdminPassword}}.Encode()))
	adminLogin.Header.Set("Origin", cfg.Origin)
	adminLogin.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	aw := httptest.NewRecorder()
	serve(aw, adminLogin)
	expect(aw, 303)
	adminCookie := aw.Result().Cookies()[0]
	adminPage := request("GET", "/admin", nil, adminCookie, "")
	expect(adminPage, 200)
	if !strings.Contains(adminPage.Body.String(), "Cozinha →") {
		t.Fatal("painel não apresentou acesso da cozinha")
	}
	var adminCSRF string
	if err = a.control.QueryRow(ctx, "SELECT csrf FROM auth_sessions WHERE token_hash=$1", tokenHash(adminCookie.Value)).Scan(&adminCSRF); err != nil {
		t.Fatal(err)
	}
	formRequest := func(path string, values url.Values) *httptest.ResponseRecorder {
		values.Set("csrf", adminCSRF)
		r := httptest.NewRequest("POST", path, strings.NewReader(values.Encode()))
		r.AddCookie(adminCookie)
		r.Header.Set("Origin", cfg.Origin)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		serve(w, r)
		return w
	}
	w = formRequest("/admin/r/"+tenant.Slug+"/staff/"+waiterID+"/active", url.Values{"active": {"false"}})
	expect(w, 200)
	if !strings.Contains(w.Body.String(), "atualizado") {
		t.Fatal(w.Body.String())
	}
	expect(request("GET", base+"/operations?view=waiter", nil, waiter.cookie, ""), 401)
	expect(request("POST", base+"/login", map[string]string{"email": "waiter@example.test", "password": password}, nil, ""), 401)
	w = formRequest("/admin/r/"+tenant.Slug+"/menu/dish", url.Values{"name": {"Novo nome"}, "price": {"3200"}, "active": {"true"}})
	expect(w, 200)
	// Password rotation revokes other sessions; wrong current passwords never change credentials.
	expect(request("POST", base+"/password", map[string]string{"current": "wrong", "password": randomID()}, chefs[0].cookie, chefs[0].csrf), 403)
	newPassword := randomID()
	expect(request("POST", base+"/password", map[string]string{"current": password, "password": newPassword}, chefs[0].cookie, chefs[0].csrf), 200)
	expect(request("GET", base+"/operations?view=kitchen", nil, chefs[0].cookie, ""), 401)
	expect(request("POST", base+"/login", map[string]string{"email": "kitchen1@example.test", "password": password}, nil, ""), 401)
	expect(request("POST", base+"/login", map[string]string{"email": "kitchen1@example.test", "password": newPassword}, nil, ""), 200)
	t.Log("salão/cozinha separados; preços imutáveis; envio idempotente; disputas de preparo; cancelamento auditado; fechamento seguro; revogação e persistência entre instâncias")
	if published {
		t.Log("Fluxo realizado por HTTPS no deployment publicado, com restaurantes temporários removidos ao final")
	}
}
