package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"golang.org/x/crypto/bcrypt"
)

func TestClientIPOnlyTrustsVercelHeadersOnVercel(t *testing.T) {
	r := httptest.NewRequest("POST", "/admin/login", nil)
	r.RemoteAddr = "192.0.2.1:4321"
	r.Header.Set("X-Vercel-Forwarded-For", "198.51.100.2")
	r.Header.Set("X-Forwarded-For", "203.0.113.3")
	a := &App{}
	if a.clientIP(r) != "192.0.2.1" {
		t.Fatal("confiou em cabeçalho de proxy fora da Vercel")
	}
	a.cfg.Serverless = true
	if a.clientIP(r) != "198.51.100.2" {
		t.Fatal("não utilizou o IP fornecido pela Vercel")
	}
	r.Header.Set("X-Vercel-Forwarded-For", "not-an-ip")
	if a.clientIP(r) != "192.0.2.1" {
		t.Fatal("aceitou um IP inválido")
	}
}

func TestLoginBurstSeparatesVercelClientsAndRenders429(t *testing.T) {
	a := &App{cfg: Config{Serverless: true, Origin: "https://example.test"}}
	router := a.router()
	request := func(ip string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/admin/login", nil)
		r.RemoteAddr = "127.0.0.1:4321"
		r.Header.Set("X-Vercel-Forwarded-For", ip)
		r.Header.Set("HX-Request", "true")
		// Invalid origin stops authentication before any database access.
		r.Header.Set("Origin", "https://wrong.example")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	for i := 0; i < 10; i++ {
		if w := request("198.51.100.1"); w.Code != 403 {
			t.Fatal("burst bloqueado antes do limite", w.Code)
		}
	}
	w := request("198.51.100.1")
	if w.Code != 429 || w.Header().Get("Retry-After") != "10" || !strings.Contains(w.Body.String(), "data-retry-seconds='10'") {
		t.Fatal("limite por IP não retornou aviso com espera")
	}
	if w = request("198.51.100.2"); w.Code != 403 {
		t.Fatal("um cliente bloqueou outro cliente no mesmo proxy", w.Code)
	}
}

func TestAdminLoginFeedbackAndCooldown(t *testing.T) {
	if os.Getenv("MEZO_INTEGRATION") != "1" {
		t.Skip("defina MEZO_INTEGRATION=1 para testar com PostgreSQL real")
	}
	_ = godotenv.Load()
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	published := os.Getenv("MEZO_PUBLISHED_TEST") == "1"
	if published && (!cfg.Serverless || !cfg.Secure) {
		t.Fatal("teste publicado exige configuração HTTPS da Vercel")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	a, err := newApp(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	email, password := "auth-fixture-"+randomID()+"@example.test", randomID()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.control.Exec(ctx, "INSERT INTO administrators(email,password_hash) VALUES($1,$2)", email, hash); err != nil {
		t.Fatal(err)
	}
	key := a.loginKey("admin:", email)
	defer func() {
		clean, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		for _, query := range []string{"DELETE FROM auth_sessions WHERE kind='admin' AND subject=$1", "DELETE FROM administrators WHERE email=$1"} {
			if _, e := a.control.Exec(clean, query, email); e != nil {
				t.Error(e)
			}
		}
		if _, e := a.control.Exec(clean, "DELETE FROM login_attempts WHERE key=$1", key); e != nil {
			t.Error(e)
		}
	}()
	router := a.router()
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	n := 0
	request := func(path, pass string, htmx bool, origin string) *httptest.ResponseRecorder {
		n++
		r := httptest.NewRequest("POST", path, strings.NewReader(url.Values{"email": {email}, "password": {pass}}.Encode()))
		r.RemoteAddr = "192.0.2." + strconv.Itoa(n) + ":4321"
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if htmx {
			r.Header.Set("HX-Request", "true")
		}
		w := httptest.NewRecorder()
		if !published {
			router.ServeHTTP(w, r)
			return w
		}
		out := r.Clone(ctx)
		out.URL, _ = url.Parse(cfg.Origin + path)
		out.Host = out.URL.Host
		out.RequestURI = ""
		res, e := client.Do(out)
		if e != nil {
			t.Fatal("falha na requisição ao deployment")
		}
		defer res.Body.Close()
		for k, vs := range res.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(res.StatusCode)
		_, _ = io.Copy(w, res.Body)
		return w
	}
	expect := func(w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("login retornou %d; esperado %d", w.Code, status)
		}
	}
	w := request("/admin/login", "incorrect-password", true, cfg.Origin)
	expect(w, 401)
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") || !strings.Contains(w.Body.String(), "role='alert'") || len(w.Result().Cookies()) != 0 {
		t.Fatal("erro de login não retornou aviso HTML sem sessão")
	}
	repeats := 20
	if published {
		repeats = 2
	}
	for i := 0; i < repeats; i++ {
		w = request("/admin/login", password, true, cfg.Origin)
		expect(w, 200)
		if w.Header().Get("HX-Redirect") != "/admin" || len(w.Result().Cookies()) != 1 {
			t.Fatal("login válido não criou sessão e redirecionamento")
		}
	}
	var count int
	if err = a.control.QueryRow(ctx, "SELECT count(*) FROM login_attempts WHERE key=$1", key).Scan(&count); err != nil || count != 0 {
		t.Fatal("logins bem-sucedidos consumiram o limite", err)
	}
	if _, err = a.control.Exec(ctx, "INSERT INTO login_attempts(key,attempts,reset_at) VALUES($1,15,now()+interval '61 seconds')", key); err != nil {
		t.Fatal(err)
	}
	w = request("/admin/login", password, true, cfg.Origin)
	expect(w, 429)
	retry, e := strconv.Atoi(w.Header().Get("Retry-After"))
	if e != nil || retry < 1 || retry > 61 || !strings.Contains(w.Body.String(), "data-retry-seconds='") || len(w.Result().Cookies()) != 0 {
		t.Fatal("bloqueio não informou espera real ou criou sessão")
	}
	if !published {
		// New in-memory limiter, same persisted account cooldown.
		second, e := newApp(ctx, cfg)
		if e != nil {
			t.Fatal(e)
		}
		defer second.close()
		router = second.router()
		w = request("/admin/login", password, true, cfg.Origin)
		expect(w, 429)
	}
	if _, err = a.control.Exec(ctx, "UPDATE login_attempts SET reset_at=now()-interval '1 second' WHERE key=$1", key); err != nil {
		t.Fatal(err)
	}
	w = request("/admin/login", password, true, cfg.Origin)
	expect(w, 200)
	w = request("/admin/login", "incorrect-password", false, cfg.Origin)
	expect(w, 401)
	if !strings.Contains(w.Body.String(), "id=\"admin-login\"") || !strings.Contains(w.Body.String(), "Sou da equipe de um restaurante") {
		t.Fatal("fallback sem HTMX perdeu formulário e acesso da equipe")
	}
	w = request("/admin/login", password, true, "https://wrong-origin.example")
	expect(w, 403)
	if !strings.Contains(w.Body.String(), "Origem não autorizada") {
		t.Fatal("proteção de origem foi alterada")
	}
	t.Log("aviso de login visível; espera real; retomada após expiração; logins válidos não acumulam limite; origem e sessão preservadas")
}
