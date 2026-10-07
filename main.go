package main

import (
	"context"
	"embed"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
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
	e.IPExtractor = a.clientIP
	e.Server.ReadHeaderTimeout = 5 * time.Second
	e.Server.IdleTimeout = 60 * time.Second
	e.Server.MaxHeaderBytes = 64 * 1024
	loginLimit := middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
		Store: middleware.NewRateLimiterMemoryStoreWithConfig(middleware.RateLimiterMemoryStoreConfig{Rate: 0.1, Burst: 10, ExpiresIn: 5 * time.Minute}),
		DenyHandler: func(c echo.Context, _ string, _ error) error {
			c.Response().Header().Set("Retry-After", "10")
			return echo.NewHTTPError(429, "Muitas tentativas. Aguarde 10 segundos antes de entrar novamente.")
		},
	})
	// Guests on the restaurant Wi-Fi share one address; the real limit is per table, in the database.
	codeLimit := middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
		Store: middleware.NewRateLimiterMemoryStoreWithConfig(middleware.RateLimiterMemoryStoreConfig{Rate: 1, Burst: 30, ExpiresIn: 5 * time.Minute}),
		DenyHandler: func(c echo.Context, _ string, _ error) error {
			c.Response().Header().Set("Retry-After", "5")
			return echo.NewHTTPError(429, "Muitas tentativas. Aguarde alguns segundos e tente novamente.")
		},
	})
	e.Use(middleware.Recover(), middleware.BodyLimitWithConfig(middleware.BodyLimitConfig{Limit: "32K", Skipper: func(c echo.Context) bool { return imageRoute(c.Path()) }}), middleware.BodyLimitWithConfig(middleware.BodyLimitConfig{Limit: "4M", Skipper: func(c echo.Context) bool { return !imageRoute(c.Path()) }}))
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Response().Header().Set("X-Content-Type-Options", "nosniff")
			// No referrer leaves the site. Under no-referrer, browsers also send
			// "Origin: null" on form posts, which the origin check rejects.
			c.Response().Header().Set("Referrer-Policy", "same-origin")
			c.Response().Header().Set("Cache-Control", "no-store")
			if a.cfg.Secure {
				c.Response().Header().Set("Strict-Transport-Security", "max-age=31536000")
			}
			c.Response().Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https://res.cloudinary.com; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
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
		if c.Path() == "/admin/login" {
			_ = a.adminLoginProblem(c, code, message)
			return
		}
		if imageRoute(c.Path()) {
			_ = c.HTML(code, "<p class='notice error' role='alert'>"+template.HTMLEscapeString(message)+"</p>")
			return
		}
		if c.Path() == "/r/:slug/m/:token" && c.Request().Method == http.MethodPost {
			_ = a.tableCodePage(c, code, "", message)
			return
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
	for name := range staticAssets {
		e.GET("/"+name, serveAsset(name))
	}
	e.GET("/", a.home)
	e.GET("/r/:slug/m/:token", a.enterTable)
	e.POST("/r/:slug/m/:token", a.submitTableCode, codeLimit)
	e.GET("/r/:slug/equipe", a.staffPage)
	e.GET("/r/:slug/garcom", a.workstationPage)
	e.GET("/r/:slug/cozinha", a.workstationPage)
	e.GET("/api/r/:slug/operations", a.operations)
	e.GET("/api/r/:slug/tables/:number/bill", a.tableBill)
	e.GET("/api/r/:slug/receipts", a.receipts)
	e.POST("/api/r/:slug/orders", a.createOrder)
	e.POST("/api/r/:slug/customer-orders", a.createCustomerOrder)
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
	e.POST("/api/r/:slug/tables/:number/code", a.renewTableCode)
	e.GET("/api/r/:slug/events", a.events)
	e.GET("/api/r/:slug/revision", a.revision)
	e.GET("/admin", a.adminPage)
	e.POST("/admin/login", a.adminLogin, loginLimit)
	e.GET("/equipe", a.teamEntry)
	e.POST("/admin/logout", a.adminLogout)
	e.POST("/admin/restaurants", a.createRestaurant)
	e.POST("/admin/r/:slug/staff", a.createStaff)
	e.POST("/admin/r/:slug/staff/:id/active", a.setStaffActive)
	e.POST("/admin/r/:slug/staff/:id/password", a.resetStaffPassword)
	e.POST("/admin/r/:slug/menu/:id", a.editMenuItem)
	e.POST("/admin/r/:slug/menu", a.createMenuItem)
	e.GET("/admin/r/:slug/qr/:number", a.tableQR)
	e.POST("/admin/r/:slug/logo", a.uploadImage)
	e.POST("/admin/r/:slug/menu/:id/photo", a.uploadImage)
	e.GET("/r/:slug/imagens", a.imagesPage)
	// The restaurant manager runs the same administration as the platform, limited to the own house.
	e.GET("/r/:slug/gestao", a.housePage)
	e.POST("/api/r/:slug/staff", a.createStaff)
	e.POST("/api/r/:slug/staff/:id/active", a.setStaffActive)
	e.POST("/api/r/:slug/staff/:id/password", a.resetStaffPassword)
	e.POST("/api/r/:slug/menu", a.createMenuItem)
	e.POST("/api/r/:slug/menu/:id", a.editMenuItem)
	e.GET("/api/r/:slug/qr/:number", a.tableQR)
	e.POST("/api/r/:slug/logo", a.uploadImage)
	e.POST("/api/r/:slug/menu/:id/photo", a.uploadImage)
	return e
}
