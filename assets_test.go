package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStaticAssetsAreVersionedAndCached(t *testing.T) {
	router := (&App{cfg: Config{Origin: "http://127.0.0.1:4173"}}).router()
	get := func(path, etag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		if etag != "" {
			r.Header.Set("If-None-Match", etag)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	for name, f := range staticAssets {
		w := get(assetURL(name), "")
		if w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") || w.Body.Len() != len(f.body) || w.Header().Get("Content-Type") != f.mime {
			t.Fatalf("%s versionado: %d %q", name, w.Code, w.Header().Get("Cache-Control"))
		}
		for _, path := range []string{"/" + name, "/" + name + "?v=antiga"} {
			w = get(path, "")
			if w.Code != 200 || w.Header().Get("Cache-Control") != "no-cache" {
				t.Fatalf("%s sem a versão atual ficou em cache: %q", path, w.Header().Get("Cache-Control"))
			}
		}
		if w = get("/"+name, w.Header().Get("ETag")); w.Code != 304 {
			t.Fatalf("%s não revalidou pelo ETag: %d", name, w.Code)
		}
	}
	pagesAndFiles := map[string][]string{
		"/":      {"admin.css", "favicon.svg", "mezo-logo.png"},
		"/admin": {"admin.css", "admin.js", "htmx.min.js", "favicon.svg", "mezo-logo.png"},
	}
	for path, names := range pagesAndFiles {
		body := get(path, "").Body.String()
		for _, name := range names {
			if !strings.Contains(body, `"`+assetURL(name)+`"`) {
				t.Fatalf("%s sem %s versionado", path, name)
			}
		}
	}
	for _, name := range []string{"styles.css", "backend.js", "customer.js", "app.js", "favicon.svg", "mezo-logo.png"} {
		if !strings.Contains(customerPage, `"`+assetURL(name)+`"`) {
			t.Fatalf("app do cliente sem %s versionado", name)
		}
	}
	if !strings.Contains(customerPage, `<meta name="mezo-runtime" content="server">`) {
		t.Fatal("app do cliente sem o marcador de execução no servidor")
	}
	// Form posts (table code, admin login without JavaScript) need the real Origin.
	if policy := get("/", "").Header().Get("Referrer-Policy"); policy != "same-origin" {
		t.Fatalf("política de referência %q faz o navegador enviar Origin nulo em formulários", policy)
	}
}

func TestStaffScreenIsInstallableWithAlerts(t *testing.T) {
	var b bytes.Buffer
	if err := pages.ExecuteTemplate(&b, "workstation.html", map[string]string{"Name": "Casa", "Slug": "casa", "View": "kitchen", "Title": "Cozinha"}); err != nil {
		t.Fatal(err)
	}
	html := b.String()
	for _, name := range []string{"manifest.webmanifest", "icon-192.png", "staff-alerts.js", "workstation.js"} {
		if !strings.Contains(html, `"`+assetURL(name)+`"`) {
			t.Fatalf("tela da equipe sem %s", name)
		}
	}
	if strings.Index(html, assetURL("staff-alerts.js")) > strings.Index(html, assetURL("workstation.js")) {
		t.Fatal("os alertas precisam carregar antes da tela da equipe")
	}
	var manifest struct {
		Name    string `json:"name"`
		Display string `json:"display"`
		Icons   []struct{ Src, Sizes, Purpose string }
	}
	if err := json.Unmarshal(staticAssets["manifest.webmanifest"].body, &manifest); err != nil || manifest.Name == "" || manifest.Display != "standalone" || len(manifest.Icons) == 0 {
		t.Fatal("manifesto inválido", err)
	}
	for _, icon := range manifest.Icons {
		if f, ok := staticAssets[strings.TrimPrefix(icon.Src, "/")]; !ok || f.mime != "image/png" {
			t.Fatalf("o manifesto aponta para um ícone que o servidor não entrega: %s", icon.Src)
		}
	}
	if staticAssets["manifest.webmanifest"].mime != "application/manifest+json" {
		t.Fatal("manifesto servido com tipo errado")
	}
}
