package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/joho/godotenv"
)

// Opt-in: this creates ONLY new, randomly named test tenants in the configured Mezô cluster.
func TestPostgresIsolationAndRestaurantFlow(t *testing.T) {
	if os.Getenv("MEZO_INTEGRATION") != "1" {
		t.Skip("defina MEZO_INTEGRATION=1 para testar com PostgreSQL real")
	}
	_ = godotenv.Load()
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	a, err := newApp(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	suffix := randomID()[:12]
	password := randomID()
	ta, err := a.provision(ctx, "test-a-"+suffix, "Teste A", "a@example.test", "Pessoa A", password, 2)
	if err != nil {
		t.Fatal(err)
	}
	tb, err := a.provision(ctx, "test-b-"+suffix, "Teste B", "b@example.test", "Pessoa B", password, 2)
	if err != nil {
		t.Fatal(err)
	}
	// Cleanup is restricted to database and role names created by this test, validated again below.
	defer func() {
		a.mu.Lock()
		for _, tenant := range []Tenant{ta, tb} {
			if p := a.pools[tenant.ID]; p != nil {
				p.pool.Close()
				delete(a.pools, tenant.ID)
			}
		}
		a.mu.Unlock()
		cleanCtx, c := context.WithTimeout(context.Background(), 15*time.Second)
		defer c()
		for _, tenant := range []Tenant{ta, tb} {
			if tenant.DBName != "mezo_t_"+tenant.ID || tenant.DBRole != "mezo_u_"+tenant.ID || !strings.HasPrefix(tenant.Slug, "test-") {
				t.Error("recusando limpeza de banco desconhecido")
				continue
			}
			_, _ = a.control.Exec(cleanCtx, "DELETE FROM auth_sessions WHERE tenant_id=$1", tenant.ID)
			_, _ = a.control.Exec(cleanCtx, "DELETE FROM tenants WHERE id=$1", tenant.ID)
			if _, e := a.admin.Exec(cleanCtx, "DROP DATABASE "+pgx.Identifier{tenant.DBName}.Sanitize()+" WITH (FORCE)"); e != nil {
				t.Error(e)
			}
			if _, e := a.admin.Exec(cleanCtx, "DROP ROLE "+pgx.Identifier{tenant.DBRole}.Sanitize()); e != nil {
				t.Error(e)
			}
		}
	}()
	e := a.router()
	// HTMX must receive its redirect header without a browser-followed 303.
	adminForm := "email=" + strings.ReplaceAll(cfg.AdminEmail, "@", "%40") + "&password=" + cfg.AdminPassword
	adminRequest := httptest.NewRequest("POST", "/admin/login", strings.NewReader(adminForm))
	adminRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	adminRequest.Header.Set("Origin", cfg.Origin)
	adminRequest.Header.Set("HX-Request", "true")
	adminResponse := httptest.NewRecorder()
	e.ServeHTTP(adminResponse, adminRequest)
	if adminResponse.Code != 200 || adminResponse.Header().Get("HX-Redirect") != "/admin" {
		t.Fatalf("login administrativo HTMX falhou: %d %s", adminResponse.Code, adminResponse.Body.String())
	}
	adminCookie := adminResponse.Result().Cookies()[0]
	defer a.control.Exec(context.Background(), "DELETE FROM auth_sessions WHERE token_hash=$1", tokenHash(adminCookie.Value))
	request := func(method, path string, body any, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		var b []byte
		if body != nil {
			b, _ = json.Marshal(body)
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", cfg.Origin)
		r.Header.Set("X-CSRF-Token", csrf)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		e.ServeHTTP(w, r)
		return w
	}
	expect := func(w *httptest.ResponseRecorder, code int) {
		t.Helper()
		if w.Code != code {
			t.Fatalf("HTTP %d; esperado %d: %s", w.Code, code, w.Body.String())
		}
	}
	read := func(w *httptest.ResponseRecorder) map[string]any {
		t.Helper()
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	login := func(tenant Tenant, email string) (*http.Cookie, string) {
		t.Helper()
		w := request("POST", "/api/r/"+tenant.Slug+"/login", map[string]string{"email": email, "password": password}, nil, "")
		expect(w, 200)
		cookie := w.Result().Cookies()[0]
		w = request("GET", "/api/r/"+tenant.Slug+"/bootstrap?role=staff", nil, cookie, "")
		expect(w, 200)
		return cookie, read(w)["csrf"].(string)
	}
	pa, release, err := a.lease(ctx, ta)
	if err != nil {
		t.Fatal(err)
	}
	// Create a second waiter through the same isolated tenant database.
	var managerHash string
	if err = pa.QueryRow(ctx, "SELECT password_hash FROM staff WHERE email='a@example.test'").Scan(&managerHash); err != nil {
		t.Fatal(err)
	}
	if _, err = pa.Exec(ctx, "INSERT INTO staff(id,name,email,password_hash,role) VALUES($1,'Pessoa 2','a2@example.test',$2,'waiter')", randomID(), managerHash); err != nil {
		t.Fatal(err)
	}
	if _, err = pa.Exec(ctx, "INSERT INTO menu_items(id,name,price,category) VALUES('dish','Prato teste',1234,'Principais')"); err != nil {
		t.Fatal(err)
	}
	var token string
	if err = pa.QueryRow(ctx, "SELECT qr_token FROM dining_tables WHERE number=1").Scan(&token); err != nil {
		t.Fatal(err)
	}
	release()
	staff1, csrf1 := login(ta, "a@example.test")
	staff2, csrf2 := login(ta, "a2@example.test")
	expect(request("GET", "/api/r/"+tb.Slug+"/bootstrap?role=staff", nil, staff1, ""), 401)
	expect(request("POST", "/api/r/"+tb.Slug+"/tables/1/open", map[string]any{}, staff1, csrf1), 401)
	pw, err := a.crypt(ta.DBSecret, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, db := range []string{tb.DBName, "mezo_control"} {
		pc, err := a.connection(db, ta.DBRole, pw)
		if err != nil {
			t.Fatal(err)
		}
		conn, err := pgx.ConnectConfig(ctx, pc.ConnConfig)
		if err == nil {
			conn.Close(ctx)
			t.Fatalf("tenant A conseguiu conectar ao banco %s", db)
		}
	}
	w := request("GET", "/r/"+ta.Slug+"/m/"+token, nil, nil, "")
	expect(w, 200)
	customer := w.Result().Cookies()[0]
	w = request("GET", "/api/r/"+ta.Slug+"/bootstrap", nil, customer, "")
	expect(w, 200)
	customerCSRF := read(w)["csrf"].(string)
	expect(request("GET", "/api/r/"+tb.Slug+"/bootstrap", nil, customer, ""), 401)
	expect(request("POST", "/api/r/"+ta.Slug+"/calls", map[string]any{"reason": "help", "note": ""}, customer, ""), 403)
	wrongOrigin := httptest.NewRequest("POST", "/api/r/"+ta.Slug+"/calls", strings.NewReader(`{"reason":"help","note":""}`))
	wrongOrigin.AddCookie(customer)
	wrongOrigin.Header.Set("Content-Type", "application/json")
	wrongOrigin.Header.Set("X-CSRF-Token", customerCSRF)
	wrongOrigin.Header.Set("Origin", "https://other.example")
	wrongResponse := httptest.NewRecorder()
	e.ServeHTTP(wrongResponse, wrongOrigin)
	expect(wrongResponse, 403)
	expect(request("POST", "/api/r/"+ta.Slug+"/calls", map[string]any{"reason": "help", "note": "", "table": 2}, customer, customerCSRF), 400)
	var wg sync.WaitGroup
	created := make([]*httptest.ResponseRecorder, 2)
	for i := range created {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			created[i] = request("POST", "/api/r/"+ta.Slug+"/calls", map[string]any{"reason": "help", "note": "teste"}, customer, customerCSRF)
		}(i)
	}
	wg.Wait()
	for _, v := range created {
		expect(v, 200)
	}
	id := read(created[0])["id"].(string)
	historyPool, historyRelease, err := a.lease(ctx, ta)
	if err != nil {
		t.Fatal(err)
	}
	_, err = historyPool.Exec(ctx, "INSERT INTO calls(id,session_id,reason,status,created_at) SELECT 'history-test-'||n,s.id,'water','done',now()+interval '1 minute' FROM generate_series(1,310) n,table_sessions s WHERE s.table_number=1 AND s.closed_at IS NULL")
	historyRelease()
	if err != nil {
		t.Fatal(err)
	}
	queueResponse := request("GET", "/api/r/"+ta.Slug+"/bootstrap?role=staff", nil, staff1, "")
	expect(queueResponse, 200)
	visible := false
	for _, v := range read(queueResponse)["calls"].([]any) {
		if v.(map[string]any)["id"] == id {
			visible = true
		}
	}
	if !visible {
		t.Fatal("histórico extenso ocultou um chamado ativo")
	}
	if read(created[1])["id"] != id {
		t.Fatal("chamados duplicados concorrentes")
	}
	accepted := make([]*httptest.ResponseRecorder, 2)
	cookies := []*http.Cookie{staff1, staff2}
	csrfTokens := []string{csrf1, csrf2}
	for i := range accepted {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			accepted[i] = request("POST", "/api/r/"+ta.Slug+"/calls/"+id+"/accept", map[string]any{}, cookies[i], csrfTokens[i])
		}(i)
	}
	wg.Wait()
	winner := 0
	if accepted[1].Code == 200 {
		winner = 1
	}
	expect(accepted[winner], 200)
	expect(accepted[1-winner], 409)
	expect(request("POST", "/api/r/"+ta.Slug+"/calls/"+id+"/complete", map[string]any{}, cookies[1-winner], csrfTokens[1-winner]), 409)
	expect(request("POST", "/api/r/"+ta.Slug+"/calls/"+id+"/complete", map[string]any{}, cookies[winner], csrfTokens[winner]), 200)
	expect(request("POST", "/api/r/"+ta.Slug+"/tables/1/consumption", map[string]any{"menuId": "dish", "qty": 2, "price": 1}, staff1, csrf1), 400)
	expect(request("POST", "/api/r/"+ta.Slug+"/tables/1/consumption", map[string]any{"menuId": "dish", "qty": 2}, staff1, csrf1), 201)
	w = request("GET", "/api/r/"+ta.Slug+"/bootstrap", nil, customer, "")
	expect(w, 200)
	totals := read(w)["totals"].(map[string]any)
	if totals["subtotal"] != float64(2468) || totals["service"] != float64(247) || totals["total"] != float64(2715) {
		t.Fatalf("total errado: %v", totals)
	}
	expect(request("POST", "/api/r/"+ta.Slug+"/service", map[string]any{"enabled": false}, customer, customerCSRF), 200)
	w = request("GET", "/api/r/"+ta.Slug+"/bootstrap", nil, customer, "")
	if read(w)["totals"].(map[string]any)["total"] != float64(2468) {
		t.Fatal("serviço opcional não persistido")
	}
	// A fresh application instance must recover both persisted sessions and consumption.
	recovered, err := newApp(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/api/r/"+ta.Slug+"/bootstrap", nil)
	r.AddCookie(customer)
	rw := httptest.NewRecorder()
	recovered.router().ServeHTTP(rw, r)
	expect(rw, 200)
	if read(rw)["totals"].(map[string]any)["total"] != float64(2468) {
		t.Fatal("dados não persistiram")
	}
	recovered.close()
	expect(request("POST", "/api/r/"+ta.Slug+"/tables/1/close", map[string]any{"confirmed": false}, staff1, csrf1), 400)
	expect(request("POST", "/api/r/"+ta.Slug+"/tables/1/close", map[string]any{"confirmed": true, "method": "pix", "expectedTotal": 2468}, staff1, csrf1), 200)
	expect(request("GET", "/api/r/"+ta.Slug+"/bootstrap", nil, customer, ""), 410)
	expect(request("POST", "/api/r/"+ta.Slug+"/tables/1/open", map[string]any{}, staff1, csrf1), 201)
	expect(request("GET", "/api/r/"+ta.Slug+"/bootstrap", nil, customer, ""), 410)
	w = request("GET", "/r/"+ta.Slug+"/m/"+token, nil, customer, "")
	expect(w, 200)
	newCustomer := w.Result().Cookies()[0]
	w = request("GET", "/api/r/"+ta.Slug+"/bootstrap", nil, newCustomer, "")
	expect(w, 200)
	if read(w)["totals"].(map[string]any)["total"] != float64(0) {
		t.Fatal("consumo da conta anterior vazou para a nova")
	}
	t.Log(fmt.Sprintf("isolamento PostgreSQL e HTTP, autenticação, CSRF, concorrência, consumo, persistência e reabertura validados (%s)", suffix))
}

func TestHubTenantAndTableScope(t *testing.T) {
	h := newHub()
	own, off := h.subscribe("tenant-a", "session-a")
	defer off()
	otherTable, off2 := h.subscribe("tenant-a", "session-b")
	defer off2()
	otherTenant, off3 := h.subscribe("tenant-b", "")
	defer off3()
	staff, off4 := h.subscribe("tenant-a", "")
	defer off4()
	h.publish("tenant-a", "session-a")
	for _, s := range []*subscription{own, staff} {
		select {
		case <-s.ch:
		default:
			t.Fatal("evento esperado não recebido")
		}
	}
	for _, s := range []*subscription{otherTable, otherTenant} {
		select {
		case <-s.ch:
			t.Fatal("evento vazou para outra mesa ou tenant")
		default:
		}
	}
}
