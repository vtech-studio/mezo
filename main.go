package main

import (
	"context"
	"embed"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

//go:embed dist migrations templates fixtures
var assets embed.FS

func main() {
	_ = godotenv.Load()
	cfg, err := loadConfig()
	if err != nil {
		slog.Error("configuração inválida", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app, err := newApp(ctx, cfg)
	if err != nil {
		slog.Error("não foi possível iniciar o Mezô", "error", err)
		os.Exit(1)
	}
	defer app.close()
	if err = app.seedDemo(ctx); err != nil {
		slog.Error("falha ao preparar exemplo local", "error", err)
		return
	}
	e := app.router()
	go func() {
		if err := e.Start(cfg.Listen); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("servidor encerrado", "error", err)
			stop()
		}
	}()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = e.Shutdown(shutdown)
}

func (a *App) router() *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.IPExtractor = echo.ExtractIPDirect()
	e.Server.ReadHeaderTimeout = 5 * time.Second
	e.Server.IdleTimeout = 60 * time.Second
	e.Server.MaxHeaderBytes = 64 * 1024
	loginLimit := middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
		Store: middleware.NewRateLimiterMemoryStoreWithConfig(middleware.RateLimiterMemoryStoreConfig{Rate: 0.1, Burst: 10, ExpiresIn: 5 * time.Minute}),
		DenyHandler: func(c echo.Context, _ string, _ error) error {
			return echo.NewHTTPError(429, "Muitas tentativas. Aguarde um momento antes de entrar novamente.")
		},
	})
	e.Use(middleware.Recover(), middleware.BodyLimit("32K"))
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Response().Header().Set("X-Content-Type-Options", "nosniff")
			c.Response().Header().Set("Referrer-Policy", "no-referrer")
			c.Response().Header().Set("Cache-Control", "no-store")
			if a.cfg.Secure {
				c.Response().Header().Set("Strict-Transport-Security", "max-age=31536000")
			}
			c.Response().Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
			return next(c)
		}
	})
	e.HTTPErrorHandler = func(err error, c echo.Context) {
		if c.Response().Committed {
			return
		}
		code := 500
		message := "Não foi possível concluir. Tente novamente."
		var he *echo.HTTPError
		if errors.As(err, &he) {
			code = he.Code
			if m, ok := he.Message.(string); ok {
				message = m
			}
		}
		if code == 500 {
			slog.Error("requisição falhou", "path", c.Path(), "error", err)
		}
		_ = c.JSON(code, map[string]string{"error": message})
	}
	e.GET("/healthz", func(c echo.Context) error {
		ctx, cancel := context.WithTimeout(c.Request().Context(), 2*time.Second)
		defer cancel()
		if err := a.control.Ping(ctx); err != nil {
			return echo.NewHTTPError(503, "Banco indisponível")
		}
		return c.JSON(200, map[string]string{"status": "ok", "app": "Mezô"})
	})
	for _, name := range []string{"styles.css", "app.js", "backend.js", "favicon.svg", "admin.css", "admin.js", "htmx.min.js", "workstation.css", "workstation.js"} {
		name := name
		e.GET("/"+name, func(c echo.Context) error {
			b, err := assets.ReadFile("dist/" + name)
			if err != nil {
				return echo.ErrNotFound
			}
			mime := "text/javascript; charset=utf-8"
			if strings.HasSuffix(name, ".css") {
				mime = "text/css; charset=utf-8"
			}
			if name == "favicon.svg" {
				mime = "image/svg+xml"
			}
			return c.Blob(200, mime, b)
		})
	}
	e.GET("/", a.home)
	e.GET("/r/:slug/m/:token", a.enterTable)
	e.GET("/r/:slug/equipe", a.staffPage)
	e.GET("/r/:slug/garcom", a.workstationPage)
	e.GET("/r/:slug/cozinha", a.workstationPage)
	e.GET("/api/r/:slug/operations", a.operations)
	e.GET("/api/r/:slug/tables/:number/bill", a.tableBill)
	e.GET("/api/r/:slug/receipts", a.receipts)
	e.POST("/api/r/:slug/orders", a.createOrder)
	e.POST("/api/r/:slug/orders/:id/:action", a.updateOrder)
	e.POST("/api/r/:slug/password", a.changeStaffPassword, loginLimit)
	e.GET("/api/r/:slug/bootstrap", a.bootstrap)
	e.POST("/api/r/:slug/login", a.staffLogin, loginLimit)
	e.POST("/api/r/:slug/logout", a.logout)
	e.POST("/api/r/:slug/calls", a.createCall)
	e.POST("/api/r/:slug/calls/:id/:action", a.updateCall)
	e.POST("/api/r/:slug/service", a.service)
	e.POST("/api/r/:slug/tables/:number/consumption", a.addConsumption)
	e.POST("/api/r/:slug/tables/:number/close", a.closeTable)
	e.POST("/api/r/:slug/tables/:number/open", a.openTable)
	e.GET("/api/r/:slug/events", a.events)
	e.GET("/admin", a.adminPage)
	e.POST("/admin/login", a.adminLogin, loginLimit)
	e.POST("/admin/logout", a.adminLogout)
	e.POST("/admin/restaurants", a.createRestaurant)
	e.POST("/admin/r/:slug/staff", a.createStaff)
	e.POST("/admin/r/:slug/staff/:id/active", a.setStaffActive)
	e.POST("/admin/r/:slug/menu/:id", a.editMenuItem)
	e.POST("/admin/r/:slug/menu", a.createMenuItem)
	e.GET("/admin/r/:slug/qr/:number", a.tableQR)
	return e
}
