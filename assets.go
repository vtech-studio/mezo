package main

import (
	"crypto/sha256"
	"encoding/hex"
	"html/template"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

type staticAsset struct {
	body          []byte
	mime, version string
}

// Pages reference these files with a content version, so browsers and the
// Vercel CDN keep them until the file changes in a new deploy.
var staticAssets = loadStaticAssets("styles.css", "app.js", "backend.js", "customer.js", "favicon.svg", "mezo-logo.png", "admin.css", "admin.js", "htmx.min.js", "workstation.css", "workstation.js", "staff-alerts.js", "manifest.webmanifest", "icon-192.png", "icon-512.png", "icon-maskable-512.png")

func loadStaticAssets(names ...string) map[string]staticAsset {
	out := make(map[string]staticAsset, len(names))
	for _, name := range names {
		body, err := assets.ReadFile("dist/" + name)
		if err != nil {
			panic(err)
		}
		mime := "text/javascript; charset=utf-8"
		switch {
		case strings.HasSuffix(name, ".css"):
			mime = "text/css; charset=utf-8"
		case strings.HasSuffix(name, ".svg"):
			mime = "image/svg+xml"
		case strings.HasSuffix(name, ".png"):
			mime = "image/png"
		case strings.HasSuffix(name, ".webmanifest"):
			mime = "application/manifest+json"
		}
		sum := sha256.Sum256(body)
		out[name] = staticAsset{body: body, mime: mime, version: hex.EncodeToString(sum[:6])}
	}
	return out
}

func assetURL(name string) string {
	return "/" + name + "?v=" + staticAssets[name].version
}

func serveAsset(name string) echo.HandlerFunc {
	f := staticAssets[name]
	etag := `"` + f.version + `"`
	return func(c echo.Context) error {
		h := c.Response().Header()
		h.Set("ETag", etag)
		if c.QueryParam("v") == f.version {
			h.Set("Cache-Control", "public, max-age=31536000, s-maxage=31536000, immutable")
		} else {
			h.Set("Cache-Control", "no-cache")
		}
		if c.Request().Header.Get("If-None-Match") == etag {
			return c.NoContent(http.StatusNotModified)
		}
		return c.Blob(http.StatusOK, f.mime, f.body)
	}
}

// Templates are embedded, so they are parsed once.
var pages = template.Must(template.New("").Funcs(template.FuncMap{"asset": assetURL}).ParseFS(assets, "templates/*.html"))

// customerPage is the static customer app with the runtime marker and versioned files.
var customerPage = func() string {
	b, err := assets.ReadFile("dist/index.html")
	if err != nil {
		panic(err)
	}
	html := strings.Replace(string(b), "<head>", "<head><meta name=\"mezo-runtime\" content=\"server\">", 1)
	for name := range staticAssets {
		html = strings.ReplaceAll(html, `"/`+name+`"`, `"`+assetURL(name)+`"`)
	}
	return html
}()
