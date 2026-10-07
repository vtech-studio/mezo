package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/joho/godotenv"
)

func testPhoto() []byte {
	var b bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{158, 74, 42, 255})
		}
	}
	_ = png.Encode(&b, img)
	return b.Bytes()
}
func TestImageValidation(t *testing.T) {
	if err := validateImage(testPhoto()); err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{nil, []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), []byte("not an image"), make([]byte, maxImageBytes+1), testPhoto()[:30]} {
		if validateImage(data) == nil {
			t.Fatal("invalid image accepted")
		}
	}
	if cloudSignature(map[string]string{"timestamp": "1315060510", "public_id": "sample_image", "eager": "w_400,h_300,c_pad|w_260,h_200,c_crop"}, "abcd") != "cc927e1290f9e3ae4c1a741eda21a4630b4ce80f9ce0bc0296337d25cf40f91e" {
		t.Fatal("wrong SHA256 signature")
	}
}

type imageRoundTrip func(*http.Request) (*http.Response, error)

func (f imageRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Real tenant databases; Cloudinary is mocked unless MEZO_CLOUDINARY_TEST=1.
// MEZO_PUBLISHED_TEST=1 exercises the actual Vercel upload route as well.
func TestRestaurantImages(t *testing.T) {
	if os.Getenv("MEZO_INTEGRATION") != "1" {
		t.Skip("PostgreSQL integration opt-in")
	}
	_ = godotenv.Load()
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	realCloud := os.Getenv("MEZO_CLOUDINARY_TEST") == "1"
	published := os.Getenv("MEZO_PUBLISHED_TEST") == "1"
	keep := os.Getenv("MEZO_IMAGE_UI_FIXTURE") == "1"
	if keep && (published || cfg.Secure) {
		t.Fatal("UI fixtures only on local loopback")
	}
	if published && (!realCloud || !cfg.Serverless || !cfg.Secure) {
		t.Fatal("published image test requires cloud credentials and HTTPS")
	}
	if realCloud && cfg.CloudName == "" {
		t.Fatal("missing Cloudinary configuration")
	}
	if !realCloud {
		cfg.CloudName = "testcloud"
		cfg.CloudKey = "test-key"
		cfg.CloudSecret = "test-secret"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	a, err := newApp(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.close)
	fixtures := []Tenant{}
	createdImages := []string{}
	t.Cleanup(func() {
		if keep && !t.Failed() {
			return
		}
		clean, stop := context.WithTimeout(context.Background(), 45*time.Second)
		defer stop()
		if realCloud {
			for _, id := range createdImages {
				if _, e := a.cloudRequest(clean, "destroy", map[string]string{"public_id": id, "invalidate": "true"}, nil); e != nil {
					t.Error("test image cleanup", e)
				}
			}
		}
		for _, v := range fixtures {
			if !strings.HasPrefix(v.Slug, "test-photo-") || v.DBName != "mezo_t_"+v.ID || v.DBRole != "mezo_u_"+v.ID {
				t.Error("refusing unrelated cleanup")
				continue
			}
			a.mu.Lock()
			if p := a.pools[v.ID]; p != nil {
				p.pool.Close()
				delete(a.pools, v.ID)
			}
			a.mu.Unlock()
			_, _ = a.control.Exec(clean, "DELETE FROM auth_sessions WHERE tenant_id=$1", v.ID)
			_, _ = a.control.Exec(clean, "DELETE FROM tenants WHERE id=$1", v.ID)
			if _, e := a.admin.Exec(clean, "DROP DATABASE "+pgx.Identifier{v.DBName}.Sanitize()+" WITH (FORCE)"); e != nil {
				t.Error(e)
			}
			if _, e := a.admin.Exec(clean, "DROP ROLE "+pgx.Identifier{v.DBRole}.Sanitize()); e != nil {
				t.Error(e)
			}
		}
	})
	password := randomID()
	suffix := randomID()[:12]
	for _, name := range []string{"a", "b"} {
		v, e := a.provision(ctx, "test-photo-"+name+"-"+suffix, "Casa de teste de imagens", "images@example.test", "Responsável", password, 1)
		if e != nil {
			t.Fatal(e)
		}
		fixtures = append(fixtures, v)
	}
	tenant := fixtures[0]
	p, release, err := a.lease(ctx, tenant)
	if err != nil {
		t.Fatal(err)
	}
	var managerID, hash string
	if err = p.QueryRow(ctx, "SELECT id,password_hash FROM staff LIMIT 1").Scan(&managerID, &hash); err != nil {
		t.Fatal(err)
	}
	waiterID := randomID()
	_, err = p.Exec(ctx, "INSERT INTO staff(id,name,email,password_hash,role) VALUES($1,'Garçom','waiter@example.test',$2,'waiter')", waiterID, hash)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Exec(ctx, "INSERT INTO menu_items(id,name,price,category) VALUES('photo-dish','Prato de teste',1200,'Principais')"); err != nil {
		t.Fatal(err)
	}
	release()
	cookieFor := func(subject string) (*http.Cookie, string) {
		r := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		c := a.router().NewContext(r, w)
		if e := a.newSession(c, "staff", tenant.ID, subject); e != nil {
			t.Fatal(e)
		}
		cookie := w.Result().Cookies()[0]
		var csrf string
		if e := a.control.QueryRow(ctx, "SELECT csrf FROM auth_sessions WHERE token_hash=$1", tokenHash(cookie.Value)).Scan(&csrf); e != nil {
			t.Fatal(e)
		}
		return cookie, csrf
	}
	manager, csrf := cookieFor(managerID)
	waiter, waiterCSRF := cookieFor(waiterID)
	uploads := 0
	failCloud := false
	if !realCloud {
		a.imageClient = &http.Client{Transport: imageRoundTrip(func(r *http.Request) (*http.Response, error) {
			if r.URL.Scheme != "https" || r.URL.Host != "api.cloudinary.com" {
				t.Fatal("unexpected destination")
			}
			_ = r.ParseMultipartForm(4 * 1024 * 1024)
			defer r.MultipartForm.RemoveAll()
			params := map[string]string{}
			for k, v := range r.MultipartForm.Value {
				if k != "api_key" && k != "signature" {
					params[k] = v[0]
				}
			}
			if cloudSignature(params, cfg.CloudSecret) != r.FormValue("signature") {
				t.Fatal("wrong signature")
			}
			if failCloud {
				return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{}`)), Header: http.Header{}}, nil
			}
			uploads++
			id := r.FormValue("public_id")
			if !strings.HasPrefix(id, "mezo/"+tenant.ID+"/") {
				t.Fatal("wrong tenant folder")
			}
			b, _ := json.Marshal(map[string]string{"secure_url": "https://res.cloudinary.com/testcloud/image/upload/v1/" + id + ".png", "public_id": id})
			return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(b)), Header: http.Header{}}, nil
		})}
	}
	router := a.router()
	client := &http.Client{Timeout: 40 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request := func(method, path string, data []byte, cookie *http.Cookie, token string) *httptest.ResponseRecorder {
		var b bytes.Buffer
		contentType := ""
		if method == "POST" {
			w := multipart.NewWriter(&b)
			_ = w.WriteField("csrf", token)
			f, _ := w.CreateFormFile("image", "fixture.png")
			_, _ = f.Write(data)
			_ = w.Close()
			contentType = w.FormDataContentType()
		}
		target := path
		if published {
			target = cfg.Origin + path
		}
		r := httptest.NewRequest(method, target, &b)
		r.Header.Set("Origin", cfg.Origin)
		r.Header.Set("HX-Request", "true")
		r.Header.Set("Content-Type", contentType)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		out := httptest.NewRecorder()
		if !published {
			router.ServeHTTP(out, r)
			return out
		}
		r.RequestURI = ""
		res, e := client.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		for k, v := range res.Header {
			out.Header()[k] = v
		}
		out.WriteHeader(res.StatusCode)
		_, _ = io.Copy(out, res.Body)
		return out
	}
	expect := func(r *httptest.ResponseRecorder, code int) {
		t.Helper()
		if r.Code != code {
			t.Fatalf("image HTTP %d expected %d: %s", r.Code, code, r.Body.String())
		}
	}
	base := "/api/r/" + tenant.Slug
	expect(request("POST", base+"/logo", testPhoto(), nil, csrf), 401)
	expect(request("POST", base+"/logo", testPhoto(), manager, "wrong"), 403)
	expect(request("POST", "/api/r/"+fixtures[1].Slug+"/logo", testPhoto(), manager, csrf), 401)
	expect(request("POST", base+"/logo", testPhoto(), waiter, waiterCSRF), 403)
	expect(request("GET", "/r/"+tenant.Slug+"/imagens", nil, waiter, ""), 403)
	expect(request("POST", base+"/logo", []byte("<svg></svg>"), manager, csrf), 400)
	expect(request("POST", base+"/logo", make([]byte, maxImageBytes+1), manager, csrf), 400)
	expect(request("POST", base+"/menu/missing/photo", testPhoto(), manager, csrf), 404)
	if !realCloud && uploads != 0 {
		t.Fatal("unauthorized or invalid file reached Cloudinary")
	}
	for _, path := range []string{base + "/logo", base + "/menu/photo-dish/photo"} {
		expect(request("POST", path, testPhoto(), manager, csrf), 200)
	}
	p, release, err = a.lease(ctx, tenant)
	if err != nil {
		t.Fatal(err)
	}
	var logo, logoID, photo, photoID string
	if err = p.QueryRow(ctx, "SELECT logo_url,logo_public_id FROM restaurant_brand").Scan(&logo, &logoID); err != nil {
		t.Fatal(err)
	}
	if err = p.QueryRow(ctx, "SELECT image_url,image_public_id FROM menu_items WHERE id='photo-dish'").Scan(&photo, &photoID); err != nil {
		t.Fatal(err)
	}
	release()
	createdImages = append(createdImages, logoID, photoID)
	if logo == "" || photo == "" || logo == photo {
		t.Fatal("images not independently persisted")
	}
	if !realCloud {
		failCloud = true
		expect(request("POST", base+"/logo", testPhoto(), manager, csrf), 502)
		failCloud = false
	}
	page := request("GET", "/r/"+tenant.Slug+"/imagens", nil, manager, "")
	expect(page, 200)
	if !strings.Contains(page.Body.String(), photo) || !strings.Contains(page.Body.String(), logo) {
		t.Fatal("persisted images absent from management page")
	}
	boot := request("GET", base+"/bootstrap?role=staff", nil, manager, "")
	expect(boot, 200)
	if !strings.Contains(boot.Body.String(), photo) || !strings.Contains(boot.Body.String(), logo) {
		t.Fatal("images absent from bootstrap")
	}
	if strings.Contains(page.Body.String(), cfg.CloudSecret) || strings.Contains(boot.Body.String(), cfg.CloudSecret) {
		t.Fatal("secret exposed")
	}
	if realCloud {
		for _, assetURL := range []string{logo, photo} {
			res, e := client.Get(assetURL)
			if e != nil {
				t.Fatal(e)
			}
			body, e := io.ReadAll(io.LimitReader(res.Body, maxImageBytes+1))
			res.Body.Close()
			if e != nil || res.StatusCode != 200 || validateImage(body) != nil {
				t.Fatal("uploaded image not retrievable")
			}
		}
	}
	if keep {
		b, _ := json.Marshal(map[string]any{"tenants": fixtures, "cookie": manager, "csrf": csrf, "images": createdImages, "photo": photo, "logo": logo})
		if err = os.WriteFile("../../work/image-ui-fixture.json", b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Log(fmt.Sprintf("logo + photo persisted; permissions, CSRF, tenant isolation, file validation verified; realCloud=%v published=%v", realCloud, published))
}
